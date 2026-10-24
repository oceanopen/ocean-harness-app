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

	"ocean-harness/server/internal/acp"
	"ocean-harness/server/internal/agentcatalog"
	"ocean-harness/server/internal/clibin"
	"ocean-harness/server/internal/dal/enums"
)

/*
 * claudeDriver TurnDriver 的 headless 实现：每回合 spawn `claude -p`（stream-json 事件流），
 * 回合边界即进程生命周期——退出码/result 事件就是完成信号，无挂死进程需要监护；会话历史
 * SSOT 在 claude 自身会话存储，--resume 重建，sidecar 重启零状态迁移。
 */
type claudeDriver struct{}

// NewClaudeDriver 构造 claude headless 引擎。
func NewClaudeDriver() TurnDriver { return claudeDriver{} }

/*
 * claudeRuntimeBin 解析 vendored 自带 claude（全链路 SSOT）：vendoring 两目录由 main
 * 启动期注入 agentcatalog（本包不依赖 global——global → bot 依赖方向不可逆），
 * 成功进程级缓存在 agentcatalog 内。
 */
func claudeRuntimeBin() (string, error) {
	return agentcatalog.ResolveVendoredClaudeBin(string(enums.AGENT_CODE_CLAUDE_ACP))
}

// RunTurn 实现见 driver.go 契约。prompt 经 stdin 传入（不经 argv：免转义/长度限制）。
func (claudeDriver) RunTurn(ctx context.Context, req TurnRequest) (<-chan TurnEvent, error) {
	bin, err := claudeRuntimeBin()
	if err != nil {
		return nil, err
	}
	args, cleanup, err := buildClaudeArgs(req)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = req.WorkspaceDir
	/*
	 * login PATH best-effort（探测失败保留原 PATH）：vendored 自带 claude 为原生二进制
	 * 不依赖 node，但 claude 派生的工具子进程（Bash 工具跑 npm 等）仍需要用户环境。
	 */
	loginPath, _ := clibin.LoginPath()
	cmd.Env = turnEnv(loginPath, req.Port)
	/*
	 * WaitDelay 收口孙进程管道：ctx 取消只 kill claude 主进程，其工具子进程若仍持有
	 * stdout 写端，管道不关则 Wait 永久阻塞（语义同 marketplace/cli.go 的 run）。
	 */
	cmd.WaitDelay = 5 * time.Second

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("claude stdin 管道创建失败: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("claude stdout 管道创建失败: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		cleanup()
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
		defer cleanup() // 临时 MCP 配置文件随进程收尾删除
		doneEmitted := false
		st := &turnParseState{toolNames: map[string]string{}}
		reader := bufio.NewReader(stdout)
		for {
			line, readErr := reader.ReadString('\n')
			if line != "" {
				for _, ev := range parseClaudeLine(line, st) {
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

/*
 * buildClaudeArgs 组装 claude argv。带投递令牌时注入 per-turn MCP 配置（im 投递端点的
 * send_file 工具）；返回的 cleanup 删除临时配置文件，调用方保证在 claude 进程收尾后执行。
 */
func buildClaudeArgs(req TurnRequest) (args []string, cleanup func(), err error) {
	args = []string{
		"-p", // headless print 模式（程序化驱动官方接口）
		"--output-format", "stream-json",
		// stream-json 必须配 --verbose（CLI 强制校验）；逐 token 增量供流式回复泵消费。
		"--verbose", "--include-partial-messages",
		/*
		 * headless 为全自动执行形态（无审批交互）：恒跳过权限询问，入口安全闸是
		 * IM 访问白名单（access_policy），工具白名单不做裁剪。
		 */
		"--dangerously-skip-permissions",
	}
	if req.SessionID != "" {
		args = append(args, "--resume", req.SessionID)
	}
	if strings.TrimSpace(req.SystemPrompt) != "" {
		args = append(args, "--append-system-prompt", req.SystemPrompt)
	}
	cleanup = func() {}
	if req.DeliveryToken != "" {
		/*
		 * 回合内文件投递：MCP server「im」指向本 sidecar 投递端点，模型经 send_file
		 * 即时推文件给用户（独立气泡，不占流）；配置文件进程私有，收尾删除。
		 */
		path, werr := writeIMMcpConfig(req.Port, req.DeliveryToken)
		if werr != nil {
			return nil, func() {}, werr
		}
		cleanup = func() { _ = os.Remove(path) }
		args = append(args, "--mcp-config", path)
	}
	return args, cleanup, nil
}

/*
 * writeIMMcpConfig 写 per-turn 临时 MCP 配置：单 server「im」指向本 sidecar 的 im 投递
 * 端点（token 即回合投递注册键）。落 os.TempDir，删除由调用方 cleanup 负责。
 */
func writeIMMcpConfig(port int, token string) (string, error) {
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"im": map[string]any{
				"type": "http",
				"url":  fmt.Sprintf("http://127.0.0.1:%d/mcp/streamableHttp/im/%s", port, token),
			},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "ocean-im-mcp-*.json")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

/*
 * turnEnv 组装 claude 子进程 env：继承 sidecar env + OCEAN_HARNESS_PORT（插件/CLI 工具链
 * 用）。PATH 仅在拿到 login PATH 时替换（nvm 等安装形态；login PATH 是父集、brew 自包含
 * 二进制注入无害）——探测失败保留原 PATH，否则子进程丢系统路径、Bash 工具全废。
 */
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
	// vendored 版本锁死：禁 claude CLI 自更新（SSOT 同 acp.ClaudeEnvOverrides）。
	filtered = append(filtered, acp.DisableAutoupdateEnv+"=1")
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
			Type     string `json:"type"` // text_delta | thinking_delta | …
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
		} `json:"delta"`
	} `json:"event"`
	Message *struct {
		Content []struct {
			Type      string `json:"type"`        // text | thinking | tool_use | tool_result
			Name      string `json:"name"`        // tool_use 的工具名
			ID        string `json:"id"`          // tool_use 的调用 id（tool_result 终态翻译锚）
			ToolUseID string `json:"tool_use_id"` // tool_result 对应的 tool_use id
			IsError   bool   `json:"is_error"`    // tool_result 失败标记
		} `json:"content"`
	} `json:"message"`
	Result  string `json:"result"`
	IsError bool   `json:"is_error"`
}

/*
 * turnParseState 回合内解析状态（RunTurn reader goroutine 私有，单线程消费）：工具调用
 * id→名映射（tool_result 终态翻译锚）与思考累计全量（差分水位线，同 ACP 驱动语义）。
 */
type turnParseState struct {
	toolNames   map[string]string
	thought     string
	thoughtSent string
}

/*
 * parseClaudeLine 单行 stream-json → 0..n 个 TurnEvent。schema 漂移时的表现为事件退化
 * （text 增量丢失、只剩 assistant 整段/result 兜底），不致挂死。thinking 增量转 TurnThinking
 * 增量事件（面板组帧在泵侧）；tool_result 经 id→名映射转带工具名的终态事件（泵就地结算面板工具行）。
 */
func parseClaudeLine(line string, st *turnParseState) []TurnEvent {
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
		if raw.Event == nil || raw.Event.Type != "content_block_delta" || raw.Event.Delta == nil {
			return nil
		}
		switch raw.Event.Delta.Type {
		case "text_delta":
			return []TurnEvent{{Type: TurnText, Delta: raw.Event.Delta.Text}}
		case "thinking_delta":
			// 思考增量差分：累计全量 + 水位线仅在有新增长时发增量事件（ACP 驱动同款语义）。
			st.thought += raw.Event.Delta.Thinking
			delta, nsent := diffWatermark(st.thought, st.thoughtSent)
			st.thoughtSent = nsent
			if delta != "" {
				return []TurnEvent{{Type: TurnThinking, Delta: delta}}
			}
			return nil
		}
	case "assistant":
		if raw.Message == nil {
			return nil
		}
		var evs []TurnEvent
		for _, block := range raw.Message.Content {
			if block.Type == "tool_use" && block.Name != "" {
				if block.ID != "" {
					st.toolNames[block.ID] = block.Name
				}
				evs = append(evs, TurnEvent{Type: TurnToolUse, ToolName: block.Name})
			}
		}
		return evs
	case "user":
		/*
		 * 工具终态：stream-json 事件只达一次，无需去重；id 未注册（schema 漂移防御）
		 * 回落中性名。
		 */
		if raw.Message == nil {
			return nil
		}
		var evs []TurnEvent
		for _, block := range raw.Message.Content {
			if block.Type != "tool_result" {
				continue
			}
			name := st.toolNames[block.ToolUseID]
			if name == "" {
				name = "工具"
			}
			evs = append(evs, TurnEvent{Type: TurnStatus, Status: toolSettledText(name, block.IsError), ToolName: name})
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
