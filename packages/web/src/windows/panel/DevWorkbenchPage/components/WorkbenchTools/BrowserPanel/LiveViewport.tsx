import type { BrowserSessionInputRequest } from '@src/services';
import type { LiveViewportFrame } from '@src/state/browserSession';
import type { RefObject } from 'react';
import { Box, Typography } from '@mui/material';
import { BrowserSessionService } from '@src/services';
import { useEffect, useRef } from 'react';
import { buttonOf, frameDisplaySize, heldButtonOf, modifiersOf, pointToPage, routeKeyDown, routeKeyUp } from './viewportInput';

interface LiveViewportProps {
  profile: string;
  /** keep-latest 帧箱（useBrowserFrameStream 持有），tick 驱动重绘。 */
  frameRef: RefObject<LiveViewportFrame | null>;
  /** 帧到达序号（重订清帧也 +1，驱动占位态切换与重绘）。 */
  tick: number;
  /** live 态才受理输入（未首帧/自愈/终态不投送）。 */
  interactive: boolean;
}

/**
 * LiveViewport：会话卡实时视口（canvas）——帧到达 createImageBitmap → drawImage，保留
 * 末帧不清屏（静态页零推帧与终态均定格最后画面）；canvas backing 尺寸 = 帧尺寸
 * （metadata.deviceWidth/Height，初始帧按位图兜底），CSS 等比撑满卡片宽。输入路由
 * （远程桌面语义）：鼠标三态（clickCount 取原生双击聚合）+ 滚轮（原生非被动监听阻断
 * 面板滚动）+ 键盘（canvas 获焦才接收，失焦天然不投送；IME 组合期不转发、
 * compositionend 走 insertText、可打印字符走 char、控制键 rawKeyDown/keyUp）——坐标
 * 与按键经 viewportInput 纯函数换算后 POST /input，分发失败静默（尽力语义，自愈
 * 重连后恢复）。
 */
export default function LiveViewport({ profile, frameRef, tick, interactive }: LiveViewportProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const hasFrame = frameRef.current != null && frameRef.current.data !== '';

  const dispatch = (req: Omit<BrowserSessionInputRequest, 'profile'>) => {
    if (!interactive) {
      return;
    }
    BrowserSessionService.input({ profile, ...req }).catch(() => {});
  };

  // pagePoint 客户端坐标 → 页面设备像素坐标（canvas backing 尺寸即帧尺寸，换算基准）。
  const pagePoint = (canvas: HTMLCanvasElement, clientX: number, clientY: number) => {
    const rect = canvas.getBoundingClientRect();
    return pointToPage(clientX - rect.left, clientY - rect.top, rect.width, rect.height, {
      width: canvas.width,
      height: canvas.height,
    });
  };

  // 帧绘制（tick 驱动）：backing 尺寸与帧尺寸一致，变化时先重设（重设清屏被同帧
  // drawImage 覆盖，末帧语义不受影响）。
  useEffect(() => {
    const canvas = canvasRef.current;
    const frame = frameRef.current;
    if (canvas == null || frame == null || frame.data === '') {
      return;
    }
    let cancelled = false;
    const bin = atob(frame.data);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) {
      bytes[i] = bin.charCodeAt(i);
    }
    createImageBitmap(new Blob([bytes], { type: 'image/jpeg' })).then((bitmap) => {
      if (cancelled) {
        bitmap.close();
        return;
      }
      const size = frameDisplaySize(frame.metadata, bitmap);
      if (canvas.width !== size.width || canvas.height !== size.height) {
        canvas.width = size.width;
        canvas.height = size.height;
      }
      canvas.getContext('2d')?.drawImage(bitmap, 0, 0);
      bitmap.close();
    }).catch(() => {
      // 坏帧丢弃：keep-latest 下一帧自愈。
    });
    return () => {
      cancelled = true;
    };
  }, [tick, frameRef]);

  // 滚轮须原生绑定 passive:false——React 合成 onWheel 为被动监听，preventDefault 失效。
  useEffect(() => {
    const canvas = canvasRef.current;
    if (canvas == null) {
      return;
    }
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      if (!interactive) {
        return;
      }
      const p = pagePoint(canvas, e.clientX, e.clientY);
      dispatch({ kind: 'wheel', x: p.x, y: p.y, deltaX: e.deltaX, deltaY: e.deltaY });
    };
    canvas.addEventListener('wheel', onWheel, { passive: false });
    return () => canvas.removeEventListener('wheel', onWheel);
  }, [interactive, profile]);

  if (!hasFrame) {
    return (
      <Box
        sx={{
          height: 160,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          border: 1,
          borderColor: 'divider',
          borderRadius: 1,
          bgcolor: 'action.hover',
        }}
      >
        <Typography variant="body2" color="text.secondary">等待实时画面…</Typography>
      </Box>
    );
  }

  const keySignal = (e: React.KeyboardEvent<HTMLCanvasElement>) => ({
    key: e.key,
    code: e.code,
    isComposing: e.nativeEvent.isComposing || e.key === 'Process' || e.keyCode === 229,
    hasTextBlockingModifier: e.ctrlKey || e.altKey || e.metaKey,
  });

  return (
    <Box sx={{ border: 1, borderColor: 'divider', borderRadius: 1, bgcolor: '#000', overflow: 'hidden', lineHeight: 0 }}>
      <canvas
        ref={canvasRef}
        tabIndex={0}
        aria-label="页面实时画面"
        style={{ display: 'block', width: '100%', height: 'auto', outline: 'none' }}
        onMouseDown={(e) => {
          const p = pagePoint(e.currentTarget, e.clientX, e.clientY);
          dispatch({
            kind: 'mouse',
            action: 'mousePressed',
            x: p.x,
            y: p.y,
            button: buttonOf(e.button),
            clickCount: Math.max(1, e.detail),
            modifiers: modifiersOf(e),
          });
        }}
        onMouseUp={(e) => {
          const p = pagePoint(e.currentTarget, e.clientX, e.clientY);
          dispatch({
            kind: 'mouse',
            action: 'mouseReleased',
            x: p.x,
            y: p.y,
            button: buttonOf(e.button),
            clickCount: Math.max(1, e.detail),
            modifiers: modifiersOf(e),
          });
        }}
        onMouseMove={(e) => {
          const p = pagePoint(e.currentTarget, e.clientX, e.clientY);
          dispatch({
            kind: 'mouse',
            action: 'mouseMoved',
            x: p.x,
            y: p.y,
            button: heldButtonOf(e.buttons),
            modifiers: modifiersOf(e),
          });
        }}
        onKeyDown={(e) => {
          // 远程桌面获焦：按键不回落面板滚动/快捷键语义。
          e.preventDefault();
          const route = routeKeyDown(keySignal(e));
          if (route == null) {
            return;
          }
          dispatch({
            kind: 'key',
            action: route.action,
            key: route.key,
            code: route.code,
            vk: route.vk,
            text: route.text,
            modifiers: modifiersOf(e),
          });
        }}
        onKeyUp={(e) => {
          const route = routeKeyUp(keySignal(e));
          if (route == null) {
            return;
          }
          dispatch({ kind: 'key', action: route.action, key: route.key, code: route.code, vk: route.vk, modifiers: modifiersOf(e) });
        }}
        onCompositionEnd={(e) => {
          // 组合文本整体 insertText（kind:text）：中英输入同一通道，不经 key 事件拆分。
          if (e.data !== '') {
            dispatch({ kind: 'text', text: e.data });
          }
        }}
      />
    </Box>
  );
}
