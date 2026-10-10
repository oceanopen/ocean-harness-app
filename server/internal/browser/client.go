package browser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"

	"ocean-harness/server/internal/acp"
	"ocean-harness/server/internal/agentcatalog"
	"ocean-harness/server/internal/buildinfo"
)

// 引擎拉起与 go-sdk stdio 桥接（T1.2）。
//
// 拉起链：EnsureVendored → node 预检（≥18）→ vendored 入口翻译 → argv 组装 →
// exec.Cmd 只配 SysProcAttr/StderrPipe（CommandTransport 的 Connect 内自行 Start，
// 本文件不得自行 Start——go-sdk v1.8.0 源码级复核结论）→ CommandTransport + Connect。
//
// 进程退出守护：go session.Wait()（连接关闭即返回——进程死亡 stdout EOF 亦触发）→
// 记录原因供上层（T1.3 manager）置 failed。注意 cmd.Wait 已被 go-sdk pipeRWC.Close
// （session.Close() 触达）独占调用，守护方不得自行 cmd.Wait（双 Wait 冲突）。

const (
	// nodeMinMajor @playwright/mcp 的 engines 口径（node≥18）。
	nodeMinMajor = 18

	// stderrTailCapacity 引擎 stderr tail 容量（对齐 acp 包 64KiB 先例）。
	stderrTailCapacity = 64 << 10

	// pidFileName 引擎 pid 文件名（user-data-dir 根部，T1.3 recoverOrphans 杀组目标）。
	pidFileName = ".engine.pid"
)

// Dirs vendoring 目录对（语义同 acpsession.Dirs / acpdoctor.Dirs：config 的
// AcpResourcesDir / AcpAdaptersDir，未配置在拉起期经 EnsureVendored 显式报错）。
type Dirs struct {
	Resources string
	Adapters  string
}

// 测试替换点（spawnFunc 惯例，对齐 acpsession/acpdoctor）。
var (
	// resolveNodeBin node 绝对路径解析（生产 = agentcatalog.ResolveNodeBin SSOT）。
	resolveNodeBin = agentcatalog.ResolveNodeBin
	// nodeVersionOf 执行 node --version（生产 = agentcatalog.NodeVersion SSOT；测试
	// 注入假版本）。
	nodeVersionOf = agentcatalog.NodeVersion
	// dialEngine cmd → MCP 会话连接（生产 = CommandTransport；测试注入 in-memory
	// transport 假引擎，cmd 不真正启动）。
	dialEngine = defaultDialEngine
)

// defaultDialEngine 生产连接：CommandTransport stdio 桥接（Connect 三参形态，内部
// 完成 Start 与 initialize 握手）。
func defaultDialEngine(ctx context.Context, cmd *exec.Cmd) (*mcp.ClientSession, error) {
	transport := &mcp.CommandTransport{Command: cmd}
	client := mcp.NewClient(&mcp.Implementation{Name: "ocean-harness-browser", Version: buildinfo.Version}, nil)
	return client.Connect(ctx, transport, nil)
}

// EngineHandle 一个引擎子进程会话的宿主（per-profile 单实例，生命周期由上层编排）。
type EngineHandle struct {
	Profile    string
	ProfileDir string
	PidFile    string

	cmd     *exec.Cmd
	session *mcp.ClientSession
	stderr  *acp.StderrTail

	closedByCaller atomic.Bool   // 主动 Close 已受理（上层归因意外死亡 vs 主动收尾）
	exited         chan struct{} // 连接终结后关闭
	exitMu         sync.Mutex
	exitErr        error
}

// Session 返回 MCP 会话（CallTool/ListTools 用；连接终结后调用返回错误）。
func (h *EngineHandle) Session() *mcp.ClientSession { return h.session }

// StderrTail 引擎 stderr tail（启动失败/崩溃诊断证据）。
func (h *EngineHandle) StderrTail() *acp.StderrTail { return h.stderr }

// Exited 连接终结信号（close 语义，可 select）。上层（T1.3 manager）据此置 failed。
func (h *EngineHandle) Exited() <-chan struct{} { return h.exited }

// ClosedByCaller 主动 Close 是否已受理（上层据此区分意外死亡与主动收尾）。
func (h *EngineHandle) ClosedByCaller() bool { return h.closedByCaller.Load() }

// ExitErr 连接终结原因（应在 <-Exited() 之后读取）。
func (h *EngineHandle) ExitErr() error {
	h.exitMu.Lock()
	defer h.exitMu.Unlock()
	return h.exitErr
}

