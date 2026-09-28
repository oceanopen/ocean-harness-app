// imBots 域对外唯一入口（一域一目录约定，见 src/state/README.md）。
// 无 store.ts：本域无 client 选中态（抽屉选中用页面局部 useState），遵守「server 状态用 Query」。
export { imBotsKeys } from './keys';
export { useCreateImBot, useDeleteImBot, useImBots, useRestartImBot, useUpdateImBot } from './queries';
