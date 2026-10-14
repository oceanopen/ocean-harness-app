// 贴底判定：距底不足 48px 即视为贴底。
export interface StickMeasurements {
  scrollHeight: number;
  scrollTop: number;
  clientHeight: number;
}

const STICK_THRESHOLD_PX = 48;

export function isStickToBottom({ scrollHeight, scrollTop, clientHeight }: StickMeasurements): boolean {
  return scrollHeight - scrollTop - clientHeight < STICK_THRESHOLD_PX;
}
