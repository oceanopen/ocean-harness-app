// Package acpdoctor ACP agent doctor 探测（T1.4）：以「真实握手跑通」为可用性判据——
// preflight 短路链（node 解析与版本下限 → vendored 安装 → vendored claude 二进制定位）
// 后真拉起子进程跑 initialize → session/new → 等 available_commands 推送（硬门槛）再清理。
// 依赖方向：本包 → acp + agentcatalog（不反向，维持 agentcatalog → acp 单向；clibin 的
// login PATH 经 acp 注入间接消费）。
// 结论三态（healthy/unhealthy/unknown）的进程内缓存、受理单飞与启动后台探测见 state.go。
package acpdoctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	acpgo "github.com/BrokkAi/acp-go"
	"go.uber.org/zap"

	"ocean-harness/server/internal/acp"
	"ocean-harness/server/internal/agentcatalog"
)

const (
	// probeBudget 单条目探测总预算：preflight 毫秒级（login shell 首探数百毫秒），
	// 真握手各步上限（spawn/initialize/session 60s + 命令窗口 10s + 回收 5s）被本预算
	// 硬截断——探测必在预算内出结论，不存在无限挂起。异步受理模型下仅影响后台探测时长。
	probeBudget = 90 * time.Second

	// commandsWindow available_commands 推送等待窗口（硬门槛）：claude-acp 在 session/new
	// 后即刻推送，通常 <1s 到达；等不到即 unhealthy（推送时序变化属 agent 侧回归，如实暴露）。
	commandsWindow = 10 * time.Second
)

// Status 条目四态（HTTP 投影形态）：healthy/unhealthy/unknown 是探测结论三态，checking
// 是「受理在跑」的派生态（由 Snapshot 推导，永不写入 Report）。
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusUnhealthy Status = "unhealthy"
	StatusUnknown   Status = "unknown" // 从未探测 ≠ 探测失败（三态语义，UI 引导按态区分）
	StatusChecking  Status = "checking"
)

// Stage 失败阶段：unhealthy 时的定位面（preflight 三项 + 握手四步）。
type Stage string

const (
	StageClaude     Stage = "claude"     // vendored 自带 claude 二进制定位（P5 定稿终态）
	StageNode       Stage = "node"       // node 解析与版本下限
	StageVendored   Stage = "vendored"   // vendored 安装与入口翻译
	StageSpawn      Stage = "spawn"      // 握手：子进程拉起
	StageInitialize Stage = "initialize" // 握手：initialize
	StageSession    Stage = "session"    // 握手：session/new
	StageCommands   Stage = "commands"   // 握手：available_commands 推送（硬门槛）
)

// authRequiredHint claude 未登录的统一提示（initialize/session 阶段 -32000 特判）。
const authRequiredHint = "claude 未登录：请先在终端运行 claude 完成登录，再重新检测"

// Report 单条目探测结论快照（结论只存 healthy/unhealthy/unknown；探测进行中由
// Snapshot 以 checking 呈现，不写本结构）。
type Report struct {
	AgentCode string
	Status    Status
	CheckedAt time.Time // 结论时刻（失败同样是结论）；unknown 时零值
	Stage     Stage     // unhealthy 时非空
	Reason    string    // unhealthy 时的用户可读原因（后端生成，前端直显）
	// CommandCount healthy 时收到的 available_commands 数量（握手附带收益的信息面）。
	CommandCount int
	// NodeVersion 探测到的 node 版本（探测项 2 通过后有值）。
	NodeVersion string
}

// Dirs vendoring 目录对（config 的 AcpResourcesDir / AcpAdaptersDir，均可选空 = 未配置；
// 空值在 EnsureVendored 调用期显式报错，落为 unhealthy 而非绕过）。
type Dirs struct {
	Resources string
	Adapters  string
}

