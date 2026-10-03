package acp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	acpgo "github.com/BrokkAi/acp-go"
	"github.com/BrokkAi/acp-go/schema"
	"go.uber.org/zap"
)

// fakeAgentBin TestMain 现场编译的假 agent 二进制（真实 stdio + agent.New().Serve）。
var fakeAgentBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ocean-acp-fakeagent-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "创建临时目录失败:", err)
		os.Exit(1)
	}
	bin := filepath.Join(dir, "fakeagent")
	cmd := exec.Command("go", "build", "-o", bin, "./fakeagent")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "构建 fakeagent 失败: %v\n%s", err, out)
		os.Exit(1)
	}
	fakeAgentBin = bin
	// os.Exit 绕过 defer，临时目录清理须显式收尾后再退出。
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// launchFake 拉起指定脚本的假 agent 并完成 握手 + 建会话 全链路。
func launchFake(t *testing.T, script string) (*AgentClient, *Session) {
	t.Helper()
	client, err := Launch(context.Background(), SpawnConfig{
		Command: []string{fakeAgentBin, script},
		Cwd:     t.TempDir(),
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("Launch(%s): %v", script, err)
	}
	t.Cleanup(client.Close)
	if _, err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	session, err := client.NewSession(context.Background(), NewSessionParams{})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	return client, session
}

func chunkText(event SessionEvent) (string, bool) {
	if event.Update != nil && event.Update.Update.AgentMessageChunk != nil {
		return event.Update.Update.AgentMessageChunk.Content.Text.Text, true
	}
	return "", false
}

// waitForChunk 排空事件流直至出现含 substr 的 message chunk（事件顺序即 wire 顺序）。
func waitForChunk(t *testing.T, events <-chan SessionEvent, substr string) string {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case event := <-events:
			if text, ok := chunkText(event); ok && strings.Contains(text, substr) {
				return text
			}
		case <-deadline:
			t.Fatalf("等待包含 %q 的 chunk 超时", substr)
		}
	}
}

// startDrain 后台持续排空事件流（慢回合防背压拖死读循环），stop 后可读已积累 chunk。
func startDrain(events <-chan SessionEvent) (chunks func() []string, stop func()) {
	mu := sync.Mutex{}
	var texts []string
	stopCh := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case event := <-events:
				if event.Terminated {
					return
				}
				if text, ok := chunkText(event); ok {
					mu.Lock()
					texts = append(texts, text)
					mu.Unlock()
				}
			case <-stopCh:
				return
			}
		}
	}()
	return func() []string {
			mu.Lock()
			defer mu.Unlock()
			return append([]string(nil), texts...)
		}, func() {
			close(stopCh)
			<-done
		}
}

type promptResult struct {
	stop schema.StopReason
	err  error
}

// promptAsync 异步发起回合（供取消类测试在回合进行中插入操作）。
func promptAsync(client *AgentClient, session *Session, text string) <-chan promptResult {
	done := make(chan promptResult, 1)
	go func() {
		stop, err := client.PromptText(context.Background(), session, text)
		done <- promptResult{stop: stop, err: err}
	}()
	return done
}

// waitPermission 阻塞等下一个权限审批事件。
func waitPermission(t *testing.T, events <-chan SessionEvent) *PendingPermission {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case event := <-events:
			if event.Permission != nil {
				return event.Permission
			}
		case <-deadline:
			t.Fatal("等待权限审批事件超时")
		}
	}
}

func waitPrompt(t *testing.T, results <-chan promptResult) promptResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(20 * time.Second):
		t.Fatal("回合未在时限内收敛")
		return promptResult{}
	}
}

func TestLaunchHappyPath(t *testing.T) {
	client, session := launchFake(t, "happy")
	stop, err := client.PromptText(context.Background(), session, "打声招呼")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if stop != schema.StopReasonEndTurn {
		t.Fatalf("stopReason = %q, want end_turn", stop)
	}
	// 两段 chunk 按序到达（wire 顺序保持）
	if text := waitForChunk(t, session.Events(), "你好，"); !strings.HasPrefix(text, "你好，") {
		t.Fatalf("首个 chunk 应为「你好，」开头，got %q", text)
	}
	waitForChunk(t, session.Events(), "ACP 回合完成")

	// 收敛断言：主动 Close 后进程组已回收（二次 Kill 幂等返回 ErrProcessDone）
	client.Close()
	select {
	case <-client.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("进程未在时限内退出（进程组回收失效）")
	}
	if err := client.proc.Kill(); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("Close 后二次 Kill 应 ErrProcessDone，got %v", err)
	}
	if err := client.Err(); err != nil {
		t.Fatalf("主动收尾后 Err 应为 nil，got %v", err)
	}
}

