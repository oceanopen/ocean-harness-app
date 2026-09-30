package bot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// claudeDriver ClaudeDriver 的 headless 实现：每回合 spawn `claude -p`（stream-json 事件流），
// 回合边界即进程生命周期——退出码/result 事件就是完成信号，无挂死进程需要监护；会话历史
// SSOT 在 claude 自身会话存储，--resume 重建，sidecar 重启零状态迁移。
type claudeDriver struct{}

// NewClaudeDriver 构造 claude 引擎。
func NewClaudeDriver() ClaudeDriver { return claudeDriver{} }

// RunTurn 实现见 driver.go 契约。prompt 经 stdin 传入（不经 argv：免转义/长度限制）。
func (claudeDriver) RunTurn(ctx context.Context, req TurnRequest) (<-chan TurnEvent, error) {
	resolved, err := resolveClaudeBin()
	if err != nil {
		return nil, err
	}

	args := []string{
		"-p", // headless print 模式（程序化驱动官方接口）
		"--output-format", "stream-json",
		// stream-json 必须配 --verbose（CLI 强制校验）；逐 token 增量供流式回复泵消费。
		"--verbose", "--include-partial-messages",
		"--permission-mode", "acceptEdits",
	}
	if req.SessionID != "" {
		args = append(args, "--resume", req.SessionID)
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if strings.TrimSpace(req.SystemPrompt) != "" {
		args = append(args, "--append-system-prompt", req.SystemPrompt)
	}
	// 工具白名单恒用默认集（P6：bot 级「执行权限」配置已废弃，权限表达上移 workspace 级
	// launch_settings.permissionMode，ACP 模式接管审批语义；headless 路径安全周界不变）。
	args = append(args, "--allowedTools", strings.Join(DefaultAllowedTools(), ","))

	cmd := exec.CommandContext(ctx, resolved.Bin, args...)
	cmd.Dir = req.WorkspaceDir
	cmd.Env = turnEnv(resolved.LoginPath, req.Port)
	// WaitDelay 收口孙进程管道：ctx 取消只 kill claude 主进程，其工具子进程若仍持有
	// stdout 写端，管道不关则 Wait 永久阻塞（语义同 marketplace/cli.go 的 run）。
	cmd.WaitDelay = 5 * time.Second

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("claude stdin 管道创建失败: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("claude stdout 管道创建失败: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("claude 启动失败: %w", err)
	}
	// prompt 写入独立 goroutine：pipe 缓冲有限，大 prompt 同步写会与读循环互锁死。
	go func() {
		_, _ = io.WriteString(stdin, req.Prompt)
		_ = stdin.Close()
	}()

	events := make(chan TurnEvent, 128)
	go func() {
		defer close(events)
		doneEmitted := false
		reader := bufio.NewReader(stdout)
		for {
			line, readErr := reader.ReadString('\n')
			if line != "" {
				for _, ev := range parseClaudeLine(line) {
					if ev.Type == TurnDone {
						doneEmitted = true
					}
					events <- ev
				}
			}
			if readErr != nil {
				break // EOF（正常退出）或读错误；终态判定交给 Wait
			}
		}
		if waitErr := cmd.Wait(); !doneEmitted {
			events <- TurnEvent{Type: TurnError, Err: turnFailure(waitErr, &stderr)}
		}
	}()
	return events, nil
}

// turnEnv 组装 claude 子进程 env：继承 sidecar env + OCEAN_HARNESS_PORT（插件/CLI 工具链
// 用）。PATH 仅在探测到 login PATH 时替换（nvm 等安装形态；login PATH 是父集、brew 自包含
// 二进制注入无害）——第 1/2 级探测命中时保留原 PATH，否则子进程丢系统路径、Bash 工具全废。
func turnEnv(loginPath string, port int) []string {
	env := os.Environ()
	filtered := env[:0:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, "OCEAN_HARNESS_PORT=") {
			continue
		}
		if loginPath != "" && strings.HasPrefix(kv, "PATH=") {
			continue
		}
		filtered = append(filtered, kv)
	}
	filtered = append(filtered, fmt.Sprintf("OCEAN_HARNESS_PORT=%d", port))
	if loginPath != "" {
		filtered = append(filtered, "PATH="+loginPath)
	}
	return filtered
}

// turnFailure 把 Wait 错误翻译成用户可读的回合失败原因。
func turnFailure(waitErr error, stderr *bytes.Buffer) error {
	if waitErr == nil {
		waitErr = errors.New("claude 回合异常结束（未收到 result 帧）")
	}
	detail := strings.TrimSpace(stderr.String())
	if detail == "" {
		return fmt.Errorf("claude 回合失败: %w", waitErr)
	}
	// stderr 摘要截断（末尾更能定位错误）。
	if rs := []rune(detail); len(rs) > 300 {
		detail = "…" + string(rs[len(rs)-300:])
	}
	return fmt.Errorf("claude 回合失败: %w；stderr: %s", waitErr, detail)
}

// rawClaudeLine stream-json 的最小解析面：只声明消费的字段，其余（usage/cost 等）忽略。
type rawClaudeLine struct {
	Type      string `json:"type"` // system | stream_event | assistant | user | result
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
	Event     *struct {
		Type  string `json:"type"` // content_block_delta 等
		Delta *struct {
			Type string `json:"type"` // text_delta | thinking_delta | …
			Text string `json:"text"`
		} `json:"delta"`
	} `json:"event"`
	Message *struct {
		Content []struct {
			Type string `json:"type"` // text | thinking | tool_use
			Name string `json:"name"` // tool_use 的工具名
		} `json:"content"`
	} `json:"message"`
	Result  string `json:"result"`
	IsError bool   `json:"is_error"`
}

// parseClaudeLine 单行 stream-json → 0..n 个 TurnEvent。schema 漂移时的表现为事件退化
// （text 增量丢失、只剩 assistant 整段/result 兜底），不致挂死。thinking 增量在本层过滤。
func parseClaudeLine(line string) []TurnEvent {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	var raw rawClaudeLine
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil // 非法行（前缀噪声等）静默丢弃
	}
	switch raw.Type {
	case "system":
		if raw.Subtype == "init" && raw.SessionID != "" {
			return []TurnEvent{{Type: TurnInit, SessionID: raw.SessionID}}
		}
	case "stream_event":
		if raw.Event != nil && raw.Event.Type == "content_block_delta" &&
			raw.Event.Delta != nil && raw.Event.Delta.Type == "text_delta" {
			return []TurnEvent{{Type: TurnText, Delta: raw.Event.Delta.Text}}
		}
	case "assistant":
		if raw.Message == nil {
			return nil
		}
		var evs []TurnEvent
		for _, block := range raw.Message.Content {
			if block.Type == "tool_use" && block.Name != "" {
				evs = append(evs, TurnEvent{Type: TurnToolUse, ToolName: block.Name})
			}
		}
		return evs
	case "result":
		return []TurnEvent{{
			Type:      TurnDone,
			Result:    raw.Result,
			SessionID: raw.SessionID,
			IsError:   raw.IsError,
		}}
	}
	return nil
}
