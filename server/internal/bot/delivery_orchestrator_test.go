package bot

import (
	"context"
	"sync"
	"testing"

	"go.uber.org/zap"
)

/*
 * fakeArtifactReplyStream 产物投递流替身（ArtifactReplyStream 实现）：帧记录之上追加
 * 产物/附言记录（MCP send_file 链路与回合收尾投递的断言入口）。
 */
type fakeArtifactReplyStream struct {
	fakeReplyStream
	mu        sync.Mutex
	artifacts []string // "key|name|path" 拼接
	notices   []string
}

func (f *fakeArtifactReplyStream) SendArtifact(key, name, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.artifacts = append(f.artifacts, key+"|"+name+"|"+path)
	return nil
}

func (f *fakeArtifactReplyStream) SendNotice(text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notices = append(f.notices, text)
	return nil
}

/*
 * deliveryProbeDriver 回合内探针替身：RunTurn 时按请求 token 查注册表并记录命中——
 * 登记/注销窗口均以 runTurn 内部时序为界，探针命中即证明回合内可见。
 */
type deliveryProbeDriver struct {
	reg       *DeliveryRegistry
	mu        sync.Mutex
	req       TurnRequest
	lookupHit bool
}

func (d *deliveryProbeDriver) RunTurn(_ context.Context, req TurnRequest) (<-chan TurnEvent, error) {
	d.mu.Lock()
	d.req = req
	_, d.lookupHit = d.reg.Lookup(req.DeliveryToken)
	d.mu.Unlock()
	events := make(chan TurnEvent, 1)
	events <- TurnEvent{Type: TurnDone, Result: "ok"}
	close(events)
	return events, nil
}

func (d *deliveryProbeDriver) captured() TurnRequest {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.req
}

func (d *deliveryProbeDriver) hit() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lookupHit
}

/*
 * TestDeliveryTokenLifecycle 投递令牌全生命周期：产物渠道回合受理即登记（请求携带
 * token、回合内 Lookup 命中），回合收尾即注销（Lookup 失效）。
 */
func TestDeliveryTokenLifecycle(t *testing.T) {
	deliveries := NewDeliveryRegistry()
	driver := &deliveryProbeDriver{reg: deliveries}
	store := &ConversationStore{DB: newIssueTestDB(t)}
	o := NewOrchestrator(store, driver, nil, nil, zap.NewNop())
	o.Deliveries = deliveries

	cfg := issueCmdCfg()
	cfg.WorkspaceDir = t.TempDir()
	rs := &fakeArtifactReplyStream{}
	o.HandleInbound(cfg, InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "画个图发我"}, func() (ReplyStream, error) {
		return rs, nil
	})
	waitFor(t, "回合执行", func() bool { return driver.captured().Prompt != "" })

	token := driver.captured().DeliveryToken
	if token == "" {
		t.Fatal("产物渠道回合应分配投递令牌")
	}
	if !driver.hit() {
		t.Fatal("回合进行中 Lookup 应命中")
	}
	waitFor(t, "令牌收尾注销", func() bool {
		_, ok := deliveries.Lookup(token)
		return !ok
	})
}

// TestDeliveryTokenAbsentForPlainStream 纯文本渠道不分配令牌（ArtifactReplyStream 探测不命中）。
func TestDeliveryTokenAbsentForPlainStream(t *testing.T) {
	deliveries := NewDeliveryRegistry()
	driver := &recordingDriver{}
	store := &ConversationStore{DB: newIssueTestDB(t)}
	o := NewOrchestrator(store, driver, nil, nil, zap.NewNop())
	o.Deliveries = deliveries

	o.HandleInbound(issueCmdCfg(), InboundMessage{MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1", Text: "接着说"}, func() (ReplyStream, error) {
		return &fakeReplyStream{}, nil
	})
	waitFor(t, "回合执行", func() bool { return driver.captured().Prompt != "" })
	if got := driver.captured().DeliveryToken; got != "" {
		t.Fatalf("纯文本渠道不应分配投递令牌: %q", got)
	}
}
