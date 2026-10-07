import { formatDate } from '@src/shared/time';
import dayjs from 'dayjs';

// ImBotPanel 中文直出文案助手（工具面板文案约定，对齐 ImBotsPage；shared/time 的
// formatRelativeTime 绑 i18n t 注入，不适用本场景）。纯函数，无组件状态。

/** 会话 chatType → 展示文案（single 私聊 / group 群聊）。 */
export function chatTypeLabel(chatType: 'single' | 'group'): string {
  return chatType === 'single' ? '私聊' : '群聊';
}

/** 会话 key 尾短码（会话无昵称存储，短码辅助人肉辨识同类型会话）。 */
export function conversationShortCode(conversationKey: string): string {
  const id = conversationKey.split(':')[1] ?? conversationKey;
  return id.length > 6 ? `…${id.slice(-6)}` : id;
}

/** 会话最近活跃相对文案：刚刚 / N 分钟前 / N 小时前 / 昨天 / N 天前，超一周落绝对日期。 */
export function conversationActiveText(lastMessageAt: string | null): string {
  if (!lastMessageAt) {
    return '从未活跃';
  }
  const now = dayjs();
  const then = dayjs(lastMessageAt);
  if (!then.isValid()) {
    return '从未活跃';
  }
  // 兜底时钟漂移/未来时间：负差值统一回落「刚刚」（对齐 shared/time formatRelativeTime 语义）。
  const diffSec = Math.max(0, now.diff(then, 'second'));
  if (diffSec < 60) {
    return '刚刚活跃';
  }
  const minutes = now.diff(then, 'minute');
  if (minutes < 60) {
    return `${minutes} 分钟前活跃`;
  }
  const hours = now.diff(then, 'hour');
  if (hours < 24) {
    return `${hours} 小时前活跃`;
  }
  const days = now.diff(then, 'day');
  if (days === 1) {
    return '昨天';
  }
  if (days < 7) {
    return `${days} 天前活跃`;
  }
  return formatDate(then, 'YYYY-MM-DD');
}
