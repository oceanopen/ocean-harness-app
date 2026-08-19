# 终端 split 分割窗口（完整树）

> **本模块定位**：嵌入式终端的交互升级——工作台右侧终端区支持水平/垂直分割、多 pane 独立 PTY、拖拽调比例、布局持久化。对齐 orca 的 PaneManager 体验（tab 级 tab 不需要——一 issue 一终端页）。
>
> **参考实现**（orca）：`src/renderer/src/lib/pane-manager/`（pane-manager.ts / pane-tree-ops.ts / pane-divider.ts / pane-dom-creation.ts）、`src/shared/terminal-tab-types.ts`（TerminalLayoutSnapshot）、`layout-serialization.ts`。
>
> **前置依赖**：模块 1 `docs/terminal_01_auto_claude.md` 的 `pty_shutdown_issue`（issue 删除清理多 pane）与 SpawnOpts 锚点扩展。
>
> **范围（本期）**：水平/垂直递归分割、divider 拖拽、关闭 pane、布局按 issue 持久化（localStorage）。**非目标**：pane 拖拽重排/拖出成 tab、Expand/Collapse、per-pane 标题、布局序列化进后端（PTY 常驻不受布局丢失影响，前端存即可）。

---

## 1. 模块概览

**要解决什么**：一 issue 一终端在「跑 claude + 手动看日志/跑命令」场景不够用。用户需要在一个 issue 终端页里分割出第二个（第 N 个）终端，各自独立会话。

**接入点**：`DevWorkbenchPage.tsx` 内容区（现 `EmbeddedTerminal` 单组件占满）→ 换为 `TerminalPaneRoot`（split 树容器）。

---

## 2. 现状基线

### 2.1 已有
| 能力 | 位置 |
|---|---|
| EmbeddedTerminal 自包含（props 仅 issueId） | `EmbeddedTerminal/` |
| 会话锚点 = issueId（一 issue 一会话） | `pty/state.rs` `PtySessionStore` |
| store 对 key 透明（HashMap<String, _>） | `pty/state.rs` |
| 右抽屉拖拽调宽（pointer capture 模式） | `src/shared/ResizableDrawer.tsx` |
| zustand 域目录范式 | `src/state/README.md` |

### 2.2 缺口
| 缺口 | 端 |
|---|---|
| pane 锚点语义（`issueId::paneUuid`）约定 | Rust+Web |
| split 递归树数据结构 + zustand store | Web |
| divider 拖拽调比例组件 | Web |
| 布局持久化（按 issue） | Web |
| usePtySession 泛化 sessionId | Web |

---

## 3. 设计

### 3.1 pane 锚点语义（向后兼容）

```
pane 0（主 pane）: sessionId = issueId            ← 现有会话原样兼容（已存在的会话不迁移）
附加 pane:         sessionId = `${issueId}::${paneUuid}`
```

- Rust store 完全透明（key 就是字符串，无需感知 `::`）；唯一需要感知处是 `pty_shutdown_issue` 的前缀扫描（模块 1 任务 3 已实现）。
- `::` 分隔符与 orca 的 `@@`（sessionId）/`::`（worktreeId）不同源——本项目 issueId 是 uuid 不含 `::`，无歧义。
- paneUuid 用 `crypto.randomUUID()` 前 8 位（orca 同长度；展示可辨识、localStorage key 短）。

### 3.2 split 树数据结构

```ts
// src/windows/panel/DevWorkbenchPage/components/TerminalPanes/types.ts
export type SplitDirection = 'horizontal' | 'vertical';  // horizontal = 左右分（flex row）

export type PaneLayoutNode =
  | { type: 'leaf'; paneId: string }                     // paneId: 'main' | 8位uuid
  | { type: 'split'; direction: SplitDirection; ratio: number; children: [PaneLayoutNode, PaneLayoutNode] }
```

- 二叉树（orca 同构）：递归 split 只在 leaf 上操作（split(leaf, dir) → split(ratio:0.5) 两 leaf）。**不做** orca 的 pane 重排/移动，操作集收敛为：`splitPane / closePane`，天然无复杂树变换。
- `ratio ∈ (0.1, 0.9)`，divider 拖拽 clamp。
- 活跃 pane：`activePaneId`（focus 跟随，xterm focus 事件更新）——分割/关闭作用于活跃 pane。

