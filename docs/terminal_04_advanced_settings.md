# 终端进阶设置

> **本模块定位**：嵌入式终端的用户可调参数。本期落地 scrollback 行数；主题选择等终端 UI 配置归 terminal_05。
>
> **前置依赖**：模块 3 `docs/terminal_03_toolbar_extras.md`（字号设置范式——本模块照抄该全链范式）。
>
> **范围（本期）**：scrollback 行数设置（默认 1000，可选 1000/2000/3000/5000）。**非目标**：主题选择、光标样式、行高（terminal_05）。

---

## 1. 背景

terminal_03 收尾时对照 orca（Electron 终端，22 项可调设置全热生效）与 hello-halo（全部写死的反例）做了进阶设置梳理，结论：

- 本项目已有「appConfig key + 设置页 draft/saved + useConfigValue 订阅 + options 运行时赋值」全链范式（字号项验证），加设置项边际成本极低
- 原后续模块「终端主题/scrollback 行数等进阶设置」与已落地的主题增强部分重叠（基建已就位）——拆分：scrollback 归本模块，主题选择归 terminal_05
- orca 参照要点：scrollback 是纯 `options.scrollback` 写入（不 refit 不通知 PTY）；默认值+归一化函数分离防脏数据直达 xterm 构造

## 2. 设计

- appConfig 新 key：`terminal_scrollback_rows`，离散选项 `[1000, 2000, 3000, 5000]`，默认 1000（= xterm 未显式配置的默认值，保守起步），DB 脏值回落默认（枚举校验范式，同字号）
- 生效路径：EmbeddedTerminal `useConfigValue` 订阅 → TerminalView props → 构造取初值 + 独立 `useEffect` 纯 `options.scrollback = n`（调大即刻多翻、调小立即截尾，无闪烁）；多 pane 各自订阅保存事件全量生效
- **注意项**：Rust 侧 reattach 回放 ring 容量与本设置独立——scrollback 调大后若 ring 容量小于回放目标仍会截断（现状 ring 容量固定，如需联动属后续优化，orca 有「backlog cap 随行数缩放」先例）

## 3. 任务清单

### ✅ 任务 1 — scrollback 行数设置
- **文件**：`appConfig.ts`（key + 选项集 + parse）+ `EmbeddedTerminal.tsx` / `TerminalView.tsx`（订阅下传 + 运行时赋值）+ `TerminalConfigPage.tsx`（下拉行，七配置共管）+ i18n 两语言
- **验证**：ESLint + web:build 通过；真机——改行数保存后即时生效（调大能多翻）、重启保持、DB 脏值回落 1000。

---

## 4. 后续模块

### terminal_05 — 终端 UI 配置（主题/光标/行高/预览）

需求定稿（2026-08-20，用户确认）：

1. **主题完全用户自选**，不跟随 app 明暗——推翻 terminal_03 收尾时的「跟随明暗双槽」设想
2. **内置 4-6 套高频主题**（从 orca 22 套挑：Dracula/One Dark/Solarized 等），**默认选一个高频暗色主题**
3. 现有 terminalTheme.ts 的 Tango 明暗双主题**退役为参考实现**（色值来源），不再作为配置项
4. **设置页真实小终端实时预览**（orca TerminalSettingsPreview 范式：36x15 真 xterm 实例，复用生产解析函数，改主题/字号即时可见）
5. 同模块候选：光标样式（block/bar/underline + 闪烁开关）、行高（1.0-2.0）——字号项从 terminal_03 平移归组（实现不动，文档归组）

**参照**：orca 主题机制要点——值门控写 options（theme 写前深比较，跳过无变化写入防丢 TUI 运行时 OSC 改色）；主题定义 TS 内联 `Record<string, ITheme>` 零运行时 IO。

**裁剪不做**：主题导入（Ghostty/Warp 解析）、22 色逐槽覆盖、透明度/Acrylic、字体族选择、连字、GPU 开关。
