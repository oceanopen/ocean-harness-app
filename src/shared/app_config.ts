import type { YesNo } from './bindings';
import { commands } from './bindings';
import { unwrap } from './commands';

// 本文件是所有配置项 key + 默认值的唯一可信源 (SSOT)。
// 后端 src-tauri/src/shared/config.rs 中 LANGUAGE_KEY 有对应常量副本（用于托盘菜单语言判定），
// 修改任一 *KEY / DEFAULT_* 时必须同步后端，否则首次启动会出现前后端兜底不一致。

// Y/N 布尔风格配置值。类型 YesNo 由后端 types.rs 的 enum 经 gen:bindings 自动生成，
// 此处只保留值常量（specta 不导出 const），用 satisfies 关联后端类型确保取值合法：
// 后端 #[serde(rename = "Y"/"N")] 改动后，此处值若不一致会编译报错。
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

export const LANGUAGE_KEY = 'language';
export const DEFAULT_LANGUAGE: Language = 'system';

// 桌宠拖拽开关。值用 YesNo，缺失视为 NO（默认关闭：点击桌宠打开监控页）。
// 与后端 config.rs 的 PET_CLAUDE_SESSIONS_SUMMARY_DRAGGABLE_KEY 对齐，修改任一处需同步另一处。
export const PET_CLAUDE_SESSIONS_SUMMARY_DRAGGABLE_KEY = 'pet_claude_sessions_summary_draggable';
export const DEFAULT_PET_DRAGGABLE = YES_NO.NO;

// sessions 兜底轮询周期（秒）。即时性由 fs watcher 负责，此处仅驱动 Dead 老化与漏报兜底。
// min/max/clamp 与后端 config.rs 镜像，改动任一处需同步另一处。
export const POLL_INTERVAL_SECS_KEY = 'poll_interval_secs';
export const DEFAULT_POLL_INTERVAL_SECS = 60;
export const MIN_POLL_INTERVAL_SECS = 5;
export const MAX_POLL_INTERVAL_SECS = 120;

// iTerm2 分屏方向。horizontal = 上下分屏，vertical = 左右分屏，none = 不分屏。
// 与后端 config.rs 镜像，改动任一处需同步另一处。
export type Iterm2SplitDirection = 'horizontal' | 'vertical' | 'none';

export const ITERM2_SPLIT_DIRECTION_KEY = 'iterm2_split_direction';
export const DEFAULT_ITERM2_SPLIT_DIRECTION: Iterm2SplitDirection = 'horizontal';

// 打开终端 cd 后追加执行的命令（全局，iTerm2 与 Terminal.app 共用）。空串 = 仅 cd。
// 与后端 app_config.rs 镜像，改动任一处需同步另一处。
export const TERMINAL_POST_OPEN_COMMAND_KEY = 'terminal_post_open_command';
export const DEFAULT_TERMINAL_POST_OPEN_COMMAND = '';

// panel 窗口侧边栏折叠状态。值用 YesNo，缺失视为 NO（默认展开）。
// 纯前端偏好，后端不读取，故无需在 config.rs 加常量副本（参照 appearance 先例）。
export const PANEL_SIDEBAR_COLLAPSED_KEY = 'panel_sidebar_collapsed';
export const DEFAULT_PANEL_SIDEBAR_COLLAPSED = YES_NO.NO;

// commands.xxx() 返回 tauri-specta 的 typedError 包装。unwrap 展开为 throw 风格，
// 保持 getAppConfig/setAppConfig 的对外 API 不变（错误时 throw）。
export async function getAppConfig(key: string): Promise<string | null> {
  return unwrap(commands.getAppConfig(key));
}

export async function setAppConfig(key: string, value: string): Promise<void> {
  await unwrap(commands.setAppConfig(key, value));
}