// CheckEntry 对单个 catalog 条目执行完整探测（preflight 短路链 + 真握手），返回结论。
// 纯函数：不触碰进程内缓存与在跑集合（state.go 职责）；总预算 probeBudget 内必出结论。
func CheckEntry(ctx context.Context, entry agentcatalog.Entry, dirs Dirs, log *zap.Logger) Report {
	if log == nil {
		log = zap.NewNop()
	}
	ctx, cancel := context.WithTimeout(ctx, probeBudget)
	defer cancel()

	report := Report{AgentCode: entry.Code, Status: StatusUnhealthy}
	fail := func(stage Stage, format string, args ...any) Report {
		report.Status = StatusUnhealthy
		report.CheckedAt = time.Now()
		report.Stage = stage
		report.Reason = fmt.Sprintf(format, args...)
		return report
	}

	// 一期 doctor 仅覆盖 npx-adapter 条目（P4 收窄：native-acp 随 catalog 扩展启用）。
	if entry.Strategy != agentcatalog.StrategyNpxAdapter {
		return fail(StageVendored, "策略 %s 的 doctor 探测暂未支持（随 catalog 扩展启用）", entry.Strategy)
	}

	// 探测项 1：node 可用 + 版本下限（复用 vendored spawn 同一份解析，单一 SSOT）。
	nodeBin, err := agentcatalog.ResolveNodeBin()
	if err != nil {
		return fail(StageNode, "%v", err)
	}
	version, err := nodeVersion(ctx, nodeBin)
	if err != nil {
		return fail(StageNode, "node 版本探测失败: %v", err)
	}
	report.NodeVersion = version
	if reason := checkNodeMinVersion(version, entry.NodeMinVersion); reason != "" {
		return fail(StageNode, "%s", reason)
	}

	// 探测项 2：vendored 安装（EnsureVendored 幂等；目录未配置在此显式落败，
	// 与生产拉起路径零差异——「能探测通过」即「能按生产链路拉起」）。
	installDir, err := agentcatalog.EnsureVendored(entry, dirs.Resources, dirs.Adapters)
	if err != nil {
		return fail(StageVendored, "%v", err)
	}

	// 探测项 3：vendored 自带 claude 二进制定位（P5 定稿终态：claude 消费 vendored
	// 自带二进制，无独立本机探测项；文案 agentcatalog 已用户可读）。
	claudeBin, err := agentcatalog.VendoredClaudeBin(installDir)
	if err != nil {
		return fail(StageClaude, "%v", err)
	}

	// 探测会话 cwd 用一次性临时目录（会话目录内容无消费方，收尾即清）。
	cwd, err := os.MkdirTemp("", "ocean-acp-doctor-")
	if err != nil {
		return fail(StageVendored, "创建探测临时目录: %v", err)
	}
	defer os.RemoveAll(cwd)
	cfg, err := entry.VendoredSpawnConfig(installDir, cwd)
	if err != nil {
		return fail(StageVendored, "%v", err)
	}
	// claude 路径一致性注入（T1.3/P5 定稿终态）：CLAUDE_CODE_EXECUTABLE = vendored
	// 二进制 + login PATH，消费方拼装。
	if cfg.Env, err = acp.ClaudeEnvOverrides(cfg.Env, claudeBin); err != nil {
		return fail(StageClaude, "%v", err)
	}

	// 真握手：initialize → session/new → available_commands（硬门槛）。
	commands, stage, err := handshakeProbe(ctx, cfg, commandsWindow, log)
	if err != nil {
		return fail(stage, "%v", err)
	}
	report.Status = StatusHealthy
	report.CheckedAt = time.Now()
	report.CommandCount = commands
	return report
}