### 3.3 zustand store

```
src/state/terminalPanes/
  store.ts    // { layouts: Record<issueId, PaneLayoutNode>, activePanes: Record<issueId, paneId> }
  actions.ts  // splitPane(issueId, paneId, dir) / closePane(issueId, paneId)
```

纯前端状态（PTY 会话在后端，布局丢失不伤会话——重新挂载 pane 即 reattach）。持久化：store 订阅写 `localStorage`（key `terminal_pane_layout_<issueId>`），初始化读回 + JSON 校验（损坏回落单 pane）。

### 3.4 组件结构

```
TerminalPanes/
  TerminalPaneRoot.tsx    // 容器：读 store 渲染递归树；工具条（分割按钮组在模块 3 扩展）
  PaneLayout.tsx          // 递归渲染：split → flex 容器 + PaneDivider + 两 children；leaf → EmbeddedTerminal
  PaneDivider.tsx         // 拖拽调比例（pointer capture，参照 ResizableDrawer）
```

- `EmbeddedTerminal` 泛化 props：`{ issueId, paneId }` → 内部 `sessionId = paneId === 'main' ? issueId : `${issueId}::${paneId}``，其余逻辑零改动（usePtySession 已按参数化 sessionId 泛化——见任务 1）。
- flex 布局：split 容器 `display:flex; flexDirection: row|column`；children `flex: 1` 与 `flex: ratio/(1-ratio)`；divider 4px 命中区 + hover 高亮。
- leaf 尺寸变化由各 TerminalView 自带 ResizeObserver → fit → pty_resize（现有链路零改动，天然支持 pane 内 resize）。

### 3.5 关闭 pane 语义

- 关闭 pane = `closePane`（store 树剪枝：split 单子折叠）+ `ptyShutdown(sessionId)` 断后端会话。
- 关最后一个 pane（树上仅剩 main）：不允许（保证终端区至少一个 pane）；「关闭终端」按钮语义 = 回到单 main pane。
- issue 删除：`useDeleteProjectIssue` 已换 `ptyShutdownIssue`（模块 1），多 pane 全清理——本模块无需再接线。

### 3.6 自动 claude 与多 pane（与模块 1 的交互）

- main pane：spawn 时按配置带 `startup_command`（进 claude）。
- **附加 pane：不带**（用户分屏通常是要裸 shell 跑命令）。`EmbeddedTerminal` 按 `paneId === 'main'` 决定 autoRunClaude 是否生效。

---

## 4. 任务清单

> 按序执行；每个任务独立实现 + 验证。
>
> 状态图例：✅ 已完成 · 🔄 进行中 · ⬜ 待办

### ✅ 任务 1 — usePtySession 泛化 sessionId
- **文件**：`EmbeddedTerminal/usePtySession.ts`（参数 `issueId` → `sessionId`）+ `EmbeddedTerminal.tsx`（传 `sessionId = issueId`）
- **目标**：hook 与 issueId 解耦，锚点由调用方派生。现有行为零变化（main pane 仍传裸 issueId）。
- **验证**：tsc/eslint；真机现有终端全场景不回归（开/切/回切/F5/删除）。

### ✅ 任务 2 — split 树 store + TerminalPaneRoot/PaneLayout 骨架
- **文件**：`src/state/terminalPanes/{store,actions}.ts`（新增）+ `TerminalPanes/{TerminalPaneRoot,PaneLayout}.tsx`（新增）+ `DevWorkbenchPage.tsx`（挂载点替换）+ `EmbeddedTerminal.tsx`（props 增 paneId，sessionId 派生）
- **目标**：§3.2/§3.3/§3.4。初始布局 = 单 main leaf；splitPane/closePane 树操作 + 二子折叠。
- **验证**：tsc/eslint/web:build；真机——工具条分割按钮出双 pane，各自独立 shell 会话（`pty_list_sessions` 见 `issueId` + `issueId::xxx` 两 key）；关闭附加 pane 回单 pane。

