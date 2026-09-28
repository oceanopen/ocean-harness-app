// Package bot 是 IM 渠道数字人 bot 的渠道无关核心层：入站消息规范化 → 幂等/白名单 →
// 每会话串行队列 → prompt 组装 → claude headless 回合（stream-json 事件流）→ 覆写式流式回复泵。
// 渠道差异（企微/飞书…）全部收窄在 adapter 包（实现本包 channel.go 的 ChannelRuntime 接口），
// 依赖方向单向：adapter → core，core 不 import 任何渠道 SDK。
// claude 引擎（driver_claude.go）属核心层：所有渠道共用同一个会话引擎。
package bot

import (
	"encoding/json"

	"ocean-harness/server/internal/dal/enums"
)

// DefaultAllowedTools 默认工具白名单（--allowedTools）：开发类任务需要 Bash 执行力，
// 安全周界是访问白名单 + 专属工作目录 + acceptEdits（headless 下 Bash 危险命令仍走 claude
// 自身 permission 判定而非自动放行）。按 bot 可收紧（如纯问答 bot 删 Bash）。
func DefaultAllowedTools() []string {
	return []string{"Read", "Glob", "Grep", "Edit", "Write", "Bash", "WebFetch", "WebSearch", "TodoWrite"}
}

// ParseAccessPolicy 解析 access_policy JSON 列；损坏返回错误（调用方 fail closed 拒绝启动）。
func ParseAccessPolicy(raw string) (AccessPolicy, error) {
	var p AccessPolicy
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return AccessPolicy{}, err
	}
	return p, nil
}

// ParseAllowedTools 解析 allowed_tools JSON 列；空/[] 视为采用默认白名单。
func ParseAllowedTools(raw string) ([]string, error) {
	var tools []string
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &tools); err != nil {
			return nil, err
		}
	}
	if len(tools) == 0 {
		return DefaultAllowedTools(), nil
	}
	return tools, nil
}

// ChatType 聊天类型（决定会话粒度与白名单拒绝语义：direct 拒绝回提示、group 拒绝静默）。
type ChatType string

const (
	ChatDirect ChatType = "direct"
	ChatGroup  ChatType = "group"
)

// FormatConversationKey 拼装会话键：单聊 single:<userid>（一人一会话）/ 群聊 group:<chatid>
// （全群共享一个 claude 会话）。前缀自描述，免 chat_type 冗余列；由适配器调用，核心不解析。
func FormatConversationKey(chatType ChatType, id string) string {
	if chatType == ChatGroup {
		return "group:" + id
	}
	return "single:" + id
}

// InboundMessage 渠道入站消息的规范化模型（适配器把渠道帧翻译成它，核心只认这个）。
// 附件必须已由适配器 staging 到本地（Files 只带路径事实，核心不做 I/O）。
// bot 归属不在消息里（HandleInbound 以 BotRuntimeConfig.BotID 为准，单一事实源）。
type InboundMessage struct {
	MessageID       string // 渠道消息 id（幂等键，如企微 msgid）
	ConversationKey string // FormatConversationKey 产物
	ChatType        ChatType
	SenderID        string    // 发送者渠道用户 id（白名单判定 + 引用作者标注）
	Text            string    // 用户正文（不含引用/附件，二者结构化传递）
	Quote           *QuoteRef // 引用消息（未加工；防注入围栏在 prompt.go 统一包装）
	Files           []InboundFile
	Route           RouteInfo // 回执路由（渠道 opaque 值，核心当黑盒原样传回 OpenReply）
}

// QuoteRef 规范化引用三要素。Text 已由适配器按渠道上限截断（Truncated 标注）。
type QuoteRef struct {
	AuthorID  string
	Text      string
	Truncated bool
}

// InboundFile 已落地的附件。Path 为 bot 工作目录内绝对路径（claude 用 Read 等工具按路径读取，
// 不内联 base64——天然绕开 prompt 体积与模型视觉限制）。
type InboundFile struct {
	Path string
	Name string // 原始文件名（manifest 展示用）
}

// RouteInfo 渠道回执路由。核心不解引用 Payload，仅在 OpenReply 时原样传回适配器。
type RouteInfo struct {
	Channel enums.Channel
	Payload any
}

// AccessPolicy 访问白名单（t_im_bots.access_policy JSON 的 Go 形态）。
// 单 scope 设计：单聊/群聊共用一套白名单（按发送者 userid 判定）；JSON 形态将来可加 group
// 键扩展为双 scope，免迁移。解析失败按 fail closed 拒绝（access.go）。
type AccessPolicy struct {
	Mode       string   `json:"mode"`       // "open" | "allowlist"
	AllowUsers []string `json:"allowUsers"` // 渠道 userid，allowlist 模式生效
}

const (
	AccessModeOpen      = "open"
	AccessModeAllowlist = "allowlist"
)

// BotRuntimeConfig supervisor → Factory 的装配参数（t_im_bots 行 + 进程环境的合并视图）。
type BotRuntimeConfig struct {
	BotID        int
	Channel      enums.Channel
	Credential   string // 渠道凭据 JSON，适配器自行解析（核心不解引用）
	WorkspaceDir string
	Model        string // claude --model；空 = CLI 默认
	SystemPrompt string // 人设；组装见 prompt.go ComposeSystemPrompt
	AllowedTools []string
	AccessPolicy AccessPolicy
	Port         int // sidecar HTTP 端口，driver 注入 claude 子进程 OCEAN_HARNESS_PORT
}

// TurnRequest 一次 claude 回合的请求。SessionID 空 = 新会话，非空 = --resume 续聊。
type TurnRequest struct {
	WorkspaceDir string
	SessionID    string
	Prompt       string // prompt.go 组装后的最终 user prompt
	Model        string
	SystemPrompt string // ComposeSystemPrompt 产物
	AllowedTools []string
	Port         int
}

// TurnEventType claude 回合事件类型（driver_claude 解析 stream-json 产出）。
type TurnEventType string

const (
	TurnInit    TurnEventType = "init"     // system/init：携带 session_id（持久化回会话映射）
	TurnText    TurnEventType = "text"     // 文本增量（thinking 增量已在 driver 层过滤）
	TurnToolUse TurnEventType = "tool_use" // 工具调用开始（回复泵置状态行）
	TurnDone    TurnEventType = "done"     // result 终态帧
	TurnError   TurnEventType = "error"    // 进程非零退出 / spawn 失败 / 解析失败
)

// TurnEvent 回合事件。Delta 仅 TurnText 携带；ToolName 仅 TurnToolUse；
// Result/IsError 仅 TurnDone；Err 仅 TurnError（spawn 失败时 RunTurn 同步返回 error 而不发本事件）。
type TurnEvent struct {
	Type      TurnEventType
	SessionID string
	Delta     string
	ToolName  string
	Result    string
	IsError   bool
	Err       error
}
