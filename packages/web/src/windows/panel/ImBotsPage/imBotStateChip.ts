/**
 * ImBot connState → 徽标文案、色调与 tooltip 的 SSOT：ImBotsPage 机器人卡片与工作台
 * ImBotPanel 聚合卡共用（tooltip 为状态语义说明，具体故障原因由调用方以 lastError 优先）。
 */
export function imBotStateChip(state: string): { label: string; color: 'success' | 'warning' | 'error' | 'default'; tooltip: string } {
  switch (state) {
    case 'connected':
      return { label: '已连接', color: 'success', tooltip: '与 IM 服务的长连接正常，可收发消息' };
    case 'connecting':
      return { label: '连接中', color: 'warning', tooltip: '正在与 IM 服务建立长连接' };
    case 'disconnected':
      return { label: '已断开', color: 'error', tooltip: '长连接已断开，可尝试重启连接' };
    default:
      return { label: '未运行', color: 'default', tooltip: '机器人未启用或尚未建立连接' };
  }
}
