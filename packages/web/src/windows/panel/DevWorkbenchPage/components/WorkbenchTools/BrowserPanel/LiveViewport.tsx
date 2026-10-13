import type { BrowserSessionInputRequest } from '@src/services';
import type { LiveViewportFrame } from '@src/state/browserSession';
import type { RefObject } from 'react';
import { Box, Typography } from '@mui/material';
import { BrowserSessionService } from '@src/services';
import { useEffect, useRef } from 'react';
import { useFramePaint } from './useFramePaint';
import { buttonOf, heldButtonOf, modifiersOf, pointToPage, routeKeyDown, routeKeyUp } from './viewportInput';

type InputDispatch = Omit<BrowserSessionInputRequest, 'profile'>;

interface LiveViewportProps {
  profile: string;
  /** keep-latest 帧箱（useBrowserFrameStream 持有），useFramePaint rAF 循环消费。 */
  frameRef: RefObject<LiveViewportFrame | null>;
  /** 是否已有帧（占位态切换；首帧置 true、重订清帧置 false）。 */
  hasFrame: boolean;
  /** live 态才受理输入（未首帧/自愈/终态不投送）。 */
  interactive: boolean;
}

/**
 * LiveViewport：操作台实时视口（canvas）——帧到达 createImageBitmap → drawImage（显式
 * 目标尺寸铺满 backing），保留末帧不清屏（静态页零推帧与终态均定格最后画面）；canvas
 * backing 尺寸 = 帧尺寸（metadata.deviceWidth/Height，初始帧按位图兜底），CSS
 * max-width/max-height:100% contain 等比缩放居中（替换元素原生约束表，容器拖拽宽度
 * 实时重排；小于容器不放大，留白即「播放器黑边」）。
 *
 * 输入路由（远程桌面语义）：鼠标事件直接挂 canvas（串行发送队列逐个投送——REST 无到达
 * 顺序保证，press/release 乱序即丢点击；背压期连续 mouseMoved 合并；mousedown
 * preventDefault + pointer capture，拖拽出画布事件不断流，捕获期 pointercancel 补发
 * release）；滚轮原生非被动监听阻断面板滚动；**键盘与 IME 挂隐藏 input**——canvas 非
 * 可编辑元素，浏览器不会在其上启动输入法组合（英文走 keydown char 正常、中文
 * compositionend 永不触发），焦点由隐藏 input 承载（canvas mousedown 显式转焦，
 * noVNC 同款），组合期 keydown 不 preventDefault 不转发（IME 自由组合），
 * compositionend 整体 insertText，可打印字符走 char、控制键 rawKeyDown/keyUp——坐标
 * 与按键经 viewportInput 纯函数换算后 POST /input，换算基准为 canvas 自身实测 rect
 *（contain 留白天然在矩形之外，无需补偿）。
 */
