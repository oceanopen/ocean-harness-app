# src/state — 前端状态管理（zustand + TanStack Query）

按「**通用基础设施层 + 自包含域模块**」组织：

- `client.ts` — QueryClient 单例 + 全局默认配置（所有域/窗口共享）。
- `<domain>/` — 每个业务域一个自包含子目录（store + keys + queries + index）。

## 设计原则

1. **一域一目录**：加新域 = 新建子目录，零改动其他域；删域 = 删目录。
2. **域对外只暴露 `index.ts`**：消费方只从 `@src/state/<domain>` import hooks，
   域内部重构（store 改结构、keys 改格式）不波及消费方。
3. **一域一 store**：域内 client 状态联动（如选 workspace 清 project）在域 store 内部处理，
   不搞全局单 store 的 slice 耦合。
4. **server 状态用 Query，client 状态用 zustand**：列表/详情/缓存/失效走 Query；
   纯前端选中态/UI 开关走 zustand。

## 新增一个域（模板）

以 `tracker/` 为参照，新建 `src/state/<domain>/`：

```
src/state/<domain>/
├── keys.ts     # query key 工厂（对象式，含 all 根用于整域失效）
├── store.ts    # 该域 client 状态（如选中态）；无 client 状态可省略
├── queries.ts  # useXxxList / useCreateXxx / useUpdateXxx / useDeleteXxx + mutation 内部 invalidate
└── index.ts    # 汇总导出 hooks，对外唯一入口
```

## 命名约定

- query hooks：`useXxxList` / `useXxx`（单查）/ `useCreateXxx` / `useUpdateXxx` / `useDeleteXxx`
- query key：`['<domain>', '<entity>', { ...params }]`，末位用对象便于扩展筛选参数
- 每域 key 工厂提供 `<domain>Keys.all`（整域失效根），便于将来接 Tauri 事件时一次性 invalidate
- mutation 内部封装 invalidate + store 联动副作用；消费方只调 `mutate(payload)`，不关心失效逻辑

## QueryClient 挂载

- 本轮仅 panel 窗口挂 `<QueryClientProvider client={queryClient}>`（见 `windows/panel/main.tsx`）。
- 其它窗口将来要用时，各自 import 本目录 `client.ts` 的 `queryClient` 挂 Provider；
  Tauri 多窗口各自独立 JS realm，QueryClient 实例天然隔离、缓存不共享。

## 跨窗口同步

前端 store/Query **不负责**跨窗口同步。跨窗口状态仍走后端 SSOT + Tauri 事件
（参考 `src/shared/useConfigValue.ts` 范式）。将来若后端给某域加 `*:changed` 推送事件，
在该域消费窗口顶层订阅一次 → `queryClient.invalidateQueries({ queryKey: <domain>Keys.all })`。
