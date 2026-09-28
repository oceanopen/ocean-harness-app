package bot

import (
	"ocean-harness/server/internal/dal/enums"
)

// 渠道适配器契约：adapter 包（internal/bot/wecom 等）实现本文件接口，核心只面向接口编排。
// 第二渠道（feishu）接入 = 新增 adapter 包实现两件套（ChannelRuntime + Factory）+ main 注册
// 一行，核心零改动。

// ChannelStatus 渠道连接状态投影（前端状态灯数据源）。
type ChannelStatus struct {
	State     string // "connecting" | "connected" | "disconnected" | "stopped"
	LastError string // 最近一次终态错误（认证失败/被新连接踢下线）；空 = 无
}

// ReplyStream 覆写式流式回复（企微 ReplyStream 同 streamId 整条覆盖 / 飞书卡片编辑流同构，
// 故抽象放核心而非渠道）。一条入站消息对应一个 ReplyStream 实例。
type ReplyStream interface {
	// Flush 整条覆盖推送。final=false 中间帧（发送失败可跳过）；final=true 终帧（必须送达，
	// 且每条流式消息恰好一次，由回复泵保证）。
	Flush(content string, final bool) error
	// ByteLimit 渠道单条内容字节上限（wecom 20480），截断策略在回复泵统一执行。
	ByteLimit() int
}

// ChannelRuntime 渠道适配器运行时：连接生命周期 + 入站回调 + 开回复流。
// 渠道身份由 Factory.Channel() 承载（注册表 key），运行时不再重复声明。
type ChannelRuntime interface {
	// Start 建立连接（SDK 长连接自带重连）；不阻塞——连接结果经 Status 投影呈现。
	Start(cfg BotRuntimeConfig, onInbound func(InboundMessage)) error
	// Stop 断开连接并释放（幂等）。supervisor 保证「一实例一次 Start」，重启用新实例。
	Stop()
	// Status 当前连接状态投影。
	Status() ChannelStatus
	// OpenReply 由路由信息开出一条覆写式回复流（Payload 为适配器自存的 opaque 值）。
	OpenReply(route RouteInfo) (ReplyStream, error)
}

// Factory 渠道工厂——supervisor 注册表的扩展缝。
type Factory interface {
	Channel() enums.Channel
	Create() (ChannelRuntime, error)
}