func TestErrAfterUnexpectedDeath(t *testing.T) {
	client, _ := launchFake(t, "happy")
	// 绕过 Close 直接整组强杀：模拟 agent 意外死亡（OOM kill / 崩溃）。
	if err := client.proc.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	select {
	case <-client.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("进程未在时限内退出")
	}
	// 意外死亡证据面：Err 必须非 nil 且携带异常退出语义（主动收尾才是 nil，
	// 见 TestLaunchHappyPath 的对照断言）。
	err := client.Err()
	if err == nil {
		t.Fatal("意外死亡后 Err 应返回非 nil 错误")
	}
	if !strings.Contains(err.Error(), "异常退出") {
		t.Fatalf("Err 应携带异常退出语义，got %v", err)
	}
}

func TestPermissionAllow(t *testing.T) {
	client, session := launchFake(t, "permission")
	results := promptAsync(client, session, "需要权限")

	pending := waitPermission(t, session.Events())
	if err := pending.Respond("allow"); err != nil {
		t.Fatalf("Respond(allow): %v", err)
	}
	if err := pending.Respond("allow"); !errors.Is(err, ErrAlreadyResolved) {
		t.Fatalf("重复应答应 ErrAlreadyResolved，got %v", err)
	}
	if result := waitPrompt(t, results); result.err != nil || result.stop != schema.StopReasonEndTurn {
		t.Fatalf("回合应 end_turn 收敛，got %+v", result)
	}
	if text := waitForChunk(t, session.Events(), "permission:selected:allow"); text == "" {
		t.Fatal("agent 未收到 allow 决策回显")
	}
}

func TestPermissionReject(t *testing.T) {
	client, session := launchFake(t, "permission")
	results := promptAsync(client, session, "需要权限")

	pending := waitPermission(t, session.Events())
	if err := pending.Respond("reject"); err != nil {
		t.Fatalf("Respond(reject): %v", err)
	}
	waitPrompt(t, results)
	if text := waitForChunk(t, session.Events(), "permission:selected:reject"); text == "" {
		t.Fatal("agent 未收到 reject 决策回显")
	}
}

func TestPermissionCancelledByTurnCancel(t *testing.T) {
	client, session := launchFake(t, "permission")
	results := promptAsync(client, session, "需要权限")

	pending := waitPermission(t, session.Events())
	client.CancelTurn(session)
	// 取消赢过迟到的用户应答：结算后的 CAS 拒绝新应答
	if err := pending.Respond("allow"); !errors.Is(err, ErrAlreadyResolved) {
		t.Fatalf("取消后应答应 ErrAlreadyResolved，got %v", err)
	}
	// 软取消以干净终态收敛（agent 侧 host 请求随回合被取消，无决策回显）
	if result := waitPrompt(t, results); result.err != nil || result.stop != schema.StopReasonCancelled {
		t.Fatalf("软取消应返回 stopReason=cancelled，got %+v", result)
	}
}