// LaunchEngine 拉起 per-profile 引擎会话。headless 为本次启动的无头终值（偏好读取与
// 覆写回存归上层 manager 编排，D10）。cwd 取 profile 下载目录（引擎 --output-dir 已
// 显式传值，cwd 仅兜底）。
func LaunchEngine(ctx context.Context, dirs Dirs, profile string, headless bool, log *zap.Logger) (*EngineHandle, error) {
	if log == nil {
		log = zap.NewNop()
	}
	profileDir, err := ProfileDir(profile)
	if err != nil {
		return nil, err
	}
	downloadDir, err := DownloadsDir(profile)
	if err != nil {
		return nil, err
	}
	// user-data-dir 正常由 Chrome 自建（不存在则建，空目录无冲突），但 pid 文件在
	// Connect 成功后立即落盘、Chrome 拉起是异步的——先行自建目录。
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建 profile 目录: %w", err)
	}

	// node 预检：@playwright/mcp engines 口径 node≥18。
	nodeBin, err := resolveNodeBin()
	if err != nil {
		return nil, fmt.Errorf("浏览器引擎拉起: %w", err)
	}
	version, err := nodeVersionOf(ctx, nodeBin)
	if err != nil {
		return nil, err
	}
	if reason := agentcatalog.CheckNodeMinVersion(version, strconv.Itoa(nodeMinMajor)); reason != "" {
		return nil, errors.New(reason)
	}

	// vendored 引擎安装（幂等）+ 入口翻译（node <包 bin 入口>）。
	entry, err := EngineEntry()
	if err != nil {
		return nil, err
	}
	installDir, err := agentcatalog.EnsureVendored(entry, dirs.Resources, dirs.Adapters)
	if err != nil {
		return nil, fmt.Errorf("浏览器引擎 vendored 安装: %w", err)
	}
	cfg, err := entry.VendoredSpawnConfig(installDir, downloadDir)
	if err != nil {
		return nil, fmt.Errorf("浏览器引擎入口解析: %w", err)
	}

	// argv = node <vendored 入口> <引擎 flags>；cmd 只配进程组与 stderr（Connect 内
	// 自行 Start，不得提前）。Env 应用条目覆盖（当前 EngineEntry 无 Env，即继承
	// sidecar 环境不变；带 Env 的未来条目不静默失效）。
	argv := append(append([]string{}, cfg.Command...), EngineSpawnArgs(profileDir, downloadDir, headless)...)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cfg.Cwd
	cmd.Env = acp.MergeEnv(cfg.Env)
	cmd.SysProcAttr = acp.ProcessGroupAttr()
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("浏览器引擎 stderr 管道创建失败: %w", err)
	}

	h := &EngineHandle{
		Profile:    profile,
		ProfileDir: profileDir,
		PidFile:    filepath.Join(profileDir, pidFileName),
		cmd:        cmd,
		stderr:     acp.NewStderrTail(stderrTailCapacity),
		exited:     make(chan struct{}),
	}
	go acp.PumpStderr(stderrPipe, h.stderr, log)

	session, err := dialEngine(ctx, cmd)
	if err != nil {
		// 拨号失败就地补杀整组：go-sdk Connect 失败只杀主进程不杀组，且此时 pid 文件
		// 尚未落盘、recoverOrphans 无锚点——不补杀会留下无锚点引擎 + 可能的有头
		// Chrome 孤儿（cmd.Process 为 nil 的 fake-dial 测试路径幂等跳过）。
		_ = acp.KillProcessGroup(cmd)
		return nil, fmt.Errorf("浏览器引擎连接失败: %w%s", err, dialFailureSuffix(profileDir, h.stderr.Text()))
	}
	h.session = session

	// pid 文件（T1.3 recoverOrphans 杀组目标）。测试注入 in-memory 引擎时 cmd 未启动，
	// 跳过。
	if cmd.Process != nil {
		if err := os.WriteFile(h.PidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
			_ = session.Close()
			_ = acp.KillProcessGroup(cmd)
			return nil, fmt.Errorf("写入引擎 pid 文件: %w", err)
		}
	}

	// 退出守护：session.Wait() 返回（连接关闭即返回——进程死亡 stdout EOF 亦触发）→
	// 记录原因，上层经 <-Exited() 感知后置 failed。不得自行 cmd.Wait（pipeRWC.Close
	// 已独占）。
	go func() {
		err := session.Wait()
		h.exitMu.Lock()
		h.exitErr = err
		h.exitMu.Unlock()
		close(h.exited)
	}()
	return h, nil
}

// dialFailureSuffix 拉起失败的诊断后缀（探测不参与拉起决策，只丰富错误归因）：
// profile 残留锁（优先，更具体）→ Chrome 缺失 → 引擎 stderr tail 证据，统一组装一处。
func dialFailureSuffix(profileDir, stderrText string) string {
	hints := dialFailureHints(profileDir)
	if detail := strings.TrimSpace(stderrText); detail != "" {
		hints = append(hints, "引擎 stderr: "+detail)
	}
	if len(hints) == 0 {
		return ""
	}
	return " " + strings.Join(hints, " ")
}

// dialFailureHints 探测类提示（profile 残留锁优先于 Chrome 缺失——更具体）。
func dialFailureHints(profileDir string) []string {
	var hints []string
	if hint := profileLockHint(profileDir); hint != "" {
		hints = append(hints, hint)
	}
	if hint := chromeMissingHint(); hint != "" {
		hints = append(hints, hint)
	}
	return hints
}

// Close 终结引擎：session.Close() 优雅关停（关 stdin → 5s → SIGTERM → SIGKILL，只杀
// 主进程；T1.1 实测 stdin EOF 即自退并自收浏览器）→ KillProcessGroup 补杀整组（纵深
// 防御：防未知挂死态与 Windows 未实测场景）→ 删 pid 文件。幂等可重入。
func (h *EngineHandle) Close() error {
	h.closedByCaller.Store(true)
	var err error
	if h.session != nil {
		err = h.session.Close()
	}
	_ = acp.KillProcessGroup(h.cmd)
	_ = os.Remove(h.PidFile)
	return err
}
