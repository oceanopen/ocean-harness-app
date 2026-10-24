package bot

import (
	"context"
	"strings"
)

/*
 * TurnDriver 回合引擎接口（渠道无关：所有渠道共用同一组引擎；双驱动并存）。
 * 实现：driver_claude（headless spawn——每回合一个进程，--resume 锚续聊）、driver_acp（经
 * acpsession 域共享会话）、driver_route（逐回合一步路由——按容器 launch_settings.mode 判定引擎与会话锚）。
 * 接口为编排器测试缝（fake driver）与将来远端引擎预留。
 */
type TurnDriver interface {
	/*
	 * RunTurn 发起一轮回合并返回事件通道：TurnText 增量 / TurnInit 会话 id /
	 * TurnToolUse 工具进度 / TurnDone 终态 / TurnError 失败；回合结束后通道关闭。
	 * 受理失败（引擎起不来 / 目标不可用）同步返回 error；ctx 取消 = 终止本回合。
	 */
	RunTurn(ctx context.Context, req TurnRequest) (<-chan TurnEvent, error)
}

// toolSettledText 工具终态面板行文案（ACP toolCall 终态与 headless tool_result 两路共用）。
func toolSettledText(name string, failed bool) string {
	if failed {
		return "❌ " + name + " 失败"
	}
	return "✅ " + name + " 已完成"
}

/*
 * diffWatermark 水位线差分：full 为累计全量、sent 为已发前缀（rune 安全，len(sent) 恒落字符
 * 边界）。前缀错配（条目被覆写换内容）重置全量重发，返回待发增量与新水位线。
 */
func diffWatermark(full, sent string) (delta string, newSent string) {
	if !strings.HasPrefix(full, sent) {
		sent = ""
	}
	return full[len(sent):], full
}
