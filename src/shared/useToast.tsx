import type { ReactElement } from 'react';
import { Alert, Snackbar } from '@mui/material';
import { useCallback, useState } from 'react';

export type ToastSeverity = 'success' | 'error';

export interface UseToastResult {
  /** 弹出一条 toast（保留最近一次内容，退出动画期间不闪烁）。 */
  show: (text: string, severity: ToastSeverity) => void;
  /** 渲染到组件 JSX 末尾的 Snackbar（open/autoHideDuration/anchorOrigin 在此 SSOT）。 */
  snack: ReactElement;
}

// 统一 toast：消除各页面重复的 toast/toastOpen state + showToast + <Snackbar> 样板。
// autoHideDuration / anchorOrigin 在此 SSOT，避免散落不同值。
export function useToast(): UseToastResult {
  const [toast, setToast] = useState<{ text: string; severity: ToastSeverity }>({ text: '', severity: 'success' });
  const [open, setOpen] = useState(false);

  const show = useCallback((text: string, severity: ToastSeverity) => {
    setToast({ text, severity });
    setOpen(true);
  }, []);

  const snack = (
    <Snackbar
      open={open}
      autoHideDuration={2000}
      onClose={() => setOpen(false)}
      anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
    >
      <Alert severity={toast.severity} variant="filled">{toast.text}</Alert>
    </Snackbar>
  );

  return { show, snack };
}
