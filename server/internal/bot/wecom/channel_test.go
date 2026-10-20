package wecom

import (
	"strings"
	"testing"
	"time"

	aibot "github.com/oceanopen/wecom-aibot-go-sdk/aibot"
	aibottypes "github.com/oceanopen/wecom-aibot-go-sdk/aibot/types"

	"ocean-harness/server/internal/bot"
)

/*
 * TestInteractionLaneBypassesInboundQueue 快车道分流（T3.2）：交互事件不排消息队列——
 * 消息通道被未消费任务占满（模拟附件下载积压）时，点击事件仍经独立通道即时到达
 * onInbound（置灰 5 秒窗口不被吃掉）；归一链（会话键/Interaction 注入）与消息同源。
 */
func TestInteractionLaneBypassesInboundQueue(t *testing.T) {
	inbound := make(chan func(*bot.InboundMessage), inboundBufferCapacity)
	actions := make(chan func(*bot.InboundMessage), interactionBufferCapacity)
	stop := make(chan struct{})
	defer close(stop)

	// 消息队列满载：inboundBufferCapacity 个未消费构建任务（不启消费 loop = 模拟下载积压）。
	for i := 0; i < inboundBufferCapacity; i++ {
		inbound <- func(in *bot.InboundMessage) { in.Text = "积压消息" }
	}

	got := make(chan bot.InboundMessage, 1)
	rt := &channelRuntime{
		inbound:   inbound,
		actions:   actions,
		stopProc:  stop,
		onInbound: func(m bot.InboundMessage) { got <- m },
	}
	go rt.processLoop(actions, stop)

	headers := aibottypes.WsFrameHeaders{ReqId: "req-1"}
	base, interaction, ok := cardEventPayload(aibottypes.EventMessage{
		MsgId: "ev-1", AibotId: "bot-1", ChatType: "single",
		From:    aibottypes.EventFrom{UserId: "u1", CorpId: "corp"},
		MsgType: "event",
		Event:   aibottypes.TemplateCardEventData{EventType: "template_card_event", EventKey: "acp-i-1-perm-7:0", TaskId: "acp-i-1-perm-7"},
	})
	if !ok {
		t.Fatal("模板卡事件应解码成功")
	}
	rt.submitInteractionMsg(headers, base, func(in *bot.InboundMessage) { in.Interaction = &interaction })

	select {
	case msg := <-got:
		if msg.ConversationKey != "single:u1" || msg.Text != "" {
			t.Fatalf("交互归一字段不符: %+v", msg)
		}
		if msg.Interaction == nil || msg.Interaction.DeliveryID != "acp-i-1-perm-7" || msg.Interaction.ActionIndex != 0 {
			t.Fatalf("Interaction 注入不符: %+v", msg.Interaction)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("满载消息队列下点击事件应经快车道即时到达（5 秒置灰窗口）")
	}
	if len(inbound) != inboundBufferCapacity {
		t.Fatalf("交互事件不得占用/插队消息队列: %d", len(inbound))
	}
}

/*
 * TestTakeoverRetryExhaustedTerminal 预算耗尽：维持 disconnected 终态，lastError 追加
 * 「已自动重连未成功」提示；耗尽分支同步返回（无等待、无建连）。
 */
func TestTakeoverRetryExhaustedTerminal(t *testing.T) {
	client := aibot.NewWsClient(aibottypes.WsClientOptions{BotId: "b", Secret: "s"})
	rt := &channelRuntime{
		client:          client,
		state:           "disconnected",
		lastErr:         "连接被其他会话接管（同一 botId/Secret 在别处建立了新连接）",
		takeoverRetries: len(takeoverRetryDelays),
	}
	rt.retryTakeover(client)
	st := rt.Status()
	if st.State != "disconnected" || !strings.Contains(st.LastError, "已自动重连 3 次未成功") {
		t.Fatalf("耗尽后应维持终态并改写提示: %+v", st)
	}
}

/*
 * TestTakeoverRetryYieldsToManualStop 手动 Stop 优先：旧代重试在代际守卫处失效——不建连、
 * stopped 终态不被覆盖。
 */
func TestTakeoverRetryYieldsToManualStop(t *testing.T) {
	client := aibot.NewWsClient(aibottypes.WsClientOptions{BotId: "b", Secret: "s"})
	rt := &channelRuntime{client: client, state: "disconnected", lastErr: "被接管"}
	rt.Stop()
	rt.retryTakeover(client)
	if st := rt.Status(); st.State != "stopped" {
		t.Fatalf("手动 Stop 后旧代重试不得复活连接: %+v", st)
	}
}

/*
 * TestTakeoverRetryWaitInterruptedByStop 在途退避等待被 Stop 提前唤醒（等待挂起零轮询），
 * 重试 goroutine 即刻退出不建连。
 */
func TestTakeoverRetryWaitInterruptedByStop(t *testing.T) {
	oldDelays, oldJitter := takeoverRetryDelays, takeoverRetryJitter
	takeoverRetryDelays, takeoverRetryJitter = []time.Duration{30 * time.Second}, time.Duration(0)
	t.Cleanup(func() { takeoverRetryDelays, takeoverRetryJitter = oldDelays, oldJitter })

	client := aibot.NewWsClient(aibottypes.WsClientOptions{BotId: "b", Secret: "s"})
	rt := &channelRuntime{client: client, state: "disconnected"}
	done := make(chan struct{})
	go func() {
		rt.retryTakeover(client)
		close(done)
	}()
	rt.Stop()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop 应提前唤醒在途重连等待")
	}
	if st := rt.Status(); st.State != "stopped" {
		t.Fatalf("唤醒后不得建连: %+v", st)
	}
}
