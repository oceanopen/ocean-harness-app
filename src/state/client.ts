import { QueryClient } from '@tanstack/react-query';

// QueryClient 全局默认配置（所有域、所有窗口共享的 SSOT）。
// 各窗口为独立 JS realm，模块级单例在每个窗口里自然是独立实例——缓存不共享，
// 符合「跨窗口同步走后端 Tauri 事件，不走前端 store/Query」的约定。
//
// - staleTime 10min：tracker 是纯 HTTP 请求-响应，无后端事件推送；
//   配合 tracker 的 display:none 保活（组件不卸载 → Query 实例常驻 →
//   即使 stale 也不会自动重取），仅在显式 invalidate 时刷新。
// - refetchOnWindowFocus false：Tauri 多窗口切走再切回会触发 focus，
//   与 display:none 保活叠加会导致无谓重取，必须关闭。
// - retry 1：网络抖动重试一次，避免 go-server 启动瞬间失败立即放弃。
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 10 * 60 * 1000,
      refetchOnWindowFocus: false,
      retry: 1,
    },
  },
});
