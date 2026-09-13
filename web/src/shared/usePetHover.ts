import { getCurrentWindow } from '@tauri-apps/api/window';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

// 桌宠类悬浮窗口（pet / pet_task）共用的高亮状态 hook。
//
// 高亮 = opacity 在 1（亮）/ 0.3（暗）间切换，由 hovered 驱动。触发规则：
//   - 鼠标真实移入（mouseenter 后伴随 mousemove）→ 高亮
//   - 鼠标按下（mousedown，未聚焦窗口首次点击的兜底）→ 高亮
//   - 鼠标移出（mouseleave）/ 窗口失焦 → 取消
//
// 为何 mouseenter 不直接高亮、要等 mousemove 确认：
// macOS 在窗口 show() / set_position() 后，若鼠标光标落在窗口矩形内，会派发一个
// "合成 mouseenter"（鼠标并未真实移动）。若直接据此高亮，会话状态变化 → count 变 →
// pet_task 窗口 show + 重定位 → 合成 mouseenter → 被误判为用户 hover → 自动高亮。
// 合成事件不伴随 mousemove，真实 hover 必伴随 mousemove，借此精确区分两者。
//
// reset()：窗口被动 show / 重定位后调用（如 pet_task 的 REFIT 监听），清掉残留的 hovered
// （hide 时 mouseleave 不一定触发）并丢弃尚未确认的 pendingEnterRef，确保弹出即暗态。

export interface PetHoverHandlers {
  onMouseEnter: () => void;
  onMouseMove: () => void;
  onMouseLeave: () => void;
  onMouseDown: () => void;
}

export interface PetHoverResult {
  hovered: boolean;
  handlers: PetHoverHandlers;
  reset: () => void;
}

export function usePetHover(): PetHoverResult {
  const [hovered, setHovered] = useState(false);
  // mouseenter 已发生、但尚未经 mousemove 确认为"真实用户 hover"。
  const pendingEnterRef = useRef(false);

  // 窗口失焦（打开新窗口 / 切换应用等）时清除 hover：失焦时鼠标常仍停在窗口内，
  // mouseleave 不触发，需监听 Tauri focus 变化兜底。
  useEffect(() => {
    const unlisten = getCurrentWindow().onFocusChanged(({ payload: focused }) => {
      if (!focused) {
        pendingEnterRef.current = false;
        setHovered(false);
      }
    });
    return () => {
      unlisten
        .then(fn => fn())
        .catch(err => console.warn('[usePetHover] onFocusChanged unlisten failed:', err));
    };
  }, []);

  const reset = useCallback(() => {
    pendingEnterRef.current = false;
    setHovered(false);
  }, []);

  // 稳定引用：仅依赖稳定的 setHovered 与 ref，空依赖即可，消费者不会因 handler 重建而重渲染。
  const handlers = useMemo<PetHoverHandlers>(
    () => ({
      // 不立即高亮：等 mousemove 确认，过滤 show / set_position 产生的合成 mouseenter。
      onMouseEnter: () => {
        pendingEnterRef.current = true;
      },
      // mousemove 紧随真实的 mouseenter（用户确实在移动鼠标）；合成事件无 mousemove。
      onMouseMove: () => {
        if (pendingEnterRef.current) {
          pendingEnterRef.current = false;
          setHovered(true);
        }
      },
      onMouseLeave: () => {
        pendingEnterRef.current = false;
        setHovered(false);
      },
      // 点击即高亮：未聚焦窗口的 mouseenter 不触发，mousedown 是"鼠标在窗口内"最可靠的信号。
      onMouseDown: () => {
        pendingEnterRef.current = false;
        setHovered(true);
      },
    }),
    [],
  );

  return { hovered, handlers, reset };
}
