// ImBot connState → 徽标文案与色调的 SSOT：ImBotsPage 机器人卡片与工作台 ImBotPanel
// 聚合卡共用（T3.2 从 ImBotsPage 局部函数提取共享）。
export function imBotStateChip(state: string): { label: string; color: 'success' | 'warning' | 'error' | 'default' } {
  switch (state) {
    case 'connected':
      return { label: '已连接', color: 'success' };
    case 'connecting':
      return { label: '连接中', color: 'warning' };
    case 'disconnected':
      return { label: '已断开', color: 'error' };
    default:
      return { label: '未运行', color: 'default' };
  }
}
