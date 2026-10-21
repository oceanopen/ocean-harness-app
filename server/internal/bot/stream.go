package bot

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

/*
 * streamTickInterval 流式推送节奏（协议固定编排，非时间性补丁）：企微流式中间帧 ack 有约 5s
 * 固有延迟，200ms ticker 单写者既保证刷新密度又自动跳过积压帧（wecom-aibot-go-sdk
 * examples/agent 同款节奏）。
 */
const streamTickInterval = 200 * time.Millisecond

/*
 * OverflowSink 超长全文落盘回调：回复超过渠道 ByteLimit 时被调用一次，回调负责把全文写入
 * 持久位置并返回展示在截断提示里的路径（如 "<workspace>/.bot-outbox/<ts>-<msgid>.md"）。
 * 返回空串则截断提示不附路径。落盘放编排器（知道 msgid/工作目录），泵只负责拼帧。
 */
type OverflowSink func(fullText string) string

/*
 * TurnOutcome 一次回合推送的终态。SessionID 为 init/result 捕获的 claude 会话 id
 * （可能为空，如 spawn 即失败）；Err 非 nil = 回合失败（终帧已写失败文案）。
 */
type TurnOutcome struct {
	FinalText string
	SessionID string
	Err       error
}

/*
 * PumpReply 回复泵：消费 TurnEvent 流 → 覆写式流式回复；主 goroutine 消费事件、ticker 唯一周期写者、终帧由主 goroutine 收口（两写者一次交接）。
 * 帧形态：quote 面板（思考+工具两大段，整回合累计不清）+ quote 挂起状态行 + 正文共存；终帧保留面板。
 * 思考分区窗口：思考/工具期尾部跟随，正文期与终帧头部摘要。卡随 tick 一次性消费，收尾未发出的卡直接丢弃（挂起随回合终态结算）。
 */
