import type { ChipProps } from '@mui/material';
import type { BrowserCallOutcome, BrowserSessionStatus } from '@src/services';

// 浏览器面板 chip 文案/配色 SSOT（镜像 imBotStateChip 范式）：状态行与操作流水行共用，
// 面板内不散落 switch。

type ChipColor = NonNullable<ChipProps['color']>;

export interface BrowserStateChip {
  label: string;
  color: ChipColor;
}

// 会话状态机（D4）chip 映射。
export function browserSessionStateChip(status: BrowserSessionStatus | undefined): BrowserStateChip {
  switch (status) {
    case 'ready':
      return { label: '运行中', color: 'success' };
    case 'starting':
      return { label: '准备中', color: 'warning' };
    case 'failed':
      return { label: '失败', color: 'error' };
    case 'idle':
      return { label: '已释放', color: 'default' };
    default:
      return { label: '未知', color: 'default' };
  }
}

// 「最近操作」outcome chip 映射（轻量展示分类，不承载重试语义）。
export function browserCallOutcomeChip(outcome: BrowserCallOutcome): BrowserStateChip {
  switch (outcome) {
    case 'ok':
      return { label: '成功', color: 'success' };
    case 'error':
      return { label: '失败', color: 'error' };
    case 'timeout':
      return { label: '超时', color: 'warning' };
    case 'stale_ref':
      return { label: '引用失效', color: 'warning' };
    case 'closed':
      return { label: '会话关闭', color: 'default' };
    default:
      return { label: outcome, color: 'default' };
  }
}
