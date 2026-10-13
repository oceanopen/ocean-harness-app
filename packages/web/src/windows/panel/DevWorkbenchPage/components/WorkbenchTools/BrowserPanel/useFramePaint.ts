import type { LiveViewportFrame } from '@src/state/browserSession';
import type { RefObject } from 'react';
import { useEffect } from 'react';
import { frameDisplaySize } from './viewportInput';

/**
 * useFramePaint 帧绘制 hook（tick 驱动，LiveViewport 与 MiniPreview 共用）：backing
 * 尺寸 = 帧尺寸（metadata.deviceWidth/Height 优先，初始帧按位图兜底），变化时先重设
 * （重设清屏被同帧 drawImage 覆盖，末帧语义不受影响）；保留末帧不清屏——静态页零推帧
 * 与终态均定格最后画面。坏帧丢弃（keep-latest 下一帧自愈）。
 */
export function useFramePaint(frameRef: RefObject<LiveViewportFrame | null>, tick: number, canvasRef: RefObject<HTMLCanvasElement | null>): void {
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
      // 显式目标尺寸 = backing 尺寸：screencast jpeg 受 maxWidth/maxHeight 等比降采样，
      // 位图自然尺寸可能小于 metadata 真实视口尺寸——按自然尺寸画会铺不满 backing，
      // 未覆盖区域露出上一帧（双层画面叠加）。缩放绘制保证整帧覆盖。
      canvas.getContext('2d')?.drawImage(bitmap, 0, 0, size.width, size.height);
      bitmap.close();
    }).catch(() => {
      // 坏帧丢弃：keep-latest 下一帧自愈。
    });
    return () => {
      cancelled = true;
    };
  }, [tick, frameRef, canvasRef]);
}