func PumpReply(ctx context.Context, rs ReplyStream, events <-chan TurnEvent, sink OverflowSink, log *zap.Logger) TurnOutcome {
	var mu sync.Mutex
	var statusText, thinkingBuf, currentText, sessionID string
	var headWindow bool // 思考分区窗口方向：正文期头部摘要 / 思考工具期尾部跟随
	var tools []toolEntry
	var pendingCard *CardSpec
	var turnErr error
	var isError bool

	/*
	 * 卡能力探测一次：headless 引擎不产卡、纯文本渠道不实现 CardReplyStream，两种场景都
	 * 无需分支处理（无卡即回落 Flush）。
	 */
	cardStream, hasCard := rs.(CardReplyStream)

	// 渲染能力探测一次：无 markdown 能力渠道降级纯文本帧（quote/粗体剥离）。
	useMarkdown := false
	if mfs, ok := rs.(MarkdownFrameStream); ok {
		useMarkdown = mfs.MarkdownFrames()
	}

	snapshot := func() string {
		mu.Lock()
		defer mu.Unlock()
		return composeFrame(panelContent(thinkingBuf, headWindow, tools, useMarkdown), statusText, currentText, useMarkdown)
	}

	// takePendingCard 取走待发卡（一次性消费语义的收口点）。
	takePendingCard := func() *CardSpec {
		mu.Lock()
		defer mu.Unlock()
		card := pendingCard
		pendingCard = nil
		return card
	}

	done := make(chan struct{})
	tickerDone := make(chan struct{})
	go func() {
		defer close(tickerDone)
		ticker := time.NewTicker(streamTickInterval)
		defer ticker.Stop()
		var lastSent string
		for {
			select {
			case <-ticker.C:
				cur := truncateForFrame(snapshot(), rs.ByteLimit(), nil)
				if cur != "" && cur != lastSent {
					if card := takePendingCard(); card != nil && hasCard {
						/*
						 * 卡随状态行一并下发（渠道侧自定帧形态，无卡渠道的降级文案不丢失）；
						 * 发送失败回落纯文本帧（卡通道故障不吞状态行）。
						 */
						err := cardStream.SendCard(*card, cur, false)
						if err == nil {
							lastSent = cur
							continue
						}
						log.Warn("bot 审批卡发送失败（回落文本帧）", zap.Error(err))
					}
					if err := rs.Flush(cur, false); err != nil {
						log.Warn("bot 流式中间帧发送失败（跳过）", zap.Error(err))
					}
					lastSent = cur
				}
			case <-done:
				return
			}
		}
	}()

	for ev := range events {
		switch ev.Type {
		case TurnInit:
			mu.Lock()
			sessionID = ev.SessionID
			mu.Unlock()
		case TurnText:
			mu.Lock()
			currentText += ev.Delta
			headWindow = true // 正文期：思考分区冻结为头部摘要
			statusText = ""   // 内容推进即解除挂起提示
			mu.Unlock()
		case TurnThinking:
			mu.Lock()
			thinkingBuf += ev.Delta
			headWindow = false // 思考工具期：尾部窗口跟随最新思考
			statusText = ""
			mu.Unlock()
		case TurnToolUse:
			mu.Lock()
			tools = append(tools, toolEntry{name: ev.ToolName})
			headWindow = false
			statusText = ""
			mu.Unlock()
		case TurnStatus:
			mu.Lock()
			if ev.ToolName != "" {
				settleTool(tools, ev.ToolName, ev.Status) // 工具终态：面板对应行就地结算
			} else {
				statusText = ev.Status // 挂起状态行：面板下方临时段，随内容推进让位
				if ev.Card != nil {
					pendingCard = ev.Card
				}
			}
			mu.Unlock()
		case TurnDone:
			mu.Lock()
			isError = ev.IsError
			if strings.TrimSpace(ev.Result) != "" && strings.TrimSpace(currentText) == "" {
				currentText = ev.Result // Result 仅空正文兜底：headless 只含末段正文，不覆盖整回合累计
			}
			if ev.SessionID != "" {
				sessionID = ev.SessionID
			}
			mu.Unlock()
		case TurnError:
			mu.Lock()
			turnErr = ev.Err
			mu.Unlock()
		}
	}
	// 收尾时序：先停 ticker（此后仅主 goroutine 写帧），再写 finish=true 终帧。
	close(done)
	<-tickerDone

	mu.Lock()
	panel := panelContent(thinkingBuf, true, tools, useMarkdown) // 终帧恒头部摘要
	final := currentText
	mu.Unlock()

	/*
	 * 终帧文案单点决策：Err 三态（nil 成功 / errTurnFailed 中断 / 其他失败）各一条路径，
	 * 失败文案保留诊断首行（含 resume 失效信号）与失败前已产出的部分文本；面板全程保留。
	 */
	out := TurnOutcome{SessionID: sessionID}
	switch {
	case turnErr != nil:
		out.Err = turnErr
	case isError:
		out.Err = errTurnFailed
	}
	switch {
	case errors.Is(out.Err, errTurnFailed):
		final = "❌ 处理未完成（回合被中断）：\n" + final
	case out.Err != nil && ctx.Err() != nil:
		final = "⏹ 已停止当前回复。" // /stop 或编排器停止：取消叙事，非失败
	case out.Err != nil:
		body := final
		final = "❌ 处理失败：" + firstLine(out.Err.Error()) + "\n请重试或调整后重新发送。"
		if strings.TrimSpace(body) != "" {
			final += "\n\n（失败前已产出：\n" + body + "\n）"
		}
	default:
		if strings.TrimSpace(final) == "" {
			final = "（回合结束，无文本输出）"
		}
	}
	// 成功路径超长走落盘，失败路径静默截断。
	frameSink := sink
	if out.Err != nil {
		frameSink = nil
	}
	out.FinalText = truncateForFrame(composeFrame(panel, "", final, useMarkdown), rs.ByteLimit(), frameSink)
	if err := rs.Flush(out.FinalText, true); err != nil {
		log.Error("bot 流式终帧发送失败", zap.Error(err))
	}
	return out
}

// errTurnFailed result 帧标记 is_error（回合被 max-turns 等中断，进程本身正常退出）。
var errTurnFailed = errors.New("claude 回合被中断")

// toolEntry 面板工具分区的一行：settled 非空为终态文案（✅/❌），空 = 执行中。
type toolEntry struct {
	name    string
	settled string
}

// settleTool 就地结算最早同名未完结工具行（同名多次调用按序结算）。
func settleTool(tools []toolEntry, name, settled string) {
	for i := range tools {
		if tools[i].name == name && tools[i].settled == "" {
			tools[i].settled = settled
			return
		}
	}
}

