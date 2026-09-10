import type { SettingsSection } from './SettingsPage/routes';
import { useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { sectionToPath } from './SettingsPage/routes';

/**
 * 「打开设置页某分区」在 panel 内的唯一深链入口：返回稳定回调，供错误态/引导按钮消费
 * （EmbeddedTerminal / WorkspaceFilePanel / WorkspaceInitGate 共用，杜绝深链入口增殖）。
 * 设置页已并入 panel，纯前端导航；分区记忆由 PanelApp 的 PATH_MEMORY_MENUS 自动记录，
 * 后续齿轮/托盘入口再进设置页时回到该分区。
 *
 * 放置在 panel 壳层（而非 SettingsPage/ 内）：消费方含 DevWorkbenchPage 组件，而
 * SettingsPage 的 TerminalConfigPage/TerminalPreview 又依赖 DevWorkbench 的 terminalTheme
 * ——hook 留在 feature 目录会形成目录级环；壳层依赖 SettingsPage/routes（叶子 SSOT）
 * 与 PanelApp 消费同级，方向恒向下。依赖（settingsNavigate → SettingsPage/routes）与
 * 既有 PanelApp → SettingsPage/routes 同向，无环。
 */
export function useSettingsNavigate(section: SettingsSection): () => void {
  const navigate = useNavigate();
  return useCallback(() => {
    navigate(sectionToPath(section));
  }, [navigate, section]);
}
