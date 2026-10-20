package acp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ironpark/acp-go/acp1"
)

func permissionFixture() *PendingPermission {
	return newPendingPermission(1, &acp1.RequestPermissionRequest{
		SessionID: "s-1",
		Options:   permissionTestOptions(),
	})
}

func permissionTestOptions() []acp1.PermissionOption {
	return []acp1.PermissionOption{
		{OptionID: "allow", Kind: acp1.PermissionOptionKindAllowOnce, Name: "Allow"},
		{OptionID: "reject", Kind: acp1.PermissionOptionKindRejectOnce, Name: "Reject"},
	}
}

func TestPermissionRespondSelectsOption(t *testing.T) {
	pending := permissionFixture()
	if err := pending.Respond("allow"); err != nil {
		t.Fatalf("Respond(allow) = %v", err)
	}
	outcome := pending.wait(context.Background())
	selected, ok := outcome.Variant().(acp1.RequestPermissionOutcomeSelected)
	if !ok || string(selected.OptionID) != "allow" {
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
	outcome := pending.wait(context.Background())
	selected, ok := outcome.Variant().(acp1.RequestPermissionOutcomeSelected)
	if !ok || string(selected.OptionID) != "reject" {
		t.Fatalf("outcome 应为 selected:reject，got %+v", outcome)
	}
}

func TestPermissionWaitCancelledByCtx(t *testing.T) {
	pending := permissionFixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	outcome := pending.wait(ctx)
	if _, ok := outcome.Variant().(acp1.RequestPermissionOutcomeCancelled); !ok {
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
	selected, ok := outcome.Variant().(acp1.RequestPermissionOutcomeSelected)
	if !ok || string(selected.OptionID) != "allow" {
		t.Fatalf("用户决策先落袋时不应被迟到取消覆写，got %+v", outcome)
	}
}

// elicitationFixture 构造 form session 变体的挂起（typed 全字段，wire 自原始参数解析）。
func elicitationFixture(t *testing.T, id pendingID, sessionID acp1.SessionID) *PendingElicitation {
	t.Helper()
	request, err := acp1.NewCreateElicitationRequest(acp1.CreateElicitationRequestFormSession{
		SessionID:       sessionID,
		Mode:            "form",
		Message:         "请选择一个选项",
		RequestedSchema: acp1.ElicitationSchema{Type: schemaTypeObject(t), Properties: map[string]acp1.ElicitationPropertySchema{}},
	})
	if err != nil {
		t.Fatalf("构造 elicitation 请求: %v", err)
	}
	return newPendingElicitation(id, &request)
}

func schemaTypeObject(t *testing.T) *acp1.ElicitationSchemaType {
	t.Helper()
	object := acp1.ElicitationSchemaTypeObject
	return &object
}

func TestElicitationAcceptAndDecline(t *testing.T) {
	pending := elicitationFixture(t, 2, "s-2")
	if pending.SessionID() != "s-2" {
		t.Fatalf("SessionID 应从 form scope 解析，got %q", pending.SessionID())
	}
	if err := pending.Respond(acp1.NewCreateElicitationResponse(acp1.CreateElicitationResponseDecline{})); err != nil {
		t.Fatalf("Respond(decline) = %v", err)
	}
	response := pending.wait(context.Background())
	if _, ok := response.Variant().(acp1.CreateElicitationResponseDecline); !ok {
		t.Fatalf("应答应为 decline，got %+v", response)
	}
}

func TestElicitationAcceptEchoesContent(t *testing.T) {
	pending := elicitationFixture(t, 4, "s-4")
	answer, err := acp1.NewElicitationContentValue("b")
	if err != nil {
		t.Fatalf("构造应答内容: %v", err)
	}
	if err := pending.Respond(acp1.NewCreateElicitationResponse(acp1.CreateElicitationResponseAccept{
		Content: map[string]acp1.ElicitationContentValue{"choice": answer},
	})); err != nil {
		t.Fatalf("Respond(accept) = %v", err)
	}
	response := pending.wait(context.Background())
	accept, ok := response.Variant().(acp1.CreateElicitationResponseAccept)
	if !ok {
		t.Fatalf("应答应为 accept，got %+v", response)
	}
	choice, err := accept.Content["choice"].As[string]()
	if err != nil || choice != "b" {
		t.Fatalf("accept 内容应回传 choice=b，got %v err %v", accept.Content, err)
	}
}

func TestElicitationAutoSettleIsDecline(t *testing.T) {
	pending := elicitationFixture(t, 3, "s-3")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response := pending.wait(ctx)
	if _, ok := response.Variant().(acp1.CreateElicitationResponseDecline); !ok {
		t.Fatalf("自动收敛应为 decline（Gold-Band 语义），got %+v", response)
	}
}

func TestElicitationSessionIDRequiresSessionScope(t *testing.T) {
	request, err := acp1.NewCreateElicitationRequest(acp1.CreateElicitationRequestFormRequest{
		RequestID:       requestID("r-1"),
		Mode:            "form",
		Message:         "请求级",
		RequestedSchema: acp1.ElicitationSchema{},
	})
	if err != nil {
		t.Fatalf("构造请求级 elicitation: %v", err)
	}
	if _, ok := elicitationSessionID(&request); ok {
		t.Fatal("请求级 scope 不属于任何会话")
	}
	if _, ok := elicitationSessionID(&acp1.CreateElicitationRequest{}); ok {
		t.Fatal("空请求应解析失败")
	}
}

// requestID envelope raw-union 的字符串构造辅助（测试用）。
func requestID(id string) acp1.RequestID {
	rid, err := acp1.NewRequestID(id)
	if err != nil {
		panic(err)
	}
	return rid
}

func TestWireFromElicitationParams(t *testing.T) {
	// form 扁平形态（真适配器 wire）：requestedSchema 等全字段保留。
	form, err := acp1.NewCreateElicitationRequest(acp1.CreateElicitationRequestFormSession{
		SessionID:       "s-1",
		ToolCallID:      toolCallIDPtr("tc-1"),
		Mode:            "form",
		Message:         "选一个",
		RequestedSchema: acp1.ElicitationSchema{Type: schemaTypeObject(t), Properties: map[string]acp1.ElicitationPropertySchema{}},
	})
	if err != nil {
		t.Fatalf("构造 form 请求: %v", err)
	}
	wire := wireFromElicitationParams(&form)
	if wire.Mode != "form" || wire.SessionID != "s-1" || wire.ToolCallID != "tc-1" || wire.Message != "选一个" {
		t.Fatalf("form wire 字段不符，got %+v", wire)
	}
	if !strings.Contains(string(wire.RequestedSchema), `"object"`) {
		t.Fatalf("requestedSchema 应全保真保留，got %s", wire.RequestedSchema)
	}

	// url 扁平形态（一期未广告，到达即兜底，投影字段完整性优先）。
	url, err := acp1.NewCreateElicitationRequest(acp1.CreateElicitationRequestURLSession{
		SessionID:     "s-2",
		ElicitationID: "e-1",
		URL:           "https://example.com/a",
		Mode:          "url",
		Message:       "看这个",
	})
	if err != nil {
		t.Fatalf("构造 url 请求: %v", err)
	}
	wire = wireFromElicitationParams(&url)
	if wire.Mode != "url" || wire.SessionID != "s-2" || wire.URL != "https://example.com/a" || wire.ElicitationID != "e-1" {
		t.Fatalf("url wire 字段不符，got %+v", wire)
	}

	// 未知 mode（custom 变体兜底）：宽松保留（前端按 message+跳过兜底渲染）。
	custom, err := acp1.NewCreateElicitationRequest(acp1.CreateElicitationRequestCustomSession{
		SessionID: "s-3",
		Mode:      "_vendor",
		Message:   "定制形态",
	})
	if err != nil {
		t.Fatalf("构造 custom 请求: %v", err)
	}
	wire = wireFromElicitationParams(&custom)
	if wire.Mode != "_vendor" || wire.Message != "定制形态" {
		t.Fatalf("未知 mode 应宽松保留，got %+v", wire)
	}
	if sessionID, ok := elicitationSessionID(&custom); !ok || sessionID != "s-3" {
		t.Fatalf("custom session 变体应有会话归属，got %q ok=%v", sessionID, ok)
	}
}

func toolCallIDPtr(id string) *acp1.ToolCallID {
	toolCallID := acp1.ToolCallID(id)
	return &toolCallID
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
