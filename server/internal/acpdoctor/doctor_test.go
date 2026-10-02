package acpdoctor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	acpgo "github.com/BrokkAi/acp-go"
	"github.com/BrokkAi/acp-go/schema"
	"go.uber.org/zap"

	"ocean-harness/server/internal/acp"
	"ocean-harness/server/internal/agentcatalog"
)

// fakeAgentBin TestMain 现场编译 acp 包的假 agent 二进制（doctor 握手测试复用其
// doctor-happy / happy 脚本，经真实 stdio 跑全链路）。
var fakeAgentBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ocean-acpdoctor-fakeagent-*")
	if err != nil {
		panic("创建临时目录失败: " + err.Error())
	}
	bin := filepath.Join(dir, "fakeagent")
	cmd := exec.Command("go", "build", "-o", bin, "../acp/fakeagent")
	if out, err := cmd.CombinedOutput(); err != nil {
		panic("构建 fakeagent 失败: " + err.Error() + "\n" + string(out))
	}
	fakeAgentBin = bin
	// os.Exit 绕过 defer，临时目录清理须显式收尾后再退出。
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func TestMajorFromVersion(t *testing.T) {
	cases := []struct {
		in     string
		want   int
		parsed bool
	}{
		{"v22.14.0", 22, true},
		{"22.3.1", 22, true},
		{"v22", 22, true},
		{" v22.14.0 ", 22, true},
		{"", 0, false},
		{"v", 0, false},
		{"v-1.2", 0, false},
		{"latest", 0, false},
	}
	for _, c := range cases {
		got, ok := majorFromVersion(c.in)
		if ok != c.parsed || (ok && got != c.want) {
			t.Fatalf("majorFromVersion(%q) = (%d, %v), want (%d, %v)", c.in, got, ok, c.want, c.parsed)
		}
	}
}

func TestCheckNodeMinVersion(t *testing.T) {
	// 通过：等于/高于下限、下限为空（不校验）。
	for _, c := range [][2]string{{"v22.14.0", "22"}, {"v23.1.0", "22"}, {"v22", "22"}, {"v20.0.0", ""}} {
		if reason := checkNodeMinVersion(c[0], c[1]); reason != "" {
			t.Fatalf("checkNodeMinVersion(%q, %q) 应通过，got %q", c[0], c[1], reason)
		}
	}
	// 过低：原因携带实测版本与下限（用户可读，前端直显）。
	if reason := checkNodeMinVersion("v20.11.1", "22"); reason == "" ||
		!strings.Contains(reason, "v20.11.1") || !strings.Contains(reason, "22") {
		t.Fatalf("过低版本原因应含实测版本与下限，got %q", reason)
	}
	// 任一侧无法解析即落败（宁失败不静默）。
	if reason := checkNodeMinVersion("latest", "22"); !strings.Contains(reason, "无法解析") {
		t.Fatalf("版本不可解析应落败，got %q", reason)
	}
	if reason := checkNodeMinVersion("v22", "x"); !strings.Contains(reason, "无法解析") {
		t.Fatalf("下限不可解析应落败，got %q", reason)
	}
}

func TestAuthRequired(t *testing.T) {
	hint, ok := authRequired(&acpgo.RPCError{Code: -32000, Message: "Not authenticated"})
	if !ok || hint != authRequiredHint {
		t.Fatalf("-32000 应命中未登录提示，got (%q, %v)", hint, ok)
	}
	if _, ok := authRequired(&acpgo.RPCError{Code: -32603, Message: "internal"}); ok {
		t.Fatal("-32603 不应命中未登录特判")
	}
	if _, ok := authRequired(errors.New("普通错误")); ok {
		t.Fatal("普通错误不应命中未登录特判")
	}
}

// event helpers 构造等待窗口测试用的事件流载荷。
func commandsEvent(n int) acp.SessionEvent {
	commands := make([]schema.AvailableCommand, n)
	for i := range commands {
		commands[i] = schema.AvailableCommand{Name: "cmd", Description: "d"}
	}
	return acp.SessionEvent{Update: &acpgo.Update{Update: schema.SessionUpdate{
		AvailableCommandsUpdate: &schema.AvailableCommandsUpdate{AvailableCommands: commands},
	}}}
}

func TestWaitAvailableCommandsArrival(t *testing.T) {
	events := make(chan acp.SessionEvent, 3)
	events <- acp.SessionEvent{} // 无关更新（应被排空）
	events <- commandsEvent(2)
	ctx := context.Background()
	n, err := waitAvailableCommands(ctx, events, time.Second)
	if err != nil || n != 2 {
		t.Fatalf("应命中推送并计数，got (%d, %v)", n, err)
	}
}

func TestWaitAvailableCommandsWindowTimeout(t *testing.T) {
	events := make(chan acp.SessionEvent)
	_, err := waitAvailableCommands(context.Background(), events, 30*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "available_commands") {
		t.Fatalf("窗口耗尽应报未收到推送，got %v", err)
	}
}

