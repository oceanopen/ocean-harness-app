import { describe, expect, it } from 'vitest';
import {
  buttonOf,
  frameDisplaySize,
  heldButtonOf,
  modifiersOf,
  pointToPage,
  routeKeyDown,
  routeKeyUp,
  vkOf,
} from './viewportInput';

// 视口输入路由纯函数单测（T4.6）：坐标换算/帧尺寸兜底/键位映射/键盘分流表驱动。

describe('modifiersOf', () => {
  it('修饰键位掩码（CDP 约定 alt=1 ctrl=2 meta=4 shift=8）', () => {
    expect(modifiersOf({ altKey: false, ctrlKey: false, metaKey: false, shiftKey: false })).toBe(0);
    expect(modifiersOf({ altKey: true, ctrlKey: false, metaKey: false, shiftKey: false })).toBe(1);
    expect(modifiersOf({ altKey: false, ctrlKey: true, metaKey: false, shiftKey: false })).toBe(2);
    expect(modifiersOf({ altKey: false, ctrlKey: false, metaKey: true, shiftKey: false })).toBe(4);
    expect(modifiersOf({ altKey: false, ctrlKey: false, metaKey: false, shiftKey: true })).toBe(8);
    expect(modifiersOf({ altKey: false, ctrlKey: true, metaKey: false, shiftKey: true })).toBe(10);
  });
});

describe('pointToPage', () => {
  it('等比放大（2x）', () => {
    expect(pointToPage(30, 40, 240, 320, { width: 480, height: 640 })).toEqual({ x: 60, y: 80 });
  });

  it('x/y 系数各自独立', () => {
    expect(pointToPage(10, 10, 100, 200, { width: 100, height: 100 })).toEqual({ x: 10, y: 5 });
  });

  it('client 0 防除零退化 1:1', () => {
    expect(pointToPage(10, 20, 0, 0, { width: 480, height: 640 })).toEqual({ x: 10, y: 20 });
  });

  it('小数坐标换算', () => {
    const p = pointToPage(1.5, 2.5, 100, 100, { width: 300, height: 300 });
    expect(p.x).toBeCloseTo(4.5);
    expect(p.y).toBeCloseTo(7.5);
  });
});

describe('frameDisplaySize', () => {
  it('metadata 就绪用 device 尺寸', () => {
    expect(frameDisplaySize({ deviceWidth: 960, deviceHeight: 600 }, { width: 1, height: 1 }))
      .toEqual({ width: 960, height: 600 });
  });

  it('初始帧 metadata 零值/缺失回落位图尺寸', () => {
    expect(frameDisplaySize({ deviceWidth: 0, deviceHeight: 0 }, { width: 800, height: 500 }))
      .toEqual({ width: 800, height: 500 });
    expect(frameDisplaySize(undefined, { width: 800, height: 500 }))
      .toEqual({ width: 800, height: 500 });
  });
});

describe('buttonOf / heldButtonOf', () => {
  it('鼠标键位名映射（DOM button → CDP）', () => {
    expect(buttonOf(0)).toBe('left');
    expect(buttonOf(1)).toBe('middle');
    expect(buttonOf(2)).toBe('right');
    expect(buttonOf(3)).toBe('left');
  });

  it('buttons 位掩码取按住键，无按键 undefined', () => {
    expect(heldButtonOf(0)).toBeUndefined();
    expect(heldButtonOf(1)).toBe('left');
    expect(heldButtonOf(2)).toBe('right');
    expect(heldButtonOf(4)).toBe('middle');
    expect(heldButtonOf(3)).toBe('left');
  });
});

describe('vkOf', () => {
  it('控制/功能键查表', () => {
    expect(vkOf('Enter')).toBe(13);
    expect(vkOf('Backspace')).toBe(8);
    expect(vkOf('ArrowDown')).toBe(40);
    expect(vkOf('F5')).toBe(116);
  });

  it('可打印字符取大写 ASCII（数字/字母/标点）', () => {
    expect(vkOf('a')).toBe(65);
    expect(vkOf('A')).toBe(65);
    expect(vkOf('1')).toBe(49);
    expect(vkOf(' ')).toBe(32);
  });
});

describe('routeKeyDown', () => {
  const sig = (overrides: Partial<Parameters<typeof routeKeyDown>[0]> = {}) => ({
    key: 'a',
    code: 'KeyA',
    isComposing: false,
    hasTextBlockingModifier: false,
    ...overrides,
  });

  it('组合期不转发（IME 文本走 compositionend）', () => {
    expect(routeKeyDown(sig({ isComposing: true }))).toBeNull();
  });

  it('可打印字符走 char 带 text 与 vk', () => {
    expect(routeKeyDown(sig())).toEqual({ action: 'char', key: 'a', code: 'KeyA', vk: 65, text: 'a' });
  });

  it('ctrl/alt/meta 组合走 rawKeyDown 不注入字符', () => {
    expect(routeKeyDown(sig({ key: 'c', code: 'KeyC', hasTextBlockingModifier: true })))
      .toEqual({ action: 'rawKeyDown', key: 'c', code: 'KeyC', vk: 67 });
  });

  it('控制键走 rawKeyDown', () => {
    expect(routeKeyDown(sig({ key: 'Enter', code: 'Enter' })))
      .toEqual({ action: 'rawKeyDown', key: 'Enter', code: 'Enter', vk: 13 });
  });

  it('shift 单修饰不阻断字符（shift+a 仍输入）', () => {
    expect(routeKeyDown(sig({ key: 'A' }))).toEqual({ action: 'char', key: 'A', code: 'KeyA', vk: 65, text: 'A' });
  });
});

describe('routeKeyUp', () => {
  it('组合期不转发', () => {
    expect(routeKeyUp({ key: 'a', code: 'KeyA', isComposing: true, hasTextBlockingModifier: false })).toBeNull();
  });

  it('其余一律 keyUp 补释放（无 text）', () => {
    expect(routeKeyUp({ key: 'a', code: 'KeyA', isComposing: false, hasTextBlockingModifier: false }))
      .toEqual({ action: 'keyUp', key: 'a', code: 'KeyA', vk: 65 });
    expect(routeKeyUp({ key: 'Enter', code: 'Enter', isComposing: false, hasTextBlockingModifier: true }))
      .toEqual({ action: 'keyUp', key: 'Enter', code: 'Enter', vk: 13 });
  });
});
