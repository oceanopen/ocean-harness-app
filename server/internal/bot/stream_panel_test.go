package bot

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// pumpFrames 预灌事件 → 后台起泵 → 等 ticker 发帧 → 收口，返回帧快照。
func pumpFrames(rs ReplyStream, events chan TurnEvent) []fakeFrame {
	done := pumpAsync(rs, events)
	time.Sleep(2*streamTickInterval + 50*time.Millisecond)
	close(events)
	<-done
	return rs.(*fakeReplyStream).snapshot()
}

// 思考流式期：quote 面板内标签分区，短内容原样直出（无省略号）。
func TestPumpReplyThinkingPanelFrame(t *testing.T) {
	rs := &fakeReplyStream{}
	events := make(chan TurnEvent, 2)
	events <- TurnEvent{Type: TurnThinking, Delta: "先分析"}
	events <- TurnEvent{Type: TurnThinking, Delta: "问题"}

	frames := pumpFrames(rs, events)
	want := "> **🤔 思考中：**\n> 先分析问题"
	if len(frames) != 2 || frames[0].content != want || frames[0].final {
		t.Fatalf("思考流式帧不符: %+v want %q", frames, want)
	}
}

// 长思考流式期取尾部窗口：前缀省略号标识所见非从头。
func TestPumpReplyThinkingTailWindow(t *testing.T) {
	rs := &fakeReplyStream{}
	events := make(chan TurnEvent, 1)
	events <- TurnEvent{Type: TurnThinking, Delta: strings.Repeat("思", thoughtWindowRunes+100)}

	frames := pumpFrames(rs, events)
	want := "> **🤔 思考中：**\n> …" + strings.Repeat("思", thoughtWindowRunes)
	if len(frames) != 2 || frames[0].content != want {
		t.Fatalf("流式期应截尾部窗口: %+v", frames)
	}
}

/*
 * 工具与思考共存：工具到达不清面板、工具分区追加执行行，思考保持尾部窗口跟随。
 */
func TestPumpReplyToolCoexistsWithThinking(t *testing.T) {
	rs := &fakeReplyStream{}
	tail := strings.Repeat("思", thoughtWindowRunes)
	events := make(chan TurnEvent, 2)
	events <- TurnEvent{Type: TurnThinking, Delta: strings.Repeat("思", thoughtWindowRunes+100)}
	events <- TurnEvent{Type: TurnToolUse, ToolName: "Bash"}

	frames := pumpFrames(rs, events)
	want := "> **🤔 思考中：**\n> …" + tail + "\n>\n> **🔧 调用工具：**\n> ⏳ Bash 执行中…"
	if len(frames) != 2 || frames[0].content != want {
		t.Fatalf("工具期应保留思考面板（尾部窗口）+ 工具分区: %+v", frames)
	}
}

// 工具终态事件：面板对应工具行就地结算（✅），不新增分区、不清面板。
func TestPumpReplyToolSettledUpdatesPanel(t *testing.T) {
	rs := &fakeReplyStream{}
	events := make(chan TurnEvent, 3)
	events <- TurnEvent{Type: TurnThinking, Delta: "先分析"}
	events <- TurnEvent{Type: TurnToolUse, ToolName: "Bash"}
	events <- TurnEvent{Type: TurnStatus, Status: "✅ Bash 已完成", ToolName: "Bash"}

	frames := pumpFrames(rs, events)
	want := "> **🤔 思考中：**\n> 先分析\n>\n> **🔧 调用工具：**\n> ✅ Bash 已完成"
	if len(frames) != 2 || frames[0].content != want {
		t.Fatalf("工具终态应就地更新工具行: %+v", frames)
	}
}

// 正文与面板共存：正文不清面板，正文期思考切头部窗口；终帧保留面板 + 正文。
func TestPumpReplyTextCoexistsWithPanelAndFinal(t *testing.T) {
	rs := &fakeReplyStream{}
	events := make(chan TurnEvent, 4)
	events <- TurnEvent{Type: TurnThinking, Delta: "先分析问题"}
	events <- TurnEvent{Type: TurnToolUse, ToolName: "Bash"}
	events <- TurnEvent{Type: TurnText, Delta: "答案"}
	events <- TurnEvent{Type: TurnDone, Result: "答案"}

	frames := pumpFrames(rs, events)
	want := "> **🤔 思考中：**\n> 先分析问题\n>\n> **🔧 调用工具：**\n> ⏳ Bash 执行中…\n\n答案"
	if len(frames) != 2 || frames[0].content != want || frames[0].final {
		t.Fatalf("正文期应面板（头部窗口）与正文共存: %+v", frames)
	}
	if frames[1].content != want || !frames[1].final {
		t.Fatalf("终帧应保留面板 + 正文: %+v", frames)
	}
}