func TestWaitAvailableCommandsTerminated(t *testing.T) {
	events := make(chan acp.SessionEvent, 1)
	events <- acp.SessionEvent{Terminated: true}
	_, err := waitAvailableCommands(context.Background(), events, time.Second)
	if err == nil || !strings.Contains(err.Error(), "终结") {
		t.Fatalf("会话终结应报错，got %v", err)
	}
}

func TestWaitAvailableCommandsClosed(t *testing.T) {
	events := make(chan acp.SessionEvent)
	close(events)
	_, err := waitAvailableCommands(context.Background(), events, time.Second)
	if err == nil || !strings.Contains(err.Error(), "关闭") {
		t.Fatalf("事件流关闭应报错，got %v", err)
	}
}

func TestCheckEntryStrategyGate(t *testing.T) {
	// doctor 一期只覆盖 npx-adapter 条目；native-acp 在 preflight 之前即短路（不依赖
	// claude/node 环境，确定性断言）。
	report := CheckEntry(context.Background(), agentcatalog.Entry{
		Code:     "x",
		Strategy: agentcatalog.StrategyNativeACP,
	}, Dirs{}, zap.NewNop())
	if report.Status != StatusUnhealthy || report.Stage != StageVendored {
		t.Fatalf("native-acp 应在 vendored 阶段短路，got (%s, %s)", report.Status, report.Stage)
	}
	if !strings.Contains(report.Reason, "暂未支持") {
		t.Fatalf("原因应说明暂未支持，got %q", report.Reason)
	}
}

// handshake probe 集成测试：经真实 stdio 跑假 agent 的最小握手生命周期。

func TestHandshakeProbeDoctorHappy(t *testing.T) {
	cfg := acp.SpawnConfig{Command: []string{fakeAgentBin, "doctor-happy"}, Cwd: t.TempDir()}
	n, stage, err := handshakeProbe(context.Background(), cfg, 5*time.Second, zap.NewNop())
	if err != nil {
		t.Fatalf("doctor-happy 握手应通过，got (stage=%s): %v", stage, err)
	}
	if n != 1 {
		t.Fatalf("应收到 1 条 available_command，got %d", n)
	}
}

func TestHandshakeProbeNoCommands(t *testing.T) {
	// happy 脚本不推送 available_commands：硬门槛落败于 commands 阶段（窗口收窄至 2s）。
	cfg := acp.SpawnConfig{Command: []string{fakeAgentBin, "happy"}, Cwd: t.TempDir()}
	_, stage, err := handshakeProbe(context.Background(), cfg, 2*time.Second, zap.NewNop())
	if err == nil || stage != StageCommands {
		t.Fatalf("无推送应落败于 commands 阶段，got (stage=%s): %v", stage, err)
	}
	if !strings.Contains(err.Error(), "available_commands") {
		t.Fatalf("原因应指向 available_commands 门槛，got %v", err)
	}
}

func TestHandshakeProbeSpawnFailure(t *testing.T) {
	cfg := acp.SpawnConfig{Command: []string{filepath.Join(t.TempDir(), "不存在")}, Cwd: t.TempDir()}
	_, stage, err := handshakeProbe(context.Background(), cfg, time.Second, zap.NewNop())
	if err == nil || stage != StageSpawn {
		t.Fatalf("拉不起进程应落败于 spawn 阶段，got (stage=%s): %v", stage, err)
	}
}

// TestRealHandshakeDoctor 生产链路全真探测（CheckEntry：clibin → node → vendored →
// claude-acp 真握手）。依赖真实环境，双门槛 gating：OCEAN_ACP_REAL_HANDSHAKE=1 且
// GO_SERVER_ACP_RESOURCES_DIR / GO_SERVER_ACP_ADAPTERS_DIR 均已配置（vendoring 资源源
// 与受管根，dev 自测由 scripts/prepare-acp-adapters.ts 产出前者）。
func TestRealHandshakeDoctor(t *testing.T) {
	if os.Getenv("OCEAN_ACP_REAL_HANDSHAKE") != "1" {
		t.Skip("需要 OCEAN_ACP_REAL_HANDSHAKE=1（真实 claude 环境）")
	}
	entry, ok := agentcatalog.GetAgentCatalogInfoByCode("claude-acp")
	if !ok {
		t.Fatal("catalog 缺 claude-acp 条目")
	}
	dirs := Dirs{
		Resources: os.Getenv("GO_SERVER_ACP_RESOURCES_DIR"),
		Adapters:  os.Getenv("GO_SERVER_ACP_ADAPTERS_DIR"),
	}
	if dirs.Resources == "" || dirs.Adapters == "" {
		t.Skip("需要 GO_SERVER_ACP_RESOURCES_DIR / GO_SERVER_ACP_ADAPTERS_DIR")
	}
	report := CheckEntry(context.Background(), entry, dirs, zap.NewNop())
	if report.Status != StatusHealthy {
		t.Fatalf("真实环境应 healthy，got %s（stage=%s reason=%s）", report.Status, report.Stage, report.Reason)
	}
	if report.CommandCount <= 0 {
		t.Fatalf("healthy 应携带正数 available_commands，got %d", report.CommandCount)
	}
	if report.NodeVersion == "" {
		t.Fatal("healthy 应携带探测到的 node 版本")
	}
}
