import type { YesNo } from './bindings';
import { commands, HTTP_SERVER_PORT_RELEASE, HTTP_SERVER_PORT_TEST } from './bindings';
import { unwrap } from './commands';

// —— Rust 单源常量 re-export（SSOT：src-tauri/src/shared/app_config.rs 等，经
// .constant() 生成于 bindings.ts）。值类型为字面量类型（as const），消费方类型安全。 ——
export {
  DEFAULT_ITERM2_SPLIT_DIRECTION,
  DEFAULT_POLL_INTERVAL_SECS,
  DEFAULT_TERMINAL_POST_OPEN_COMMAND,
  HTTP_SERVER_PORT_KEY,
  HTTP_SERVER_PORT_RELEASE,
  HTTP_SERVER_PORT_TEST,
  ITERM2_SPLIT_DIRECTION_KEY,
  LANGUAGE_KEY,
  MAX_HTTP_SERVER_PORT,
  MAX_POLL_INTERVAL_SECS,
  MIN_HTTP_SERVER_PORT,
  MIN_POLL_INTERVAL_SECS,
  PET_CLAUDE_SESSIONS_SUMMARY_DRAGGABLE_KEY,
  POLL_INTERVAL_SECS_KEY,
  TERMINAL_POST_OPEN_COMMAND_KEY,
} from './bindings';

// 本文件是前端配置项消费的统一出口。两类来源：
// 1. 后端读取的 key（LANGUAGE_KEY / POLL_INTERVAL_SECS 族等）：SSOT 在
//    src-tauri/src/shared/app_config.rs，经 tauri-specta .constant() 导出到
//    bindings.ts，此处仅 re-export（Rust 单源，改值只动 Rust + gen:bindings）。
// 2. 纯前端 key（appearance / terminal_font_size 等）：本文件定义即 SSOT，
//    后端不读取，无镜像。

// Y/N 布尔风格配置值。类型 YesNo 由后端 types.rs 的 enum 经 gen:bindings 自动生成，
// 值常量仍本地定义（satisfies 关联后端类型）：后端 #[serde(rename)] 改动后，
// 此处值若不一致会编译报错——与 .constant() 导出等价的编译期保障，维持不动。
export const YES_NO = {
  YES: 'Y',
  NO: 'N',
} as const satisfies Record<string, YesNo>;

export function isYes(value: string | null): boolean {
  return value === YES_NO.YES;
}

export function toYesNo(value: boolean): YesNo {
  return value ? YES_NO.YES : YES_NO.NO;
}

export function parseYesNo(value: string | null, fallback: YesNo): YesNo {
  return value === YES_NO.YES || value === YES_NO.NO ? value : fallback;
}

export type Appearance = 'system' | 'light' | 'dark';

export const APPEARANCE_KEY = 'appearance';
export const DEFAULT_APPEARANCE: Appearance = 'system';

export type Language = 'system' | 'zh-CN' | 'en';

export type ResolvedLanguage = Exclude<Language, 'system'>;

export const DEFAULT_LANGUAGE: Language = 'system';

// 桌宠拖拽开关。值用 YesNo，缺失视为 NO（默认关闭：点击桌宠打开监控页）。
// key 在 Rust 单源（见顶部 re-export）；默认值类型是 YesNo，走本地 satisfies 关联。
export const DEFAULT_PET_DRAGGABLE = YES_NO.NO;

// iTerm2 分屏方向。horizontal = 上下分屏，vertical = 左右分屏，none = 不分屏。
// 类型与默认值均为字面量联合，默认值从 Rust 单源 re-export（as const 兼容）。
export type Iterm2SplitDirection = 'horizontal' | 'vertical' | 'none';

// 嵌入式终端启动时自动运行的编程 CLI 工具（shell-ready 注入，docs/terminal_01_auto_claude.md）。
// 值域：'' = 不自动运行（默认）；'claude' = 当前唯一支持项，未来扩 'codex' 等。
// 后端 Rust 侧 SpawnOpts.startup_command 为通用 Option<String>，前端把枚举值映射为命令名，
// 后端不读取本 key，故无需在 config.rs 加常量副本（参照 workspace_base_dir 先例）。
export type TerminalStartupCodeCli = '' | 'claude';

