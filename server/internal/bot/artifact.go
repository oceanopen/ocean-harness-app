package bot

/*
 * ArtifactReplyStream 渠道产物回传的可选扩展契约（与 CardReplyStream 同构的能力探测接口）：
 * 支持产物投递的渠道由 reply stream 额外实现本接口，编排器经类型断言探测——未实现即跳过
 * 交付目录机制（wecom 与桌面会话零变化）。TODO(多题表单文本化): 评估后可复用本文件扩展。
 */
type ArtifactReplyStream interface {
	/*
	 * SendArtifact 投递一个产物文件（name 展示名；deliveryKey 为回合内唯一投递键——渠道侧
	 * 以其派生幂等投递 id，重试不重复投递）。失败返回错误，由调用方决定是否交代，不中断其余产物。
	 */
	SendArtifact(deliveryKey string, name string, path string) error
	// SendNotice 产物链路的附带说明（投递失败交代等；纯文本，复用渠道文本通道）。
	SendNotice(text string) error
}

/*
 * NoticeReplyStream 附带说明的独立文本投递面（会话边界提醒等非回合气泡；ArtifactReplyStream
 * 的子面，实现方天然满足）。
 */
type NoticeReplyStream interface {
	SendNotice(text string) error
}