// 正文期思考切头部摘要窗口（尾部 … 标识后续未完），终帧同。
func TestPumpReplyThinkingHeadWindowInTextPhase(t *testing.T) {
	rs := &fakeReplyStream{}
	head := strings.Repeat("思", thoughtWindowRunes)
	events := make(chan TurnEvent, 3)
	events <- TurnEvent{Type: TurnThinking, Delta: strings.Repeat("思", thoughtWindowRunes+100)}
	events <- TurnEvent{Type: TurnText, Delta: "答案"}
	events <- TurnEvent{Type: TurnDone, Result: "答案"}

	frames := pumpFrames(rs, events)
	want := "> **🤔 思考中：**\n> " + head + " …\n\n答案"
	if len(frames) != 2 || frames[0].content != want || frames[1].content != want {
		t.Fatalf("正文期与终帧应取头部摘要: %+v", frames)
	}
}

/*
 * 挂起状态行与面板共存：quote 临时段附面板之后，内容推进让位、终帧不带状态行。
 */
func TestPumpReplyStatusCoexistsWithPanel(t *testing.T) {
	rs := &fakeReplyStream{}
	events := make(chan TurnEvent, 1)
	events <- TurnEvent{Type: TurnThinking, Delta: "先分析"}
	done := pumpAsync(rs, events)
	time.Sleep(2*streamTickInterval + 50*time.Millisecond)
	events <- TurnEvent{Type: TurnStatus, Status: "⏳ Agent 请求审批…"}
	time.Sleep(2*streamTickInterval + 50*time.Millisecond)
	close(events)
	<-done

	frames := rs.snapshot()
	if len(frames) != 3 {
		t.Fatalf("应恰三帧（面板/面板+状态行/终帧）: %+v", frames)
	}
	if frames[0].content != "> **🤔 思考中：**\n> 先分析" || frames[0].final {
		t.Fatalf("首帧应为思考面板: %+v", frames[0])
	}
	want := "> **🤔 思考中：**\n> 先分析\n\n> ⏳ Agent 请求审批…"
	if frames[1].content != want || frames[1].final {
		t.Fatalf("状态行应与面板共存（quote 段附后）: %+v want %q", frames[1], want)
	}
}

// 失败终帧保留面板：思考痕迹不随失败丢失，失败文案在面板之后。
func TestPumpReplyErrorFinalKeepsPanel(t *testing.T) {
	rs := &fakeReplyStream{}
	events := make(chan TurnEvent, 2)
	events <- TurnEvent{Type: TurnThinking, Delta: "先分析"}
	events <- TurnEvent{Type: TurnError, Err: errors.New("boom")}

	frames := pumpFrames(rs, events)
	want := "> **🤔 思考中：**\n> 先分析\n\n❌ 处理失败：boom\n请重试或调整后重新发送。"
	if len(frames) != 2 || frames[0].final || frames[1].content != want {
		t.Fatalf("失败终帧应保留面板 + 失败文案: %+v want %q", frames, want)
	}
}

// plainReplyStream 无 markdown 能力的纯文本渠道替身（覆写能力探测为 false）。
type plainReplyStream struct {
	fakeReplyStream
}

func (p *plainReplyStream) MarkdownFrames() bool { return false }

// 纯文本渠道降级：无 quote 块、无粗体标签，面板分区与正文结构保留。
func TestPumpReplyPlainChannelDegrades(t *testing.T) {
	rs := &plainReplyStream{}
	events := make(chan TurnEvent, 4)
	events <- TurnEvent{Type: TurnThinking, Delta: "先分析问题"}
	events <- TurnEvent{Type: TurnToolUse, ToolName: "Bash"}
	events <- TurnEvent{Type: TurnText, Delta: "答案"}
	events <- TurnEvent{Type: TurnDone, Result: "答案"}
	done := pumpAsync(rs, events)
	time.Sleep(2*streamTickInterval + 50*time.Millisecond)
	close(events)
	<-done

	frames := rs.snapshot()
	want := "🤔 思考中：\n先分析问题\n\n🔧 调用工具：\n⏳ Bash 执行中…\n\n答案"
	if len(frames) != 2 || frames[1].content != want || !frames[1].final {
		t.Fatalf("纯文本渠道应降级为无 quote/粗体帧: %+v want %q", frames, want)
	}
}
