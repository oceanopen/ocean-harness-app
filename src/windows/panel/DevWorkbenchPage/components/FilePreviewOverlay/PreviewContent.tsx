import type { IssueWorkspaceFileContentResponseData } from '@src/services';
import type { ReactNode } from 'react';
import {
  BrokenImageOutlined as BrokenImageOutlinedIcon,
  SubjectRounded as SubjectRoundedIcon,
} from '@mui/icons-material';
import { Alert, Box, Button, CircularProgress, Typography } from '@mui/material';
import { useWorkspaceFileContent } from '@src/state/workspaceFiles';
import { useEffect, useRef } from 'react';
import CodeViewer from '../fileViewer/CodeViewer';
import ImageViewer from '../fileViewer/ImageViewer';
import MarkdownViewer from '../fileViewer/MarkdownViewer';
import { resolveTextViewer } from '../fileViewer/viewerKind';

interface PreviewContentProps {
  issueId: string;
  baseDir: string;
  /// 激活 tab 的文件相对路径（父层按 path 作 key，切 tab 即重挂载——query 缓存命中 +
  /// staleTime 0 静默重验，见 state/workspaceFiles/queries.ts）。
  path: string;
}

/// 预览内容区：content query 四分支（错误/加载/信息态/渲染态）+ 双层分派——传输 kind
/// （后端定夺：text/image/binary/tooLarge）× text 内呈现细分（markdown/code，viewerKind
/// 纯函数）。容器 tabIndex=-1 于挂载时夺焦（焦点若留在背后 xterm，键盘输入会打进不可见
/// 终端）；Escape 冒泡至浮层根处理。
export default function PreviewContent({ issueId, baseDir, path }: PreviewContentProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const { data, dataUpdatedAt, error, isLoading, refetch } = useWorkspaceFileContent(issueId, baseDir, path);

  // 夺焦：mount 时一次（组件按 path 作 key，切 tab 重挂载即重新夺焦）。
  useEffect(() => {
    containerRef.current?.focus();
  }, []);

  return (
    <Box
      ref={containerRef}
      tabIndex={-1}
      sx={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', outline: 'none' }}
    >
      {error
        ? (
            <Box sx={{ p: 1.5 }}>
              <Alert
                severity="error"
                action={<Button color="inherit" size="small" onClick={() => void refetch()}>重试</Button>}
              >
                文件读取失败：{error.message}
              </Alert>
            </Box>
          )
        : isLoading || data == null
          ? (
              <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                <CircularProgress size={20} />
              </Box>
            )
          : renderContent(data, { issueId, baseDir, path, dataUpdatedAt })}
    </Box>
  );
}

interface RenderContext {
  issueId: string;
  baseDir: string;
  path: string;
  /// 内容 query 的最后更新时间戳（图片缓存刷新令牌）。
  dataUpdatedAt: number;
}

/// 渲染态分派：kind × 呈现细分。binary/tooLarge 为合法信息态（非错误）。
function renderContent(data: IssueWorkspaceFileContentResponseData, ctx: RenderContext) {
  switch (data.kind) {
    case 'image':
      return <ImageViewer issueId={ctx.issueId} baseDir={ctx.baseDir} path={ctx.path} version={ctx.dataUpdatedAt} />;
    case 'text':
      return resolveTextViewer(ctx.path) === 'markdown'
        ? <MarkdownViewer content={data.content ?? ''} issueId={ctx.issueId} baseDir={ctx.baseDir} path={ctx.path} />
        : <CodeViewer content={data.content ?? ''} path={ctx.path} />;
    case 'binary':
      return <InfoPanel icon={<BrokenImageOutlinedIcon sx={{ fontSize: 48, color: 'text.secondary' }} />} title="二进制文件，暂不支持预览" size={data.size} />;
    case 'tooLarge':
      return <InfoPanel icon={<SubjectRoundedIcon sx={{ fontSize: 48, color: 'text.secondary' }} />} title="文件过大，暂不支持预览" size={data.size} />;
    default:
      return null;
  }
}

/// binary/tooLarge 信息面板（图标 + 说明 + 人类可读大小）。
function InfoPanel({ icon, title, size }: { icon: ReactNode; title: string; size: number }) {
  return (
    <Box
      sx={{
        flex: 1,
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 1,
        p: 2,
      }}
    >
      {icon}
      <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>{title}</Typography>
      <Typography variant="body2" color="text.secondary">大小：{formatBytes(size)}</Typography>
    </Box>
  );
}

/// 字节数 → 人类可读（B/KB/MB，一位小数；预览提示文案用）。
function formatBytes(size: number): string {
  if (size < 1024) {
    return `${size} B`;
  }
  if (size < 1024 * 1024) {
    return `${(size / 1024).toFixed(1)} KB`;
  }
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}
