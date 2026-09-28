package bot

import "context"

// ClaudeDriver claude 会话引擎接口（渠道无关：所有渠道共用同一引擎）。
// 唯一实现 driver_claude（headless spawn）；接口为编排器测试缝（fake driver）与
// 将来远端引擎预留。每回合一个进程：SessionID 空 = 新会话，非空 = --resume 续聊。
type ClaudeDriver interface {
	// RunTurn spawn 一个 headless 回合并返回事件通道：TurnText 增量 / TurnInit 会话 id /
	// TurnToolUse 工具进度 / TurnDone 终态 / TurnError 失败；进程退出后通道关闭。
	// spawn 失败（如 claude 不在场）同步返回 error；ctx 取消 = kill 子进程。
	RunTurn(ctx context.Context, req TurnRequest) (<-chan TurnEvent, error)
}
