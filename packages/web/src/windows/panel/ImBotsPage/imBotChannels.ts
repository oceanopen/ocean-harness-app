// IM bot 渠道元数据 SSOT（前端）：列表页左栏渠道卡片与编辑抽屉渠道表单项共用。
// 本期仅企微；飞书等接入时在此追加一项（后端枚举见 server/internal/dal/enums/channel.go）。

/** 渠道元数据（key 对齐后端 Channel 枚举值）。 */
export interface ImBotChannelMeta {
  key: string;
  label: string;
}

export const IM_BOT_CHANNELS: readonly ImBotChannelMeta[] = [
  { key: 'wecom', label: '企业微信' },
];
