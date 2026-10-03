package acp

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	acpgo "github.com/BrokkAi/acp-go"
	"github.com/BrokkAi/acp-go/schema"
)

func permissionFixture() *PendingPermission {
	return newPendingPermission(1, schema.RequestPermissionRequest{
		SessionID: "s-1",
		Options:   permissionTestOptions(),
	})
}

func permissionTestOptions() []schema.PermissionOption {
	return []schema.PermissionOption{
		{OptionID: "allow", Kind: schema.PermissionOptionKindAllowOnce, Name: "Allow"},
		{OptionID: "reject", Kind: schema.PermissionOptionKindRejectOnce, Name: "Reject"},
	}
}

func TestPermissionRespondSelectsOption(t *testing.T) {
	pending := permissionFixture()
	if err := pending.Respond("allow"); err != nil {
		t.Fatalf("Respond(allow) = %v", err)
	}
	outcome := pending.wait(context.Background())
	if outcome.Selected == nil || string(outcome.Selected.OptionID) != "allow" {
		t.Fatalf("outcome 应为 selected:allow，got %+v", outcome)
	}
	if err := pending.Respond("reject"); !errors.Is(err, ErrAlreadyResolved) {
		t.Fatalf("重复应答应 ErrAlreadyResolved，got %v", err)
	}
}

func TestPermissionUnknownOptionKeepsCAS(t *testing.T) {
	pending := permissionFixture()
	err := pending.Respond("nope")
	if !errors.Is(err, ErrUnknownOption) {
		t.Fatalf("未知选项应 ErrUnknownOption，got %v", err)
	}
	if err := pending.Respond("reject"); err != nil {
		t.Fatalf("未知选项不应消耗 CAS 名额，got %v", err)
	}
	if outcome := pending.wait(context.Background()); outcome.Selected == nil || string(outcome.Selected.OptionID) != "reject" {
		t.Fatalf("outcome 应为 selected:reject，got %+v", outcome)
	}
}

func TestPermissionWaitCancelledByCtx(t *testing.T) {
	pending := permissionFixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	outcome := pending.wait(ctx)
	if outcome.Cancelled == nil {
		t.Fatalf("ctx 取消应收敛为 cancelled，got %+v", outcome)
	}
	// cancel 走的 CAS：之后的人类应答应被拒绝
	if err := pending.Respond("allow"); !errors.Is(err, ErrAlreadyResolved) {
		t.Fatalf("取消后再应答应 ErrAlreadyResolved，got %v", err)
	}
}

