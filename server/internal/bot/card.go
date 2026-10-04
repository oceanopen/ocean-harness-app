package bot

// 本文件是渠道卡片交互的核心契约（T3.1 企微卡片通道接线）：入站交互消费者骨架
// （interactionHandler）+ 出站窄中立卡片模型（CardSpec）与 ReplyStream 的可选卡片扩展面
// （CardReplyStream）。渠道差异（企微 vote_interaction 映射、文案截断、按钮 key 编码、
// event_key 反解）全在适配器翻译层（wecom/card.go），核心不感知渠道。

// CardOption 中立选项：应用侧语义 id（如审批 optionID）+ 展示文案。ID 语义由投递方自持；
// 适配器只按序号编码按钮 key，不理解 ID 含义。
type CardOption struct {
	ID   string
	Text string // 选项文案（企微截 11 字）
}

// CardSpec 核心窄中立卡片模型：一张「标题 + 描述 + 选项 + 提交」的一次性交互卡（T3.2 审批
// 单选卡 / T3.3 表单卡可见视界）。刻意窄——按场景消费后不足再扩字段，不为未证实协议预建。
type CardSpec struct {
	Title       string       // 标题（企微 main_title.title，截 26 字）
	Description string       // 描述（企微 main_title.desc，截 30 字）
	Options     []CardOption // 选项列表（企微 checkbox option_list，1..20）
	Multiple    bool         // true 多选（企微 checkbox mode 1 + 提交按钮）
	TaskID      string       // 投递锚 = 企微 task_id（≤128 字节，仅 [0-9A-Za-z_\-@]）；确定性生成，点击回传即 Interaction.DeliveryID
	Disabled    bool         // 仅 UpdateCard 消费：置灰整卡（企微 checkbox.disable）；创建帧忽略该字段
}

// CardReplyStream ReplyStream 的可选卡片扩展面：实现它的回复流可随流附卡、以卡片更新应答
// 一条点击事件。纯文本渠道不实现即完全不受影响——消费方类型断言探测，失败回落纯文本
// （双渲染单事件：卡是升级渲染，文本是无卡渠道的回落）。
type CardReplyStream interface {
	ReplyStream
	// SendCard 附卡帧：content/final 语义对齐 Flush（覆写、final 终帧），卡随帧附带。
	// 渠道协议「卡同消息仅一次」：已附卡后的调用返回错误，调用方回落 Flush(content, final)。
	SendCard(spec CardSpec, content string, final bool) error
	// UpdateCard 以卡片更新应答本条入站（仅对「点击事件路由」开出的流有意义，如置灰原卡）。
	// 企微约束与调用方责任：须用点击事件帧 headers（本流的 RouteInfo 即点击帧）；须在点击
	// 事件 5 秒窗口内——拦截步在渠道入站串行消费 goroutine 执行，前置消息的附件下载排队会
	// 吃掉窗口（T3.2 置灰落地时评估交互事件分流）；spec 须为被点卡的完整 spec（企微
	// UpdateTemplateCard 整卡替换，置灰即「原卡全量字段 + Disabled=true」，仅给 TaskID+
	// Disabled 会被适配器拒绝）且 TaskID 与被点卡一致（card_type/submit_button.key 一致同此）。
	UpdateCard(spec CardSpec) error
}

// TODO(T3.4): 桌面发起回合的挂起主动推卡（无入站消息锚点）时，在此扩 ChannelRuntime 的
// 可选扩展接口 CardPusher{ PushCard(conversationKey string, spec CardSpec) error }（企微落
// SendMessage(chatid, SendTemplateCardMsgBody)，chatid 由适配器从 ConversationKey 反解）。
// CardSpec 无消息锚依赖、翻译纯函数无路由依赖，届时零重构接入。

// interactionHandler 卡片交互消费者的可选依赖（通用拦截骨架，仿 pendingGate；nil = 仅兜底）。
// T3.2 由 driverRoute 实现（pendingGate 双角色装配同款，supervisor 换第三角色注入）。
// handler 是纯决策：不做流 I/O——流的写权始终在编排器（两写者一次交接纪律）。
type interactionHandler interface {
	// TryHandleInteraction 判定一条卡片点击是否可消费。handled=true 时 text 即终帧文案；
	// update 非 nil 时调用方先经 CardReplyStream.UpdateCard 应用（流无卡能力则静默跳过，
	// 终帧照发）。未命中（过期投递/未知锚）由调用方兜底——点击不承载回合正文，永不回落
	// 主路径。
	TryHandleInteraction(cfg BotRuntimeConfig, msg InboundMessage) (text string, update *CardSpec, handled bool)
}

// interactionFallbackText 兜底文案：通道已通、消费者未接（T3.2 前）或投递过期时的统一提示。
func interactionFallbackText() string {
	return "该卡片交互暂不能在此处理，请在桌面端操作，或直接发送文字消息。"
}