// handshakeProbe 真握手最小生命周期（CheckEntry 与假 agent 集成测试共用的纯握手段，
// 不含 preflight）：spawn → initialize → session/new → 等 available_commands 推送。
// window 为推送等待窗口（生产传 commandsWindow，测试收窄）。返回命令数量；失败返回
// 阶段与已用户可读的错误（进程异常退出时附 Err() 证据摘要）。
func handshakeProbe(ctx context.Context, cfg acp.SpawnConfig, window time.Duration, log *zap.Logger) (int, Stage, error) {
	client, err := acp.Launch(ctx, cfg, log)
	if err != nil {
		return 0, StageSpawn, err
	}
	defer client.Close()

	if _, err := client.Initialize(ctx); err != nil {
		if hint, authed := authRequired(err); authed {
			return 0, StageInitialize, errors.New(hint)
		}
		return 0, StageInitialize, withProcessEvidence(client, fmt.Errorf("ACP initialize 失败: %w", err))
	}
	session, err := client.NewSession(ctx, acp.NewSessionParams{})
	if err != nil {
		if hint, authed := authRequired(err); authed {
			return 0, StageSession, errors.New(hint)
		}
		return 0, StageSession, withProcessEvidence(client, fmt.Errorf("ACP 建会话失败: %w", err))
	}
	commands, err := waitAvailableCommands(ctx, session.Events(), window)
	if err != nil {
		return 0, StageCommands, err
	}
	return commands, "", nil
}

// authRequired 判定握手步失败是否为 -32000（claude 未登录），是则给出统一用户提示
// （纯函数，单元可测——假 agent 侧无法回指定错误码）。
func authRequired(err error) (string, bool) {
	if acpgo.IsAuthRequired(err) {
		return authRequiredHint, true
	}
	return "", false
}

// waitAvailableCommands 硬门槛等待：排空事件流直至 available_commands 推送到达（持续
// 排空是 events.go 的背压契约——慢消费会拖住连接层读循环），窗口耗尽/会话终结/取消即败。
func waitAvailableCommands(ctx context.Context, events <-chan acp.SessionEvent, window time.Duration) (int, error) {
	timer := time.NewTimer(window)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("探测已取消: %w", ctx.Err())
		case <-timer.C:
			return 0, fmt.Errorf("会话建立后未在 %s 内收到 available_commands 推送", window)
		case event, ok := <-events:
			if !ok {
				return 0, errors.New("会话事件流意外关闭")
			}
			if event.Terminated {
				return 0, errors.New("会话在等待 available_commands 期间终结")
			}
			if event.Update != nil && event.Update.Update.AvailableCommandsUpdate != nil {
				return len(event.Update.Update.AvailableCommandsUpdate.AvailableCommands), nil
			}
		}
	}
}

// withProcessEvidence 握手失败时追加进程死亡证据（stderr 摘要，Err() 在主动 Close 前
// 可查；进程仍在收尾时 Err() 为 nil 则原样返回）。
func withProcessEvidence(client *acp.AgentClient, err error) error {
	if derr := client.Err(); derr != nil {
		return fmt.Errorf("%w（进程诊断: %v）", err, derr)
	}
	return err
}

// nodeVersion 执行 `node --version`（以解析到的绝对路径执行，防 PATH 漂移），返回形如
// v22.14.0 的版本串。
func nodeVersion(ctx context.Context, nodeBin string) (string, error) {
	out, err := exec.CommandContext(ctx, nodeBin, "--version").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// checkNodeMinVersion 主版本下限校验：返回空串 = 通过；非空 = 用户可读原因。
// min 为空 = 不校验（catalog 条目语义）；任一侧无法解析即落败（宁失败不静默，
// 与 catalog parse 同哲学——产物缺陷不该被静默放行）。
func checkNodeMinVersion(version, min string) string {
	if min == "" {
		return ""
	}
	major, ok := majorFromVersion(version)
	if !ok {
		return fmt.Sprintf("node 版本输出无法解析: %q", version)
	}
	minMajor, ok := majorFromVersion(min)
	if !ok {
		return fmt.Sprintf("catalog nodeMinVersion 无法解析: %q", min)
	}
	if major < minMajor {
		return fmt.Sprintf("node 版本过低（%s）：ACP 模式需要 Node.js ≥%s，或切换终端模式", version, min)
	}
	return ""
}

// majorFromVersion 取主版本号（容忍 v 前缀与任意后缀，如 v22.14.0 → 22）；无法解析
// 返回 false。
func majorFromVersion(version string) (int, bool) {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if dot := strings.IndexByte(v, '.'); dot >= 0 {
		v = v[:dot]
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}
