import type { AcpToolCallStatus } from '@src/services';

// 工具调用四态 → 徽章呈现的映射 SSOT（claudeSessionStatus.ts 同范式：单表映射 +
// exhaustive key 检查防漏）。中文标签直出不加 i18n key（T0.2 起文案约定）。
export const TOOL_CALL_STATUS_META: Record<AcpToolCallStatus, { label: string; color: 'default' | 'primary' | 'success' | 'error' }> = {
  pending: { label: '等待中', color: 'default' },
  in_progress: { label: '执行中', color: 'primary' },
  completed: { label: '已完成', color: 'success' },
  failed: { label: '失败', color: 'error' },
};