// thoughtWindowRunes 思考内容展示窗口的 rune 数。
const thoughtWindowRunes = 300

/*
 * panelContent 面板明文形态（包 quote 前）：思考分区 + 工具分区两大段、段间空行；
 * headWindow 决定思考窗口方向（尾部跟随 / 头部摘要），md=false 时标签剥离粗体，任一段可缺席。
 */
func panelContent(thinkingBuf string, headWindow bool, tools []toolEntry, md bool) string {
	thinkingLabel, toolLabel := "🤔 思考中：", "🔧 调用工具："
	if md {
		thinkingLabel, toolLabel = "**🤔 思考中：**", "**🔧 调用工具：**"
	}
	var sections []string
	if thinkingBuf != "" {
		sections = append(sections, thinkingLabel+"\n"+thoughtWindow(thinkingBuf, headWindow))
	}
	if len(tools) > 0 {
		var b strings.Builder
		b.WriteString(toolLabel)
		for _, t := range tools {
			b.WriteString("\n")
			if t.settled != "" {
				b.WriteString(t.settled)
			} else {
				b.WriteString("⏳ " + t.name + " 执行中…")
			}
		}
		sections = append(sections, b.String())
	}
	return strings.Join(sections, "\n\n")
}

// quoteBlock 把多行文本包成 markdown quote 块（空行渲染为 > 分隔行）。
func quoteBlock(text string) string {
	var b strings.Builder
	first := true
	for line := range strings.SplitSeq(strings.TrimRight(text, "\n"), "\n") {
		if !first {
			b.WriteString("\n")
		}
		first = false
		b.WriteString(">")
		if line != "" {
			b.WriteString(" ")
			b.WriteString(line)
		}
	}
	return b.String()
}

// quoteAware 状态词按渠道渲染能力包 quote（无 markdown 能力渠道原样纯文本）。
func quoteAware(rs ReplyStream, text string) string {
	if mfs, ok := rs.(MarkdownFrameStream); ok && mfs.MarkdownFrames() {
		return quoteBlock(text)
	}
	return text
}

/*
 * composeFrame 组一帧：quote 面板 + quote 挂起状态行 + 正文（在 quote 外），三者可任意缺席；
 * md=false 时不包 quote（纯文本渠道降级）。
 */
func composeFrame(panel, status, body string, md bool) string {
	var b strings.Builder
	for _, part := range []string{panel, status} {
		if part == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		if md {
			b.WriteString(quoteBlock(part))
		} else {
			b.WriteString(part)
		}
	}
	if body != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(body)
	}
	return b.String()
}

/*
 * thoughtWindow 思考内容展示窗口（rune 安全）：尾部跟随（前缀 … 标识所见非从头）或
 * 头部摘要（尾部 … 标识后续未完）；未超限原样。
 */
func thoughtWindow(s string, head bool) string {
	rs := []rune(s)
	if len(rs) <= thoughtWindowRunes {
		return s
	}
	if head {
		return string(rs[:thoughtWindowRunes]) + " …"
	}
	return "…" + string(rs[len(rs)-thoughtWindowRunes:])
}

/*
 * truncateForFrame 把内容截到 limit 字节内（rune 边界安全）。超限时调用 sink 落盘全文并把
 * 路径附进截断提示（数据不丢：全文始终可取）；sink 为 nil 或中间帧场景只做静默截断。
 */
func truncateForFrame(content string, limit int, sink OverflowSink) string {
	if limit <= 0 || len(content) <= limit {
		return content
	}
	// 按 rune 累计字节，不切半个 UTF-8 序列。
	kept := 0
	cut := len(content)
	for i, r := range content {
		size := len(string(r))
		if kept+size > limit-reservedOverflowMarkBytes {
			cut = i
			break
		}
		kept += size
	}
	truncated := strings.TrimRight(content[:cut], "\n")
	if sink != nil {
		if path := sink(content); path != "" {
			return truncated + "\n\n…（内容过长已截断，全文已保存：" + path + "）"
		}
	}
	return truncated + "\n\n…（内容过长已截断）"
}

// reservedOverflowMarkBytes 截断保留余量：给截断提示（含路径）预留的字节数。
const reservedOverflowMarkBytes = 512

// firstLine 取错误首行（错误详情可能多行，IM 消息保持紧凑）。
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