export default function LiveViewport({ profile, frameRef, hasFrame, interactive }: LiveViewportProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);
  // 串行发送队列：buffer 保序、pump 单飞逐个投送；背压期队列尾 move 被最新 move 覆写。
  const bufferRef = useRef<InputDispatch[]>([]);
  const pumpingRef = useRef(false);
  // 按住的鼠标键（pointer capture 期 pointercancel 补发 release 用；up/cancel 清零）。
  const heldRef = useRef<'left' | 'middle' | 'right' | null>(null);

  const send = (req: InputDispatch): Promise<void> => BrowserSessionService.input({ profile, ...req }).then(() => undefined, (err: unknown) => {
    // 尽力语义不中断交互，但留排查痕迹（「浏览器未在投送」等服务端错误不再静默）。
    console.warn('[browserSession] 输入投送失败:', err);
  });

  const pump = () => {
    if (pumpingRef.current) {
      return;
    }
    pumpingRef.current = true;
    const step = () => {
      const next = bufferRef.current.shift();
      if (next == null) {
        pumpingRef.current = false;
        return;
      }
      send(next).then(step, step);
    };
    step();
  };

  const dispatch = (req: InputDispatch) => {
    if (!interactive) {
      return;
    }
    // 背压期连续 mouseMoved 合并（队列尾已是 move 则覆写）：press/release 有序且延迟
    // 不堆积；队列空直入，hover 实时不受影响。
    if (req.kind === 'mouse' && req.action === 'mouseMoved' && bufferRef.current.length > 0) {
      const tail = bufferRef.current[bufferRef.current.length - 1];
      if (tail.kind === 'mouse' && tail.action === 'mouseMoved') {
        bufferRef.current[bufferRef.current.length - 1] = req;
        return;
      }
    }
    bufferRef.current.push(req);
    pump();
  };

  // pagePoint 客户端坐标 → 页面设备像素坐标（canvas backing 尺寸即帧尺寸，换算基准）。
  const pagePoint = (canvas: HTMLCanvasElement, clientX: number, clientY: number) => {
    const rect = canvas.getBoundingClientRect();
    return pointToPage(clientX - rect.left, clientY - rect.top, rect.width, rect.height, {
      width: canvas.width,
      height: canvas.height,
    });
  };

  // 帧绘制（rAF 消费循环，与帧事件解耦）：backing 尺寸与帧尺寸一致（公共 hook，与迷你预览同源）。
  useFramePaint(frameRef, canvasRef);

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
          flex: 1,
          minHeight: 0,
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

  const keySignal = (e: React.KeyboardEvent<HTMLInputElement>) => ({
    key: e.key,
    code: e.code,
    isComposing: e.nativeEvent.isComposing || e.key === 'Process' || e.keyCode === 229,
    hasTextBlockingModifier: e.ctrlKey || e.altKey || e.metaKey,
  });

  return (
    <Box
      sx={{
        flex: 1,
        minHeight: 0,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        border: 1,
        borderColor: 'divider',
        borderRadius: 1,
        bgcolor: '#000',
        overflow: 'hidden',
        lineHeight: 0,
        userSelect: 'none',
        position: 'relative',
      }}
    >
      <canvas
        ref={canvasRef}
        aria-label="页面实时画面"
        style={{ display: 'block', maxWidth: '100%', maxHeight: '100%', outline: 'none' }}
        onPointerDown={(e) => {
          // 远程桌面获焦 + 捕获：preventDefault 杜绝面板文本选择/默认焦点迁移抢事件，
          // 焦点显式转给隐藏 input（键盘/IME 宿主）；capture 后拖拽出画布 pointermove/up
          // 持续派发到画布。
          e.preventDefault();
          inputRef.current?.focus();
          try {
            e.currentTarget.setPointerCapture(e.pointerId);
          } catch {
            // 指针已释放等场景：捕获失败退化为仅画布内监听。
          }
          const button = buttonOf(e.button);
          heldRef.current = button;
          const p = pagePoint(e.currentTarget, e.clientX, e.clientY);
          dispatch({
            kind: 'mouse',
            action: 'mousePressed',
            x: p.x,
            y: p.y,
            button,
            clickCount: Math.max(1, e.detail),
            modifiers: modifiersOf(e),
          });
        }}
        onPointerUp={(e) => {
          const button = heldRef.current ?? buttonOf(e.button);
          heldRef.current = null;
          try {
            e.currentTarget.releasePointerCapture(e.pointerId);
          } catch {
            // 捕获已释放（up 天然释放）。
          }
          const p = pagePoint(e.currentTarget, e.clientX, e.clientY);
          dispatch({
            kind: 'mouse',
            action: 'mouseReleased',
            x: p.x,
            y: p.y,
            button,
            clickCount: Math.max(1, e.detail),
            modifiers: modifiersOf(e),
          });
        }}
        onPointerCancel={(e) => {
          // 捕获期被系统打断（alt-tab 等）：补发 release，页面不停留「按住」态。
          if (heldRef.current == null) {
            return;
          }
          const button = heldRef.current;
          heldRef.current = null;
          try {
            e.currentTarget.releasePointerCapture(e.pointerId);
          } catch {
            // 捕获已随取消释放。
          }
          const p = pagePoint(e.currentTarget, e.clientX, e.clientY);
          dispatch({ kind: 'mouse', action: 'mouseReleased', x: p.x, y: p.y, button, modifiers: modifiersOf(e) });
        }}
        onMouseMove={(e) => {
          const p = pagePoint(e.currentTarget, e.clientX, e.clientY);
          dispatch({
            kind: 'mouse',
            action: 'mouseMoved',
            x: p.x,
            y: p.y,
            button: heldRef.current ?? heldButtonOf(e.buttons),
            modifiers: modifiersOf(e),
          });
        }}
      />
      {/* 键盘/IME 宿主（不可见，1px）：canvas 非可编辑元素，IME 组合无法在其上启动；
          焦点经 canvas mousedown 显式转入。data 标记供浮层 Esc 处理器让位（焦点在本
          input 即远端页面键盘交互中，Esc 先服务远端）。 */}
      <input
        ref={inputRef}
        data-browser-stage-input="true"
        aria-label="页面键盘输入"
        autoComplete="off"
        autoCorrect="off"
        autoCapitalize="off"
        spellCheck={false}
        style={{
          position: 'absolute',
          left: 0,
          bottom: 0,
          width: 2,
          height: 2,
          opacity: 0,
          border: 'none',
          padding: 0,
          pointerEvents: 'none',
          caretColor: 'transparent',
          outline: 'none',
        }}
        onKeyDown={(e) => {
          // 组合期不拦截不转发（IME 自由组合，compositionend 整体注入）；其余按键
          // preventDefault（不落隐藏 input 的 value、不触发浏览器快捷键语义）后路由。
          const sig = keySignal(e);
          if (sig.isComposing) {
            return;
          }
          e.preventDefault();
          const route = routeKeyDown(sig);
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
          // 组合文本整体 insertText（kind:text）：中英输入同一通道，不经 key 事件拆分；
          // 组合期积在 value 里的文本就地清空（value 恒空，防回填累积）。
          e.currentTarget.value = '';
          if (e.data !== '') {
            dispatch({ kind: 'text', text: e.data });
          }
        }}
        onInput={(e) => {
          // 防御：非组合期落入 value 的文本（浏览器旁路 keydown 的注入路径）即清，
          // 文本统一走 compositionend/char 通道。
          if (!e.nativeEvent.isComposing) {
            e.currentTarget.value = '';
          }
        }}
      />
    </Box>
  );
}