export const TERMINAL_STARTUP_CODE_CLI_KEY = 'terminal_startup_code_cli';
export const DEFAULT_TERMINAL_STARTUP_CODE_CLI: TerminalStartupCodeCli = '';

export function parseTerminalStartupCodeCli(value: string | null): TerminalStartupCodeCli {
  return value === 'claude' ? value : '';
}

// 嵌入式终端字号（terminal_03 §3.3）。离散选项语义：合法值 = 选项集内整数，
// DB 脏值（非数字/越界/不在选项集如 15）一律回落默认 13（与 startup_code_cli
// 枚举校验同范式，不用 poll_interval_secs 的连续 clamp）。
// 纯前端偏好，后端不读取，故无需在 config.rs 加常量副本（参照 startup_code_cli 先例）。
export const TERMINAL_FONT_SIZE_KEY = 'terminal_font_size';
export const TERMINAL_FONT_SIZE_OPTIONS = [10, 11, 12, 13, 14, 15, 16] as const;
export type TerminalFontSize = (typeof TERMINAL_FONT_SIZE_OPTIONS)[number];
export const DEFAULT_TERMINAL_FONT_SIZE: TerminalFontSize = 12;

export function parseTerminalFontSize(value: string | null): TerminalFontSize {
  const parsed = value != null ? Number.parseInt(value, 10) : Number.NaN;
  return (TERMINAL_FONT_SIZE_OPTIONS as readonly number[]).includes(parsed)
    ? (parsed as TerminalFontSize)
    : DEFAULT_TERMINAL_FONT_SIZE;
}

// HTTP 本地服务端口（Go sidecar）。留空=用模式默认（由后端解析）。
// key/min/max 与模式默认端口均 Rust 单源（见顶部 re-export：app_config.rs + http_server.rs）。
// defaultHttpServerPort 与 Rust http_server.rs 的 default_port() 逻辑对应，
// 用于设置页帮助文案展示当前运行时的具体默认端口（而非 dev/release 并列）。
export function defaultHttpServerPort(mode: string): number {
  return mode === 'release' ? HTTP_SERVER_PORT_RELEASE : HTTP_SERVER_PORT_TEST;
}

// panel 窗口侧边栏折叠状态。值用 YesNo，缺失视为 NO（默认展开）。
// 纯前端偏好，后端不读取，故无需在 config.rs 加常量副本（参照 appearance 先例）。
export const PANEL_SIDEBAR_COLLAPSED_KEY = 'panel_sidebar_collapsed';
export const DEFAULT_PANEL_SIDEBAR_COLLAPSED = YES_NO.NO;

// 开发工作台左栏 issue 任务树折叠状态。值用 YesNo，缺失视为 NO（默认展开）。
// 同为纯前端偏好（参照 panel_sidebar_collapsed 先例）。
export const PANEL_DEV_TREE_COLLAPSED_KEY = 'panel_dev_tree_collapsed';
export const DEFAULT_PANEL_DEV_TREE_COLLAPSED = YES_NO.NO;

// 工作空间默认根目录（新建/导入项目类流程的目录选择起点）。空串 = 未设置，消费方自行兜底。
// 纯前端偏好，后端不读取，故无需在 config.rs 加常量副本（参照 panel_sidebar_collapsed 先例）。
export const WORKSPACE_BASE_DIR_KEY = 'workspace_base_dir';
export const DEFAULT_WORKSPACE_BASE_DIR = '';

// commands.xxx() 返回 tauri-specta 的 typedError 包装。unwrap 展开为 throw 风格，
// 保持 getAppConfig/setAppConfig 的对外 API 不变（错误时 throw）。
export async function getAppConfig(key: string): Promise<string | null> {
  return unwrap(commands.getAppConfig(key));
}

export async function setAppConfig(key: string, value: string): Promise<void> {
  await unwrap(commands.setAppConfig(key, value));
}