func TestPermissionUserDecisionWinsOverLateCtxCancel(t *testing.T) {
	pending := permissionFixture()
	if err := pending.Respond("allow"); err != nil {
		t.Fatalf("Respond: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	outcome := pending.wait(ctx)
	if outcome.Selected == nil || string(outcome.Selected.OptionID) != "allow" {
		t.Fatalf("用户决策先落袋时不应被迟到取消覆写，got %+v", outcome)
	}
}

func TestElicitationAcceptAndDecline(t *testing.T) {
	pending := newPendingElicitation(2, schema.CreateElicitationRequest{
		Message: "选择",
		Form: &schema.ElicitationFormMode{
			Session: &schema.ElicitationSessionScope{SessionID: "s-2"},
		},
	}, nil)
	if pending.SessionID() != "s-2" {
		t.Fatalf("SessionID 应从 form scope 解析，got %q", pending.SessionID())
	}
	if err := pending.Respond(acpgo.AcceptElicitation(map[string]schema.ElicitationContentValue{"choice": "b"})); err != nil {
		t.Fatalf("Respond(accept) = %v", err)
	}
	response := pending.wait(context.Background())
	if response.Accept == nil || response.Accept.Content["choice"] != "b" {
		t.Fatalf("应答应为 accept{choice:b}，got %+v", response)
	}
}

func TestElicitationAutoSettleIsDecline(t *testing.T) {
	pending := newPendingElicitation(3, schema.CreateElicitationRequest{
		Form: &schema.ElicitationFormMode{Session: &schema.ElicitationSessionScope{SessionID: "s-3"}},
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response := pending.wait(ctx)
	if response.Decline == nil {
		t.Fatalf("自动收敛应为 decline（Gold-Band 语义），got %+v", response)
	}
}

func TestElicitationSessionIDRequiresFormSessionScope(t *testing.T) {
	if _, ok := elicitationSessionID(schema.CreateElicitationRequest{Message: "无 scope"}); ok {
		t.Fatal("无 form scope 应解析失败")
	}
	if _, ok := elicitationSessionID(schema.CreateElicitationRequest{
		Form: &schema.ElicitationFormMode{Request: &schema.ElicitationRequestScope{RequestID: "r-1"}},
	}); ok {
		t.Fatal("请求级 scope 不属于任何会话")
	}
}

func TestParseElicitationWire(t *testing.T) {
	// form 扁平形态（真适配器 wire）：requestedSchema 全保真保留。
	wire, err := parseElicitationWire([]byte(`{"mode":"form","sessionId":"s-1","toolCallId":"tc-1","message":"选一个","requestedSchema":{"type":"object","properties":{"choice":{"type":"string"}}}}`))
	if err != nil {
		t.Fatalf("form wire 解析: %v", err)
	}
	if wire.Mode != "form" || wire.SessionID != "s-1" || wire.ToolCallID != "tc-1" || wire.Message != "选一个" {
		t.Fatalf("form wire 字段不符，got %+v", wire)
	}
	if !bytes.Contains(wire.RequestedSchema, []byte(`"choice"`)) {
		t.Fatalf("requestedSchema 应原样保留，got %s", wire.RequestedSchema)
	}

	// url 扁平形态（一期未广告，到达即兜底，投影字段完整性优先）。
	wire, err = parseElicitationWire([]byte(`{"mode":"url","sessionId":"s-2","message":"看这个","url":"https://example.com/a","elicitationId":"e-1"}`))
	if err != nil {
		t.Fatalf("url wire 解析: %v", err)
	}
	if wire.Mode != "url" || wire.SessionID != "s-2" || wire.URL != "https://example.com/a" || wire.ElicitationID != "e-1" {
		t.Fatalf("url wire 字段不符，got %+v", wire)
	}

	// 未知 mode：宽松保留（前端按 message+跳过兜底渲染）。
	wire, err = parseElicitationWire([]byte(`{"mode":"_vendor","message":"定制形态"}`))
	if err != nil {
		t.Fatalf("未知 mode 解析: %v", err)
	}
	if wire.Mode != "_vendor" || wire.Message != "定制形态" {
		t.Fatalf("未知 mode 应宽松保留，got %+v", wire)
	}

	// 非对象参数：解析失败（拦截层降级，不阻断交互）。
	if _, err := parseElicitationWire([]byte(`"str"`)); err == nil {
		t.Fatal("非对象参数应解析失败")
	}
}

func TestWireFromRequestFallback(t *testing.T) {
	// typed form scope：message + form 标记 + 归属可还原；requestedSchema 不可恢复。
	toolCallID := schema.ToolCallId("tc-9")
	wire := WireFromRequest(schema.CreateElicitationRequest{
		Message: "降级",
		Form: &schema.ElicitationFormMode{Session: &schema.ElicitationSessionScope{
			SessionID: "s-1", ToolCallID: &toolCallID,
		}},
	})
	if wire.Mode != "form" || wire.Message != "降级" || wire.SessionID != "s-1" || wire.ToolCallID != "tc-9" {
		t.Fatalf("form 降级投影不符，got %+v", wire)
	}
	if wire.RequestedSchema != nil {
		t.Fatalf("typed 模型无 schema，降级不应编造，got %s", wire.RequestedSchema)
	}

	// url mode（scope 在 URL 侧）。
	wire = WireFromRequest(schema.CreateElicitationRequest{
		URL: &schema.ElicitationUrlMode{Session: &schema.ElicitationSessionScope{SessionID: "s-2"}},
	})
	if wire.Mode != "url" || wire.SessionID != "s-2" {
		t.Fatalf("url 降级投影不符，got %+v", wire)
	}
}

func TestNewPendingElicitationWireSessionFallback(t *testing.T) {
	// typed 双空（url 载荷被上游丢弃后的残形）+ wire 带归属：归属回落 wire。
	pending := newPendingElicitation(7, schema.CreateElicitationRequest{Message: "残形"}, &ElicitationWire{Mode: "url", SessionID: "s-7"})
	if pending.SessionID() != "s-7" {
		t.Fatalf("归属应回落 wire.SessionID，got %q", pending.SessionID())
	}
	if pending.Wire() == nil || pending.Wire().Mode != "url" {
		t.Fatalf("wire 应原样携带，got %+v", pending.Wire())
	}
}

func TestSettleIsIdempotent(t *testing.T) {
	pending := permissionFixture()
	if err := pending.cancel(); err != nil {
		t.Fatalf("首次 cancel = %v", err)
	}
	if err := pending.cancel(); !errors.Is(err, ErrAlreadyResolved) {
		t.Fatalf("重复 cancel 应 ErrAlreadyResolved，got %v", err)
	}
	// wait 不应阻塞：decided 已关闭
	done := make(chan struct{})
	go func() {
		pending.wait(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("已结算后 wait 应立即返回")
	}
}
