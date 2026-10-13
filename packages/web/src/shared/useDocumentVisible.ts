import { useEffect, useState } from 'react';

/**
 * useDocumentVisible：webview 可见性订阅（document.visibilitychange）——窗口隐藏
 * （关托盘/最小化）时为 false，恢复显示即回 true。隐藏期 webview 不卸载组件树，订阅类
 * 副作用（SSE 帧流、轮询）须自行以此闸门停订。初值取同步 visibilityState，首帧即终值
 * （编码规则 1）。
 */
export function useDocumentVisible(): boolean {
  const [visible, setVisible] = useState(() => document.visibilityState !== 'hidden');

  useEffect(() => {
    const onVisibilityChange = () => setVisible(document.visibilityState !== 'hidden');
    document.addEventListener('visibilitychange', onVisibilityChange);
    return () => document.removeEventListener('visibilitychange', onVisibilityChange);
  }, []);

  return visible;
}