func TestPermissionNoActiveTurn(t *testing.T) {
	client, session := launchFake(t, "late-permission")
	if _, err := client.PromptText(context.Background(), session, "立刻结束"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	// 回合已结束后 agent 才发起权限请求：无活动回合路径直接 cancelled，不投递事件。
	sawPermission := false
	deadline := time.After(15 * time.Second)
loop:
	for {
		select {
		case event := <-session.Events():
			if event.Permission != nil {
				sawPermission = true
				continue
			}
			if text, ok := chunkText(event); ok && strings.Contains(text, "late-permission:") {
				if text != "late-permission:cancelled" {
					t.Fatalf("无活动回合的权限请求应被 cancelled，got %q", text)
				}
				break loop
			}
		case <-deadline:
			t.Fatal("等待 late-permission 回显超时")
		}
	}
	if sawPermission {
		t.Fatal("无活动回合的权限请求不应投递审批事件")
	}
}

func TestElicitationAccept(t *testing.T) {
	client, session := launchFake(t, "elicitation")
	results := promptAsync(client, session, "要一个输入")

	deadline := time.After(15 * time.Second)
	var pending *PendingElicitation
	for pending == nil {
		select {
		case event := <-session.Events():
			if event.Elicitation != nil {
				pending = event.Elicitation
			}
		case <-deadline:
			t.Fatal("等待 elicitation 事件超时")
		}
	}
	if pending.Request().Message != "请选择一个选项" {
		t.Fatalf("请求文案不符，got %q", pending.Request().Message)
	}
	if err := pending.Respond(acpgo.AcceptElicitation(map[string]schema.ElicitationContentValue{"choice": "b"})); err != nil {
		t.Fatalf("Respond(accept): %v", err)
	}
	waitPrompt(t, results)
	if text := waitForChunk(t, session.Events(), "elicitation:accept:choice=b"); text == "" {
		t.Fatal("agent 未收到 accept 内容回显")
	}
}

func TestCancelTurnSoftCancel(t *testing.T) {
	client, session := launchFake(t, "slow-cancel")
	chunks, stopDrain := startDrain(session.Events())
	defer stopDrain()

	results := promptAsync(client, session, "跑起来")
	// 等首个 tick 落袋再取消（确认回合已进入推进态）
	deadline := time.After(15 * time.Second)
	for {
		found := false
		for _, text := range chunks() {
			if strings.HasPrefix(text, "tick-") {
				found = true
			}
		}
		if found {
			break
		}
		select {
		case <-time.After(20 * time.Millisecond):
		case <-deadline:
			t.Fatal("等待首个 tick 超时")
		}
	}
	client.CancelTurn(session)
	result := waitPrompt(t, results)
	if result.err != nil || result.stop != schema.StopReasonCancelled {
		t.Fatalf("软取消应返回 stopReason=cancelled，got %+v", result)
	}
}

func TestSetPermissionMode(t *testing.T) {
	client, session := launchFake(t, "happy")
	if err := client.SetPermissionMode(context.Background(), session, PermissionModeAcceptEdits); err != nil {
		t.Fatalf("SetPermissionMode(acceptEdits): %v", err)
	}
	// 本地模式视图经 current_mode_update 通知回写（异步，轮询等待）
	deadline := time.After(10 * time.Second)
	for {
		if modes := session.Modes(); modes != nil && modes.CurrentModeID == "acceptEdits" {
			break
		}
		select {
		case <-time.After(20 * time.Millisecond):
		case <-deadline:
			t.Fatal("current_mode_update 未回写本地模式视图")
		}
	}
	// 目录外的值本地拒（不发请求），错误列出可用值
	err := client.SetPermissionMode(context.Background(), session, "bogus")
	if err == nil || !strings.Contains(err.Error(), "可用") {
		t.Fatalf("目录外的模式应本地拒绝并列出可用值，got %v", err)
	}
}

func TestCloseDuringActiveTurn(t *testing.T) {
	client, session := launchFake(t, "slow-cancel")
	results := promptAsync(client, session, "跑起来")

	// 活动回合中直接全量收尾：Close 必须有界返回（进程组回收 + 连接 join）
	closed := make(chan struct{})
	go func() {
		client.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(20 * time.Second):
		t.Fatal("活动回合中 Close 未有界收敛")
	}
	result := waitPrompt(t, results)
	if result.err == nil {
		t.Fatalf("进程被终结后回合应以错误返回，got %+v", result)
	}
	select {
	case <-client.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("进程未在时限内退出")
	}
}

func TestNewSessionMetaPassthrough(t *testing.T) {
	client, _ := launchFake(t, "happy")
	session, err := client.NewSession(context.Background(), NewSessionParams{
		Meta: schema.Meta{"custom": "value"},
	})
	if err != nil {
		t.Fatalf("带 Meta 建会话: %v", err)
	}
	if session.ID() == "" {
		t.Fatal("sessionId 不应为空")
	}
}

func TestEarlyFrameReplay(t *testing.T) {
	client, session := launchFake(t, "early-push")
	// session/new 响应前抢跑推送的 chunk 经早到帧缓冲回放：首个事件即 early（不再丢弃）
	if text := waitForChunk(t, session.Events(), "early"); text != "early" {
		t.Fatalf("早到帧应最先回放，got %q", text)
	}
	// 回放不影响后续 wire 顺序：prompt 推进正常
	if _, err := client.PromptText(context.Background(), session, "继续"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	waitForChunk(t, session.Events(), "late")
}
