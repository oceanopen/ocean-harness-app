import type { LiveViewportFrame } from '@src/state/browserSession';
import type { RefObject } from 'react';
import { useEffect } from 'react';
import { frameDisplaySize } from './viewportInput';

/**
 * useFramePaint 帧绘制 hook（rAF 消费循环，LiveViewport 与 MiniPreview 共用）：绘制与帧到达事件解耦——帧事件只更新 frameRef（keep-latest 单槽），
 * 循环每渲染帧以引用比对感知新帧、取最新帧解码绘制；绘制中到达的帧不重入、下一轮取最新——满帧率流至多每渲染帧一次解码绘制且零 React 重渲染。
 * canvas 现读 canvasRef.current（条件渲染/重挂天然适配，不得在 effect 作用域捕获实例）；backing 尺寸 = 帧尺寸、变化时先重设（清屏被同帧 drawImage 覆盖），保留末帧不清屏、坏帧丢弃自愈；rAF 在 webview 隐藏时暂停。
 */
export function useFramePaint(frameRef: RefObject<LiveViewportFrame | null>, canvasRef: RefObject<HTMLCanvasElement | null>): void {
  useEffect(() => {
    let raf = 0;
    let disposed = false;
    let painting = false; // 绘制在途（异步解码期）：期间到达的新帧留待下一轮循环合并
    let lastSeen: LiveViewportFrame | null = null; // 上次取走的帧（引用比对感知新帧）

    const paint = (canvas: HTMLCanvasElement, frame: LiveViewportFrame) => {
      /**
       * 同步段整体 try/catch：atob 对非法 base64 同步抛错，.catch 兜不住——不兜住则
       * painting 旗标永久卡死、后续帧全部不画。
       */
      try {
        const bin = atob(frame.data);
        const bytes = new Uint8Array(bin.length);
        for (let i = 0; i < bin.length; i++) {
          bytes[i] = bin.charCodeAt(i);
        }
        createImageBitmap(new Blob([bytes], { type: 'image/jpeg' })).then((bitmap) => {
          painting = false;
          if (disposed) {
            bitmap.close();
            return;
          }
          const size = frameDisplaySize(frame.metadata, bitmap);
          if (canvas.width !== size.width || canvas.height !== size.height) {
            canvas.width = size.width;
            canvas.height = size.height;
          }
          /**
           * 显式目标尺寸 = backing 尺寸：screencast jpeg 受 maxWidth/maxHeight 等比降采样，
           * 位图自然尺寸可能小于 metadata 真实视口尺寸——按自然尺寸画会铺不满 backing，
           * 未覆盖区域露出上一帧（双层画面叠加）。缩放绘制保证整帧覆盖。
           */
          canvas.getContext('2d')?.drawImage(bitmap, 0, 0, size.width, size.height);
          bitmap.close();
        }).catch(() => {
          painting = false;
          // 坏帧丢弃：keep-latest 下一帧自愈。
        });
      } catch {
        painting = false;
      }
    };

    const loop = () => {
      if (disposed) {
        return;
      }
      raf = requestAnimationFrame(loop);
      if (painting) {
        return; // 绘制中：本轮跳过，完成后下一轮取 frameRef 最新帧（天然合并跳帧）
      }
      // canvas 现读：消费组件按 hasFrame 条件渲染 canvas（占位态/重挂期可能为 null）。
      const canvas = canvasRef.current;
      const frame = frameRef.current;
      if (canvas == null || frame === lastSeen) {
        return;
      }
      lastSeen = frame;
      if (frame == null || frame.data === '') {
        return; // 清帧/空载荷：无可绘制内容（占位态由 hasFrame 驱动）
      }
      painting = true;
      paint(canvas, frame);
    };
    raf = requestAnimationFrame(loop);

    return () => {
      disposed = true;
      cancelAnimationFrame(raf);
    };
  }, [frameRef, canvasRef]);
}
