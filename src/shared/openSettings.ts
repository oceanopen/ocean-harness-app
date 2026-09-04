import { commands } from './bindings';

/**
 * 语义化深链：打开设置窗口并定位到「项目配置」分区——错误态引导用户去设置工作空间
 * 根目录时使用（EmbeddedTerminal / WorkspaceFilePanel 共用，避免深链片段复制增殖）。
 * source 仅用于 warn 日志定位调用方。
 */
export function openProjectConfigSettings(source: string): void {
  void commands.showSettingsWindow('projectConfig').then((res) => {
    if (res.status === 'error') {
      console.warn(`[${source}] open settings failed:`, res.error);
    }
  });
}
