/**
 * 视口输入路由纯函数（T4.6）：坐标换算与键盘分流零状态，LiveViewport 只做事件编排
 * ——换算与分流规则表驱动可测（viewportInput.test.ts），「先换算后投送」时序在组件侧
 * 显式。
 */

/**
 * modifiersOf CDP modifiers 位掩码（Chromium Input 域约定：alt=1 ctrl=2 meta=4
 * shift=8）。
 */
export function modifiersOf(e: { altKey: boolean; ctrlKey: boolean; metaKey: boolean; shiftKey: boolean }): number {
  return (e.altKey ? 1 : 0) | (e.ctrlKey ? 2 : 0) | (e.metaKey ? 4 : 0) | (e.shiftKey ? 8 : 0);
}

/**
 * pointToPage canvas 内 css 偏移 → 页面设备像素坐标：等比系数 = 帧尺寸 / canvas client
 * 尺寸（x/y 各自对称；client 0 防除零退化 1:1——canvas 未布局时输入本就不发生）。
 */
export function pointToPage(
  offsetX: number,
  offsetY: number,
  clientWidth: number,
  clientHeight: number,
  frame: { width: number; height: number },
): { x: number; y: number } {
  const scaleX = clientWidth > 0 ? frame.width / clientWidth : 1;
  const scaleY = clientHeight > 0 ? frame.height / clientHeight : 1;
  return { x: offsetX * scaleX, y: offsetY * scaleY };
}

/**
 * frameDisplaySize 帧 backing store 尺寸：metadata 就绪即后端 screencast 实际像素；
 * 初始帧（captureScreenshot）metadata 零值回落位图自然尺寸——坐标换算以同一尺寸为
 * 基准，渲染与投送两态一致。
 */
export function frameDisplaySize(
  metadata: { deviceWidth: number; deviceHeight: number } | null | undefined,
  bitmap: { width: number; height: number },
): { width: number; height: number } {
  if (metadata != null && metadata.deviceWidth > 0 && metadata.deviceHeight > 0) {
    return { width: metadata.deviceWidth, height: metadata.deviceHeight };
  }
  return { width: bitmap.width, height: bitmap.height };
}

/**
 * buttonOf 鼠标键位 DOM button → CDP button 名（0/1/2 之外的设备键按 left 兜底，
 * 远程桌面消费面不含 back/forward）。
 */
export function buttonOf(button: number): 'left' | 'middle' | 'right' {
  if (button === 1) {
    return 'middle';
  }
  if (button === 2) {
    return 'right';
  }
  return 'left';
}

/**
 * heldButtonOf 移动事件携带的按住键（拖拽语义，DOM buttons 位掩码 1=left 2=right
 * 4=middle）；无按键按住返回 undefined（JSON 即省略，CDP 默认 none）。
 */
export function heldButtonOf(buttons: number): 'left' | 'middle' | 'right' | undefined {
  if ((buttons & 1) !== 0) {
    return 'left';
  }
  if ((buttons & 2) !== 0) {
    return 'right';
  }
  if ((buttons & 4) !== 0) {
    return 'middle';
  }
  return undefined;
}

/**
 * vkOf Windows 虚拟键码（CDP windowsVirtualKeyCode）：控制/功能键查表；其余取首字符
 * 大写 ASCII（字母/数字/常见标点的 VK 值与 ASCII 同值）。
 */
const VK_TABLE: Record<string, number> = {
  Backspace: 8,
  Tab: 9,
  Enter: 13,
  Shift: 16,
  Control: 17,
  Alt: 18,
  CapsLock: 20,
  Escape: 27,
  Space: 32,
  PageUp: 33,
  PageDown: 34,
  End: 35,
  Home: 36,
  ArrowLeft: 37,
  ArrowUp: 38,
  ArrowRight: 39,
  ArrowDown: 40,
  Insert: 45,
  Delete: 46,
  Meta: 91,
  ContextMenu: 93,
  F1: 112,
  F2: 113,
  F3: 114,
  F4: 115,
  F5: 116,
  F6: 117,
  F7: 118,
  F8: 119,
  F9: 120,
  F10: 121,
  F11: 122,
  F12: 123,
};

export function vkOf(key: string): number {
  const mapped = VK_TABLE[key];
  if (mapped !== undefined) {
    return mapped;
  }
  const ascii = key.toUpperCase().charCodeAt(0);
  return Number.isNaN(ascii) ? 0 : ascii;
}

/**
 * KeySignal 键盘事件分流入参（组件从 KeyboardEvent 摘取）。isComposing 含 key=
 * 'Process' 与 keyCode 229 兜底——部分浏览器组合期首个 keydown 先于 compositionstart，
 * isComposing 仍为 false。
 */
export interface KeySignal {
  key: string;
  code: string;
  isComposing: boolean;
  /** ctrl/alt/meta 按下（组合键不产生字符注入；shift 排除——shift+a 仍须输入 A）。 */
  hasTextBlockingModifier: boolean;
}

// 键盘路由结果（kind 恒 'key' 由组件补齐；text 仅 char 路径携带）。
export interface KeyRoute {
  action: 'char' | 'rawKeyDown' | 'keyUp';
  key: string;
  code: string;
  vk: number;
  text?: string;
}

/**
 * routeKeyDown keydown 分流：组合期不转发（IME 文本整体走 compositionend → insertText）；
 * 可打印字符且无组合修饰 → char（text 承载字符）；控制键/修饰组合键 → rawKeyDown
 * （ctrl+c 等不得注入字符，页面按 modifiers 自行解释快捷键）。
 */
export function routeKeyDown(sig: KeySignal): KeyRoute | null {
  if (sig.isComposing) {
    return null;
  }
  if (sig.key.length === 1 && !sig.hasTextBlockingModifier) {
    return { action: 'char', key: sig.key, code: sig.code, vk: vkOf(sig.key), text: sig.key };
  }
  return { action: 'rawKeyDown', key: sig.key, code: sig.code, vk: vkOf(sig.key) };
}

/**
 * routeKeyUp keyup 分流：组合期不转发；其余一律 keyUp（CDP char 事件即完整注入，
 * keyup 仅补释放语义，字符与控制键同构）。
 */
export function routeKeyUp(sig: KeySignal): KeyRoute | null {
  if (sig.isComposing) {
    return null;
  }
  return { action: 'keyUp', key: sig.key, code: sig.code, vk: vkOf(sig.key) };
}
