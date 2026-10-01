package acp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	// processWaitDelay 进程组回收等待：cmd.Cancel（整组强杀）触发后，WaitDelay 内
	// 管道未收口则强制关闭。对齐 bot 域 driver_claude 的 5s 先例（工具孙进程可能
	// 持写端，取消只 kill 主进程时管道不关则 Wait 永久阻塞）。
	processWaitDelay = 5 * time.Second

	// stderrTailCapacity stderr tail 容量（runner 的 osrun.Tail 诊断同款 64KiB）。
	stderrTailCapacity = 64 << 10
	// stderrSummaryRunes 错误摘要里 stderr tail 的保留 rune 数（driver_claude 300 先例）。
	stderrSummaryRunes = 300
	// stderrLineBufSize stderr 逐行读取缓冲；单行超过 stderrMaxLineLen 截断日志但不断流。
	stderrLineBufSize = 64 * 1024
	stderrMaxLineLen  = 16 * 1024
)

// SpawnConfig 描述一次 ACP agent 子进程拉起参数。client 只认本结构，不感知 agent：
// catalog 条目经 agentcatalog.Entry.SpawnConfig 翻译为本结构后驱动（T1.2）；
// TODO(T1.3): claude 路径一致性（resolveClaudeBin → CLAUDE_CODE_EXECUTABLE）经 Env 注入，
// login PATH 亦由调用方按探测结果经 Env["PATH"] 覆盖（nvm 等 GUI 缺失路径场景）。
type SpawnConfig struct {
	// Command argv 全量（Command[0] 为可执行文件；vendored 策略下为 vendored 入口）。
	Command []string
	// Env 追加/覆盖的环境变量（在 sidecar 环境之上按键覆盖）。
	Env map[string]string
	// Cwd 子进程工作目录（同为 session/new 的默认目录候选）。
	Cwd string
}

// AgentProcess 一个 ACP agent 子进程 + 进程组句柄。生命周期独立于任何回合 ctx：
// 回合取消只发 session/cancel，不动进程；进程终结只经 Kill/Close。
type AgentProcess struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc

	stdin  io.WriteCloser
	stdout io.ReadCloser

	cwd string // NewSession 缺省目录候选

	mu      sync.Mutex
	waitErr error
	done    chan struct{} // cmd.Wait 返回后关闭；waitErr 的写入 happens-before 关闭

	Stderr *stderrTail // 全量 stderr tail（进程异常退出时拼进错误摘要）
}

// spawnAgent 拉起子进程并启动 stderr 泵。两根管道的协议装配（acp.Connect）由 client.go 接管。
func spawnAgent(cfg SpawnConfig, log *zap.Logger) (*AgentProcess, error) {
	if len(cfg.Command) == 0 || strings.TrimSpace(cfg.Command[0]) == "" {
		return nil, errors.New("ACP agent 启动命令为空")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, cfg.Command[0], cfg.Command[1:]...)
	cmd.Dir = cfg.Cwd
	cmd.Env = mergeEnv(cfg.Env)
	cmd.SysProcAttr = processGroupAttr()
	// ctx 取消 → 整组 SIGKILL；WaitDelay 收口孙进程管道（进程组里有后代持 stdout
	// 写端时管道不关则 Wait 永久阻塞，driver_claude 同款注释）。
	cmd.Cancel = func() error { return killProcessGroup(cmd) }
	cmd.WaitDelay = processWaitDelay

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("ACP agent stdin 管道创建失败: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("ACP agent stdout 管道创建失败: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("ACP agent stderr 管道创建失败: %w", err)
	}

	p := &AgentProcess{
		cmd:    cmd,
		cancel: cancel,
		stdin:  stdin,
		stdout: stdout,
		cwd:    cfg.Cwd,
		done:   make(chan struct{}),
		Stderr: newStderrTail(stderrTailCapacity),
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("ACP agent 启动失败: %w", err)
	}
	go p.pumpStderr(stderr, log)
	go p.awaitExit()
	return p, nil
}

// pumpStderr 逐行读 stderr：全量进 tail（崩溃证据完整性），三分类只决定日志级别。
func (p *AgentProcess) pumpStderr(r io.Reader, log *zap.Logger) {
	reader := bufio.NewReaderSize(r, stderrLineBufSize)
	for {
		line, readErr := reader.ReadString('\n')
		if trimmed := strings.TrimRight(line, "\r\n"); trimmed != "" {
			_, _ = p.Stderr.Write([]byte(trimmed + "\n"))
			logStderrLine(log, classifyStderr(trimmed), truncateRunes(trimmed, stderrMaxLineLen))
		}
		if readErr != nil {
			return
		}
	}
}

func logStderrLine(log *zap.Logger, kind stderrKind, line string) {
	switch kind {
	case stderrNoise:
		// 已知噪音：不逐行打日志（进程退出时 Err 摘要统一可见 tail）
	case stderrFailure:
		log.Warn("ACP agent stderr 失败信号", zap.String("line", line))
	default:
		log.Debug("ACP agent stderr", zap.String("line", line))
	}
}

// awaitExit 等进程退出并落因：waitErr 写入后关闭 done（后续经 <-Done() 的读取可见）。
func (p *AgentProcess) awaitExit() {
	waitErr := p.cmd.Wait()
	p.mu.Lock()
	p.waitErr = waitErr
	p.mu.Unlock()
	close(p.done)
}

// Done 进程退出信号（close 语义，可在 select 中等待）。
func (p *AgentProcess) Done() <-chan struct{} { return p.done }

// WaitErr 进程退出原因；未退出返回 nil。调用方应在 <-Done() 之后读取。
func (p *AgentProcess) WaitErr() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.waitErr
}

// Kill 强杀整组（幂等：组不存在视为已回收）。不等 Wait；收尾交给 <-Done()。
func (p *AgentProcess) Kill() error {
	return killProcessGroup(p.cmd)
}

// Close 终结进程：整组强杀 + 释放 ctx + 等 Wait（有界，由 WaitDelay 收口）。
func (p *AgentProcess) Close() {
	_ = p.Kill()
	p.cancel()
	<-p.done
}

// exitDetail 进程退出诊断摘要（stderr tail 尾部，超长截断）。
func (p *AgentProcess) exitDetail() string {
	detail := strings.TrimSpace(p.Stderr.Text())
	return truncateRunes(detail, stderrSummaryRunes)
}

// stripEnvKeys 子进程环境剥离键。CLAUDECODE：claude CLI 的嵌套会话守卫标记——
// sidecar 自身跑在 claude 会话内（开发期在 claude 终端里起 server、经 MCP 调起等路径）
// 时，该变量沿 spawn 链传入 adapter 再传给子 claude 会触发嵌套误判；剥离优先级高于
// overrides（调用方无法重新注入），ACP agent 恒以干净环境冷启。
var stripEnvKeys = []string{"CLAUDECODE"}

// mergeEnv sidecar 环境为底、overrides 按键覆盖、stripEnvKeys 恒剥离。先建 map 去重
// 再展开，避免 execve 环境里同键双份导致子进程 getenv 取值不确定。
func mergeEnv(overrides map[string]string) []string {
	env := make(map[string]string, len(os.Environ())+len(overrides))
	for _, kv := range os.Environ() {
		if k, _, ok := strings.Cut(kv, "="); ok {
			env[k] = kv
		}
	}
	for k, v := range overrides {
		env[k] = k + "=" + v
	}
	for _, k := range stripEnvKeys {
		delete(env, k)
	}
	merged := make([]string, 0, len(env))
	for _, v := range env {
		merged = append(merged, v)
	}
	return merged
}
