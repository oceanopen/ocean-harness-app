import type { IssueWorkspaceFileContentResponseData, IssueWorkspaceFileDiffResponseData } from '@src/services';
import type { ReactNode } from 'react';
import {
  BrokenImageOutlined as BrokenImageOutlinedIcon,
  SubjectRounded as SubjectRoundedIcon,
} from '@mui/icons-material';
import { Alert, Box, Button, CircularProgress, Typography } from '@mui/material';
import { useWorkspaceFileContent, useWorkspaceFileDiff } from '@src/state/workspaceFiles';
import { useEffect, useRef } from 'react';
import CodeViewer from '../fileViewer/CodeViewer';
import DiffViewer from '../fileViewer/DiffViewer';
import ImageViewer from '../fileViewer/ImageViewer';
import MarkdownViewer from '../fileViewer/MarkdownViewer';
import { resolveTextViewer } from '../fileViewer/viewerKind';
import ViewerToolbar from '../fileViewer/ViewerToolbar';

interface PreviewContentProps {
  issueId: string;
  baseDir: string;
  /// 激活 tab 的文件相对路径（父层按 tab id 作 key，切 tab 即重挂载——query 缓存命中 +
  /// staleTime 0 静默重验，见 state/workspaceFiles/queries.ts）。
  path: string;
  /// Git 变更模式下的变更文件路径集（父层 FilePreviewOverlay 派生，与 tab 栏小点/刷新分流
  /// 同源；空集 = 全部文件模式或变更集未就绪）。
  changedPaths: ReadonlySet<string>;
}

/// 预览内容区：tab 内容形态（文件内容 / 未提交变更 diff）为渲染期派生——激活文件在变更集
/// （父层按「面板模式 + getGitChanges 变更集」派生的 changedPaths）时走 fileDiff query，
/// 否则走 content query。变更集加载中 changedPaths 为空集，先保持文件内容渲染，到位后原位
/// 切 diff（组件按 tab id 作 key，模式切换不重挂载）。file 链路双层分派——传输 kind（后端
/// 定夺：text/image/binary/tooLarge）× text 内呈现细分（markdown/code，viewerKind 纯函数）；
/// diff 走 DiffViewer（text）或信息态（binary/tooLarge）。容器 tabIndex=-1 于挂载时夺焦
/// （焦点若留在背后 xterm，键盘输入会打进不可见终端）；Escape 冒泡至浮层根处理。
export default function PreviewContent({ issueId, baseDir, path, changedPaths }: PreviewContentProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const hasChange = changedPaths.has(path);
  // 两条 query 按派生决策互斥启用（path 传 null 关闸），hook 顺序恒定不条件化。
  const contentQuery = useWorkspaceFileContent(issueId, baseDir, hasChange ? null : path);
  const diffQuery = useWorkspaceFileDiff(issueId, baseDir, hasChange ? path : null);
  const active = hasChange ? diffQuery : contentQuery;
  const { data, dataUpdatedAt, error, isLoading, refetch } = active;
  // 挂载→内容就绪耗时观测（含传输与渲染提交；DEV 计时日志，每挂载记首份数据一次）。
  // t0 在 mount effect 里取（纯净性：渲染期不调 performance.now），切 tab 重挂载即重置。
  const mountedAtRef = useRef<number | null>(null);
  const readyLoggedRef = useRef(false);

  // 夺焦：mount 时一次（组件按 path 作 key，切 tab 重挂载即重新夺焦）。
  useEffect(() => {
    mountedAtRef.current = performance.now();
    containerRef.current?.focus();
  }, []);

  useEffect(() => {
    const t0 = mountedAtRef.current;
    if (data != null && !readyLoggedRef.current && t0 != null) {
      readyLoggedRef.current = true;
      if (import.meta.env.DEV) {
        console.info(`[FilePreviewOverlay] 预览就绪 ${path} ${Math.round(performance.now() - t0)}ms（挂载→内容提交）`);
      }
    }
  }, [data, path]);

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
                {hasChange ? '变更内容读取失败' : '文件读取失败'}：{error.message}
              </Alert>
            </Box>
          )
        : isLoading || data == null
          ? (
              <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                <CircularProgress size={20} />
              </Box>
            )
          : hasChange
            ? renderDiffContent(data as IssueWorkspaceFileDiffResponseData, path)
            : renderContent(data as IssueWorkspaceFileContentResponseData, { issueId, baseDir, path, dataUpdatedAt })}
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

/// diff 渲染态分派：text → DiffViewer；binary/tooLarge 为合法信息态（非错误，无 size 信息）。
function renderDiffContent(data: IssueWorkspaceFileDiffResponseData, path: string) {
  switch (data.kind) {
    case 'text':
      return <DiffViewer path={path} data={data} />;
    case 'binary':
      return <InfoPanel icon={<BrokenImageOutlinedIcon sx={{ fontSize: 48, color: 'text.secondary' }} />} title="二进制文件，暂不支持 diff 预览" />;
    case 'tooLarge':
      return <InfoPanel icon={<SubjectRoundedIcon sx={{ fontSize: 48, color: 'text.secondary' }} />} title="文件过大，暂不支持 diff 预览" />;
    default:
      return null;
  }
}

/// 渲染态分派：kind × 呈现细分。binary/tooLarge 为合法信息态（非错误）。
function renderContent(data: IssueWorkspaceFileContentResponseData, ctx: RenderContext) {
  switch (data.kind) {
    case 'image':
      return <ImageViewer issueId={ctx.issueId} baseDir={ctx.baseDir} path={ctx.path} version={ctx.dataUpdatedAt} />;
    case 'text':
      return resolveTextViewer(ctx.path) === 'markdown'
        ? <MarkdownViewer content={data.content ?? ''} issueId={ctx.issueId} baseDir={ctx.baseDir} path={ctx.path} />
        : (
            <>
              {/* 操作栏（语言名·行数 + 复制）在 PreviewContent 层组合而非塞进 CodeViewer
                  本体——后者被 md 源码视图/代码块全屏 Dialog 复用，内置栏会在复用点重复。 */}
              <ViewerToolbar path={ctx.path} content={data.content ?? ''} />
              <CodeViewer content={data.content ?? ''} path={ctx.path} />
            </>
          );
    case 'binary':
      return <InfoPanel icon={<BrokenImageOutlinedIcon sx={{ fontSize: 48, color: 'text.secondary' }} />} title="二进制文件，暂不支持预览" size={data.size} />;
    case 'tooLarge':
      return <InfoPanel icon={<SubjectRoundedIcon sx={{ fontSize: 48, color: 'text.secondary' }} />} title="文件过大，暂不支持预览" size={data.size} />;
    default:
      return null;
  }
}

/// binary/tooLarge 信息面板（图标 + 说明 + 可选人类可读大小）。
function InfoPanel({ icon, title, size }: { icon: ReactNode; title: string; size?: number }) {
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
      {size != null && <Typography variant="body2" color="text.secondary">大小：{formatBytes(size)}</Typography>}
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
