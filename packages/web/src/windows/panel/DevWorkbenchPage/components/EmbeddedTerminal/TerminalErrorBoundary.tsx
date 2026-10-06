import type { ErrorInfo, ReactNode } from 'react';
import { Box, Button, Typography } from '@mui/material';
import { Component } from 'react';

interface TerminalErrorBoundaryProps {
  children: ReactNode;
  /** 错误态标题，按被保护子树区分：终端子树缺省「终端异常退出」，ACP 视图传「会话视图异常退出」。 */
  title?: string;
}

interface TerminalErrorBoundaryState {
  error: Error | null;
}

// 启动区错误边界：xterm/PTY 链路的未捕获错误（如 write 非法输入）不再卸载整页
// （React 19 未捕获错误默认白屏），降级为区域内的错误态 + 重试。重试 = 清空 error
// 重新渲染 children（子树全新挂载），TerminalView/usePtySession/AcpSessionView 全新
// 初始化。除终端子树外，ACP 会话视图在 LaunchModePicker acp 分支嵌套本组件并传
// title 区分文案——ACP 渲染异常在内层收敛，不冒泡到外层终端文案边界。
export default class TerminalErrorBoundary extends Component<TerminalErrorBoundaryProps, TerminalErrorBoundaryState> {
  state: TerminalErrorBoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): TerminalErrorBoundaryState {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // 完整栈进 console 供排障（白屏时期用户只能看到截断的报错）
    console.error('[TerminalErrorBoundary] terminal crashed:', error, info.componentStack);
  }

  render() {
    if (this.state.error != null) {
      return (
        <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: 1.5, p: 2 }}>
          <Typography variant="body2" color="text.secondary">{this.props.title ?? '终端异常退出'}</Typography>
          <Typography variant="caption" color="text.disabled" sx={{ maxWidth: 480, wordBreak: 'break-all' }}>
            {this.state.error.message}
          </Typography>
          <Button size="small" onClick={() => this.setState({ error: null })}>重试</Button>
        </Box>
      );
    }
    return this.props.children;
  }
}
