package bot

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// streamTickInterval 流式推送节奏（协议固定编排，非时间性补丁）：企微流式中间帧 ack 有约 5s
// 固有延迟，200ms ticker 单写者既保证刷新密度又自动跳过积压帧（wecom-aibot-go-sdk
// examples/agent 同款节奏）。
const streamTickInterval = 200 * time.Millisecond

// OverflowSink 超长全文落盘回调：回复超过渠道 ByteLimit 时被调用一次，回调负责把全文写入
// 持久位置并返回展示在截断提示里的路径（如 "<workspace>/.bot-outbox/<ts>-<msgid>.md"）。
// 返回空串则截断提示不附路径。落盘放编排器（知道 msgid/工作目录），泵只负责拼帧。
type OverflowSink func(fullText string) string

// TurnOutcome 一次回合推送的终态。SessionID 为 init/result 捕获的 claude 会话 id
// （可能为空，如 spawn 即失败）；Err 非 nil = 回合失败（终帧已写失败文案）。
type TurnOutcome struct {
	FinalText string
	SessionID string
	Err       error
}

// PumpReply 回复泵：消费 TurnEvent 流 → 覆写式流式回复。时序固定（两写者一次交接零并发写）：
// 主 goroutine 消费事件累积状态；ticker goroutine 周期取快照 Flush 中间帧（唯一周期写者）；
// 事件流关闭后停 ticker，由主 goroutine 写 finish=true 终帧。tool_use 到达时清空已累计文本、
// 置「正在执行 …」状态行（丢弃工具前泄漏文本，SDK agent 示例同款）；result 终文本优先于增量累计。
// 回合失败同样以终帧收尾（失败文案），保证每条流式消息恰好一个 finish 帧。
func PumpReply(ctx context.Context, rs ReplyStream, events <-chan TurnEvent, sink OverflowSink, log *zap.Logger) TurnOutcome {
	var mu sync.Mutex
	var statusText, currentText, sessionID string
	var turnErr error
	var isError bool

	snapshot := func() string {
		mu.Lock()
		defer mu.Unlock()
		if currentText != "" {
			return currentText
		}
		return statusText
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
			mu.Unlock()
		case TurnToolUse:
			mu.Lock()
			currentText = ""
			statusText = "🔧 正在执行 " + ev.ToolName + " …"
			mu.Unlock()
		case TurnDone:
			mu.Lock()
			isError = ev.IsError
			if strings.TrimSpace(ev.Result) != "" {
				currentText = ev.Result
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
	final := currentText
	mu.Unlock()

	// 终帧文案单点决策：Err 三态（nil 成功 / errTurnFailed 中断 / 其他失败）各一条路径，
	// 失败文案保留诊断首行（含 resume 失效信号）与失败前已产出的部分文本。
	out := TurnOutcome{SessionID: sessionID}
	switch {
	case turnErr != nil:
		out.Err = turnErr
	case isError:
		out.Err = errTurnFailed
	}
	switch {
	case errors.Is(out.Err, errTurnFailed):
		out.FinalText = "❌ 处理未完成（回合被中断）：\n" + truncateForFrame(final, rs.ByteLimit(), nil)
	case out.Err != nil:
		out.FinalText = "❌ 处理失败：" + firstLine(out.Err.Error()) + "\n请重试或调整后重新发送。"
		if strings.TrimSpace(final) != "" {
			out.FinalText += "\n\n（失败前已产出：\n" + truncateForFrame(final, rs.ByteLimit(), nil) + "）"
		}
	default:
		if strings.TrimSpace(final) == "" {
			final = "（回合结束，无文本输出）"
		}
		out.FinalText = truncateForFrame(final, rs.ByteLimit(), sink)
	}
	if err := rs.Flush(out.FinalText, true); err != nil {
		log.Error("bot 流式终帧发送失败", zap.Error(err))
	}
	return out
}

// errTurnFailed result 帧标记 is_error（回合被 max-turns 等中断，进程本身正常退出）。
var errTurnFailed = errors.New("claude 回合被中断")

// truncateForFrame 把内容截到 limit 字节内（rune 边界安全）。超限时调用 sink 落盘全文并把
// 路径附进截断提示（数据不丢：全文始终可取）；sink 为 nil 或中间帧场景只做静默截断。
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