### ✅ 任务 3 — PaneDivider 拖拽 + 布局持久化
- **文件**：`TerminalPanes/PaneDivider.tsx`（新增）+ store 持久化订阅 + types.ts
- **目标**：§3.3 持久化 + §3.4 divider（pointer capture、clamp 0.1-0.9、水平/垂直两向）。
- **验证**：真机——拖拽流畅无布局抖动、pane 内 xterm 随动 fit；F5 后布局还原且各 pane reattach 出 scrollback；localStorage 清空回落单 pane 不崩。

### ✅ 任务 4 — 边界与收尾（自动 claude 交互 + 活跃 pane）
- **文件**：`EmbeddedTerminal.tsx`（附加 pane 不带 startup_command）+ `TerminalPaneRoot.tsx`（activePaneId focus 跟随 + 分割/关闭作用对象）+ 文档落地记录
- **目标**：§3.5/§3.6。
- **验证**：真机——main pane 自动进 claude、附加 pane 裸 shell；活跃 pane 高亮/焦点正确；关 main pane 被拦截；issue 删除后 `pty_list_sessions` 无该 issue 任何 key。
- **已落地（用户交互反馈批次）**：
  - 自动 CLI 仅 main pane 注入（`startupCodeCli: isMain ? cli : ''`，附加 pane 恒裸 shell）。
  - 关闭语义分流：附加 pane 点击 X 直处理（ptyShutdown + 树剪枝卸载，无确认）；main pane 先二次确认 Dialog，确认后杀会话 + 占位视图（无蒙层）「当前任务工作目录为：{cwd}」+「重新打开终端」（reopen → 全新 spawn，startup_command 重新注入）。
  - main pane 工具栏最左侧 'main' 标识（TerminalView `toolbarLabel` prop）。
  - 修复 closeNode 剪枝失效（初版 leaf 分支原样返回、引用不等误判，附加 pane 关闭后杀会话但不卸载）：重写为 pruneLeaf null 标记递归剪枝 + 单子折叠 + 未命中引用原样返回，7 场景断言验证。
- **已落地（focus 跟随）**：TerminalView `onActive` prop（订阅 `.xterm-helper-textarea` focus 事件——xterm 6 公开 API 无 focus 事件、旧 onFocusChange 已移除、内部 `_onFocus` 属私有，DOM 结构是官方稳定路径）→ EmbeddedTerminal `setActivePane(issueId, paneId)` → TerminalSplitButtons 读 `activePanes`（hasPane 校验回落树上最后一个 leaf）。blur 不报：焦点只会转移，新 pane focus 自然接管。

---

## 5. 工程约束

### 5.1 会话与布局解耦
PTY 会话生命周期完全在后端（锚点 sessionId）；前端布局纯 UI 状态。布局丢失/损坏的最坏情况 = 回落单 main pane + reattach，不丢会话。**禁止**把布局写进后端或让后端感知 pane 概念（除 `pty_shutdown_issue` 前缀扫描）。

### 5.2 §3.9 前端范式延续
TerminalPanes 组件同样遵守函数式范式：divider 拖拽用 ref 记录起始位置（外部事件型数据 → ref）、树操作走 zustand 显式 action、不做响应式转发层。

### 5.3 orca → 本项目裁剪对照
| orca | 本项目 | 裁剪理由 |
|---|---|---|
| pane 重排/拖出成 tab | 不做 | 无 tab 概念，需求未出现 |
| Expand/Collapse、Equalize | 不做（可后补） | 快速跟随价值低 |
| leafId 稳定 UUID 全量 | main + 8 位 uuid | 单页规模小 |
| 布局随 workspace session 后端持久化 | localStorage 按 issue | PTY 常驻已抗刷新，后端存布局过度设计 |
| detachPaneForExternalMove（保留 PTY 移动） | 不做 | 无移动目标 |

---

## 6. 后续模块（不在本文档范围）
- pane 拖拽重排（需求出现时参照 orca pane-drag-reorder）。
- 布局快照进后端（若未来做多窗口/工作区恢复）。
