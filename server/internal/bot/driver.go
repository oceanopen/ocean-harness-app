package bot

import "context"

// ClaudeDriver claude 会话引擎接口（渠道无关：所有渠道共用同一组引擎）。
// 实现：driver_claude（headless spawn——每回合一个进程，--resume 锚点续聊）、driver_acp
// （T2.1——经 acpsession 域复用 issue 级共享会话）、driver_route（逐回合一步路由——
// workspace 启动模式与会话绑定合并判定 headless/ACP，T2.3 并入绑定解析）。接口为编排器
// 测试缝（fake driver）与将来远端引擎预留。
// SessionID 为 headless 语义：空 = 新会话，非空 = --resume 续聊（ACP 驱动不消费）。
type ClaudeDriver interface {
	// RunTurn 发起一轮回合并返回事件通道：TurnText 增量 / TurnInit 会话 id /
	// TurnToolUse 工具进度 / TurnDone 终态 / TurnError 失败；回合结束后通道关闭。
	// 受理失败（引擎起不来 / 目标不可用）同步返回 error；ctx 取消 = 终止本回合。
	RunTurn(ctx context.Context, req TurnRequest) (<-chan TurnEvent, error)
}
