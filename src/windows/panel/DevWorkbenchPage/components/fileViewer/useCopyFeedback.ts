import { useCallback, useState } from 'react';

/// 复制反馈（项目统一剪贴板模式）：copy(text) → writeText → copied 置位 2 秒自动回退。
/// ViewerToolbar 全文复制与 md 代码块头部复制共用（原先两份平行实现）；成功态图标/
/// 文案（CheckIcon + success 色 / Tooltip 切换）由调用方渲染。React 18 下组件卸载后的
/// setState 回退为 no-op，无需清理定时器。
export function useCopyFeedback() {
  const [copied, setCopied] = useState(false);
  const copy = useCallback((text: string) => {
    void navigator.clipboard.writeText(text).then(() => {
      setCopied(true);
      setTimeout(setCopied, 2000, false);
    });
  }, []);
  return { copied, copy };
}
