package bot

import (
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

// 思考面板流式期：标题 + quote 块，短内容原样直出（无省略号）。
func TestPumpReplyThinkingPanelFrame(t *testing.T) {
	rs := &fakeReplyStream{}
	events := make(chan TurnEvent, 2)
	events <- TurnEvent{Type: TurnThinking, Delta: "先分析"}
	events <- TurnEvent{Type: TurnThinking, Delta: "问题"}

	frames := pumpFrames(rs, events)
	want := "**🤔 思考中**\n> 先分析问题"
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
	want := "**🤔 思考中**\n> …" + strings.Repeat("思", thoughtWindowRunes)
	if len(frames) != 2 || frames[0].content != want {
		t.Fatalf("流式期应截尾部窗口: %+v", frames)
	}
}

/*
 * 工具与思考共存：工具到达不清思考面板，思考切头部窗口（尾部 … 标识后续未完），
 * 工具行换行追加其后。
 */
func TestPumpReplyToolCoexistsWithThinking(t *testing.T) {
	rs := &fakeReplyStream{}
	head := strings.Repeat("思", thoughtWindowRunes)
	events := make(chan TurnEvent, 2)
	events <- TurnEvent{Type: TurnThinking, Delta: strings.Repeat("思", thoughtWindowRunes+100)}
	events <- TurnEvent{Type: TurnToolUse, ToolName: "Bash"}

	frames := pumpFrames(rs, events)
	want := "**🤔 思考中**\n> " + head + " …\n\n🔧 正在执行 Bash …"
	if len(frames) != 2 || frames[0].content != want {
		t.Fatalf("工具期应保留思考面板（头部窗口）+ 工具行: %+v want %q", frames, want)
	}
}

// 正文到达清思考面板：帧让位正文，面板不出现在任何帧。
func TestPumpReplyTextYieldsPanel(t *testing.T) {
	rs := &fakeReplyStream{}
	events := make(chan TurnEvent, 4)
	events <- TurnEvent{Type: TurnThinking, Delta: "先分析问题"}
	events <- TurnEvent{Type: TurnToolUse, ToolName: "Bash"}
	events <- TurnEvent{Type: TurnText, Delta: "答案"}
	events <- TurnEvent{Type: TurnDone, Result: "答案"}

	frames := pumpFrames(rs, events)
	if len(frames) != 2 || frames[0].content != "答案" || frames[0].final || !frames[1].final {
		t.Fatalf("正文应独占帧面（面板已让位，正文中间帧 + 终帧）: %+v", frames)
	}
}

// 覆写型状态行（审批/工具终态）清面板：面板帧之后被状态行整体覆写。
func TestPumpReplyStatusOverwritesPanel(t *testing.T) {
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
		t.Fatalf("应恰三帧（面板/状态行/终帧）: %+v", frames)
	}
	if frames[0].content != "**🤔 思考中**\n> 先分析" || frames[0].final {
		t.Fatalf("首帧应为思考面板: %+v", frames[0])
	}
	if frames[1].content != "⏳ Agent 请求审批…" || frames[1].final {
		t.Fatalf("状态行应整体覆写面板: %+v", frames[1])
	}
}
