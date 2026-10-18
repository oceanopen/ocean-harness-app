/**
 * IM bot 渠道元数据 SSOT（前端）：列表页左栏渠道卡片与编辑抽屉渠道表单项共用。
 * key 对齐后端 Channel 枚举（server/internal/dal/enums/channel.go）。
 */

/** 渠道元数据（key 对齐后端 Channel 枚举值；文案与接入入口差异全部由此驱动）。 */
export interface ImBotChannelMeta {
  key: string;
  label: string;
  /** 右栏顶部引导文案。 */
  intro: string;
  /** 空列表引导文案。 */
  emptyHint: string;
  /** 扫码入口按钮文案。 */
  provisionLabel: string;
  /** 是否支持手工凭据接入（false = 仅扫码授权：weixin 凭据唯一来源是扫码）。 */
  manualProvision: boolean;
}

export const IM_BOT_CHANNELS: readonly ImBotChannelMeta[] = [
  {
    key: 'wecom',
    label: '企业微信',
    intro: '接入企业微信智能机器人：消息驱动本机 Claude Code 会话，流式回复，支持引用与附件。',
    emptyHint: '还没有机器人：推荐「扫码接入」（企业微信 App 授权自动创建），或「手动接入」填写后台凭据。',
    provisionLabel: '扫码接入',
    manualProvision: true,
  },
  {
    key: 'weixin',
    label: '微信',
    intro: '接入微信机器人：微信 App 扫码授权自动创建，消息驱动本机 Claude Code 会话，流式回复，支持图片与文件收发。',
    emptyHint: '还没有机器人：点右上角「扫码授权接入」，用微信 App 扫码即可自动创建。',
    provisionLabel: '扫码授权接入',
    manualProvision: false,
  },
];
