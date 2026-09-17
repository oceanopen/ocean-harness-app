import type { Dayjs } from 'dayjs';
import dayjs from 'dayjs';

type DateInput = Dayjs | Date | number | string;

/**
 * 把日期对象/时间戳格式化为指定模式（本地时区）。
 *
 * pattern 遵循 dayjs 占位符：`YYYY-MM-DD HH:mm:ss`（大写 HH=24 小时制，hh=12 小时制）。
 * 与原生 `new Date()` 的 getHours 等同为本地时区行为。
 */
export function formatDate(date: DateInput, pattern: string): string {
  return dayjs(date).format(pattern);
}

/**
 * 相对时间文案（刚刚 / X 分钟前 / X 小时前 / X 天前 / X 周前 / X 个月前 / X 年前），
 * 依赖调用方注入的 i18n t，差值计算交给 dayjs（不手写除法，无需 dayjs 插件）。
 * namespace 默认 'claudeSessions'（向后兼容），用到的 i18n key：
 * ${namespace}:time.{justNow, minutesAgo, hoursAgo, daysAgo, weeksAgo, monthsAgo, yearsAgo}。
 * 新页面（如 repositories）传入对应命名空间即可复用，无需各自实现相对时间逻辑。
 */
export function formatRelativeTime(
  updatedAt: number,
  t: (key: string, opts?: Record<string, unknown>) => string,
  namespace = 'claudeSessions',
): string {
  const now = dayjs();
  const then = dayjs(updatedAt);
  // 兜底时钟漂移/未来时间：diffSec 最小为 0，负差值统一回落到「刚刚」
  const diffSec = Math.max(0, now.diff(then, 'second'));
  if (diffSec < 60) {
    return t(`${namespace}:time.justNow`);
  }
  const minutes = now.diff(then, 'minute');
  if (minutes < 60) {
    return t(`${namespace}:time.minutesAgo`, { count: minutes });
  }
  const hours = now.diff(then, 'hour');
  if (hours < 24) {
    return t(`${namespace}:time.hoursAgo`, { count: hours });
  }
  const days = now.diff(then, 'day');
  if (days < 7) {
    return t(`${namespace}:time.daysAgo`, { count: days });
  }
  if (days < 30) {
    const weeks = now.diff(then, 'week');
    return t(`${namespace}:time.weeksAgo`, { count: weeks });
  }
  const months = now.diff(then, 'month');
  if (months < 12) {
    return t(`${namespace}:time.monthsAgo`, { count: months });
  }
  const years = now.diff(then, 'year');
  return t(`${namespace}:time.yearsAgo`, { count: years });
}
