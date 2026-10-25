import type { IssueWorkspaceState, IssueWorkspaceStatus, IssueWorkspaceStep } from '@src/services';
import type { ReactNode } from 'react';
import {
  Cancel as CancelIcon,
  CheckCircle as CheckCircleIcon,
  RemoveCircleOutlined as RemoveCircleOutlinedIcon,
} from '@mui/icons-material';
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  Typography,
} from '@mui/material';
import { ISSUE_WORKSPACE_STEP_KEY } from '@src/services';
import { useInitTaskWorkspace, useTaskWorkspaceStatus } from '@src/state/taskWorkspace';
import { useEffect, useRef, useState } from 'react';

// 可重试终态的提示文案（FAILED 的原因在 state.error，另行展示）。
const RETRY_HINTS: Record<string, string> = {
  INTERRUPTED: '上次初始化被中断（进程退出遗留），可重新初始化',
  CORRUPTED: '状态文件损坏，重新初始化将覆盖修复',
};

/** 步骤状态辅助文案（跟在标题后的次级说明，与圆标语义一一对应）。 */
const STATUS_TEXT: Partial<Record<IssueWorkspaceStatus, string>> = {
  PENDING: '待执行',
  RUNNING: '进行中',
  SUCCESS: '已完成',
  FAILED: '失败',
  SKIPPED: '已跳过',
};

/**
 * 步骤状态圆标：PENDING 编号圈 / RUNNING 转圈 / SUCCESS 绿勾 / FAILED 红叉 / SKIPPED 灰杠。
 * 编号 = 步骤条位次（1 起），仅待执行态展示数字，其余态以状态图标替代。
 */
function StepStatusIcon({ status, index }: { status: IssueWorkspaceStatus; index: number }) {
  switch (status) {
    case 'RUNNING':
      return <CircularProgress size={16} />;
    case 'SUCCESS':
      return <CheckCircleIcon sx={{ fontSize: '18px', color: 'success.main' }} />;
    case 'FAILED':
      return <CancelIcon sx={{ fontSize: '18px', color: 'error.main' }} />;
    case 'SKIPPED':
      return <RemoveCircleOutlinedIcon sx={{ fontSize: '18px', color: 'text.disabled' }} />;
    default:
      return (
        <Box
          sx={{
            width: '18px',
            height: '18px',
            borderRadius: '50%',
            border: '1px solid',
            borderColor: 'divider',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
          }}
        >
          <Typography sx={{ fontSize: '11px', lineHeight: 1, color: 'text.secondary' }}>{index}</Typography>
        </Box>
      );
  }
}

/** 步骤条行模型：key 去重、status 驱动圆标、note 为说明行、repos 为 Clone 步骤的仓库级子进度。 */
interface StepRowModel {
  key: string;
  title: string;
  status: IssueWorkspaceStatus;
  note?: string | null;
  repos?: IssueWorkspaceStep['repos'];
}

/** 仓库级进度小圆点：PENDING 空心 / RUNNING 转圈 / SUCCESS 绿 / FAILED 红。 */
function RepoStatusDot({ status }: { status: IssueWorkspaceStatus }) {
  switch (status) {
    case 'RUNNING':
      return <CircularProgress size={10} />;
    case 'SUCCESS':
      return <Box sx={{ width: '8px', height: '8px', borderRadius: '50%', bgcolor: 'success.main' }} />;
    case 'FAILED':
      return <Box sx={{ width: '8px', height: '8px', borderRadius: '50%', bgcolor: 'error.main' }} />;
    default:
      return <Box sx={{ width: '8px', height: '8px', borderRadius: '50%', border: '1px solid', borderColor: 'divider' }} />;
  }
}

/**
 * 三段式初始化纵向步骤条：编号状态圆标 + 连接线自上而下（① createDirs / ② 工程化步骤，
 * cloneRepos 内嵌仓库级进度 / ③ 终端，仅过渡浮层传入）。state 为 null 时仅渲染骨架。
 * SKIPPED 行整行置灰；连接线由左列 flex 撑满行高自然衔接。
 */
function InitStepsCard({
  state,
  terminalStepStatus,
  terminalStepTitle: step3Title,
}: {
  state: IssueWorkspaceState | null;
  terminalStepStatus: IssueWorkspaceStatus | null;
  terminalStepTitle: string;
}) {
  const steps = state?.steps ?? [];
  const dirStep = steps.find(s => s.key === ISSUE_WORKSPACE_STEP_KEY.CREATE_DIRS);
  const engSteps = steps.filter(s => s.key !== ISSUE_WORKSPACE_STEP_KEY.CREATE_DIRS);
  const rows: StepRowModel[] = [
    ...(dirStep ? [{ key: dirStep.key, title: '初始化任务目录', status: dirStep.status, note: dirStep.message }] : []),
    ...engSteps.map(step => ({
      key: step.key,
      title: step.title,
      status: step.status,
      note: step.message,
      repos: step.key === ISSUE_WORKSPACE_STEP_KEY.CLONE_REPOS ? step.repos : undefined,
    })),
    ...(terminalStepStatus != null ? [{ key: 'terminal', title: step3Title, status: terminalStepStatus }] : []),
  ];
  return (
    <Box>
      {rows.map((row, i) => (
        <Box key={row.key} sx={{ display: 'flex', gap: '12px' }}>
          <Box sx={{ display: 'flex', flexDirection: 'column', alignItems: 'center', flexShrink: 0 }}>
            <StepStatusIcon status={row.status} index={i + 1} />
            {i < rows.length - 1 && <Box sx={{ width: '1px', flex: 1, bgcolor: 'divider', my: '4px' }} />}
          </Box>
          <Box sx={{ flex: 1, minWidth: '0px', pt: '2px', pb: i < rows.length - 1 ? '20px' : '0px' }}>
            <Typography variant="body2" color={row.status === 'SKIPPED' ? 'text.disabled' : undefined}>{row.title}</Typography>
            <Typography
              variant="caption"
              color="text.disabled"
              sx={{ display: 'block', wordBreak: 'break-all', mt: '2px' }}
            >
              {row.note != null && row.note !== '' ? `${STATUS_TEXT[row.status]} · ${row.note}` : STATUS_TEXT[row.status]}
            </Typography>
            {row.note != null && row.note !== '' && (
              <Typography
                variant="caption"
                color={row.status === 'SKIPPED' ? 'text.disabled' : 'text.secondary'}
                sx={{ display: 'block', wordBreak: 'break-all', mt: '2px' }}
              >
                {row.note}
              </Typography>
            )}
            {(row.repos ?? []).map(repo => (
              <Box key={repo.localRepositoryId} sx={{ display: 'flex', alignItems: 'flex-start', gap: '6px', mt: '6px' }}>
                <Box sx={{ width: '10px', pt: '4px', flexShrink: 0, display: 'flex', justifyContent: 'center' }}>
                  <RepoStatusDot status={repo.status} />
                </Box>
                <Box sx={{ minWidth: '0px' }}>
                  <Typography variant="caption">{repo.name}</Typography>
                  <Typography variant="caption" color="text.disabled" sx={{ display: 'block' }}>
                    {repo.message != null && repo.message !== '' ? `${STATUS_TEXT[repo.status]} · ${repo.message}` : STATUS_TEXT[repo.status]}
                  </Typography>
                </Box>
              </Box>
            ))}
          </Box>
        </Box>
      ))}
    </Box>
  );
}

interface WorkspaceInitGateProps {
  issueId: string;
  // 终端直启值（DevWorkbenchPage 从 workspace 启动设置解析下传；null = 解析中或不
  // 自动启动），仅驱动步骤③文案（非 null → 「启动 Claude 终端」），无时序影响。
  startupCli: string | null;
  // 过渡浮层开关（SUCCESS 瞬间步骤清单浮层 1.2s）：仅 children 为终端时播放——none 档
  // children 是「选择启动方式」面板，浮层叠上成重影且步骤③语义不成立，故父层关掉。
  terminalTransitionEnabled: boolean;
  /** 工作空间就绪后渲染的内容（终端 split 树）。 */
  children: ReactNode;
}

/**
 * WorkspaceInitGate：工作空间初始化闸门（T1.5）——选中 issue 后先走三段式初始化引导，
 * SUCCESS 才渲染终端。占位整个右侧内容区（期间仅顶部 issue 标题栏与本面板）：
 *  ① 初始化任务目录（后端 createDirs，无则创建有则秒过）
 *  ② 工程化步骤（sshConfig / cloneRepos，cloneRepos 展开仓库级进度）
 *  ③ 启动 Claude 任务 / 启动任务（文案按 startupCodeCli 配置区分）——仅过渡浮层渲染此步，
 *  静止视图恒为 PENDING 无信息量不渲染
 * 右上角「清理终端并重新初始化」按钮（DevWorkbenchPage）与面板按钮共用 useInitTaskWorkspace——
 * 同一 query key 订阅，任一触发面板自动切换状态。
 */
export default function WorkspaceInitGate({ issueId, startupCli, terminalTransitionEnabled, children }: WorkspaceInitGateProps) {
  const { data: statusResp, isLoading, error, refetch } = useTaskWorkspaceStatus(issueId);
  const initWorkspace = useInitTaskWorkspace();
  const serverStatus = statusResp?.serverStatus;
  const state = statusResp?.state;

  // ③ 项过渡：初始化刚跑完（本组件存活期内见证 非 SUCCESS → SUCCESS）时，步骤面板短暂
  // 浮层（步骤③转圈）再隐去，让「启动任务」可感知。挂载即 SUCCESS（切回已初始化 issue
  // 缓存命中/页面恢复）不算——直接终端零浮层。置位用渲染期重置（上轮不一致才 set，同
  // usePtySession lastAttachKeyRef 范式，规避 effect 内同步 setState）；「到时熄灭」走异步定时器。
  const [terminalTransition, setTerminalTransition] = useState(false);
  const prevStatusRef = useRef<IssueWorkspaceStatus | undefined>(undefined);
  if (serverStatus !== prevStatusRef.current) {
    const prev = prevStatusRef.current;
    prevStatusRef.current = serverStatus;
    if (serverStatus === 'SUCCESS' && prev !== undefined) {
      // 仅「已观察过非 SUCCESS 后到达 SUCCESS」且 children 为终端时播放（开关见 props 注释）。
      if (terminalTransitionEnabled) {
        setTerminalTransition(true);
      }
    } else if (serverStatus !== 'SUCCESS') {
      setTerminalTransition(false);
    }
  }

  useEffect(() => {
    if (!terminalTransition) {
      return;
    }
    const timer = setTimeout(setTerminalTransition, 1200, false);
    return () => clearTimeout(timer);
  }, [terminalTransition]);

  // 就绪：渲染终端；过渡期浮层展示步骤清单（pointerEvents none，终端即刻可交互）。
  if (serverStatus === 'SUCCESS') {
    return (
      <Box sx={{ position: 'relative', height: '100%' }}>
        {children}
        {terminalTransition && (
          <Box
            sx={{
              position: 'absolute',
              inset: '0px',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              pointerEvents: 'none',
            }}
          >
            <Box sx={{ width: '100%', maxWidth: '480px', p: '16px' }}>
              <InitStepsCard state={state ?? null} terminalStepStatus="RUNNING" terminalStepTitle={terminalStepTitle(startupCli)} />
            </Box>
          </Box>
        )}
      </Box>
    );
  }

  // 首次状态查询中。
  if (isLoading) {
    return (
      <Box sx={{ height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <CircularProgress />
      </Box>
    );
  }

  // 查询失败（go-server 未运行等）：展示原因并允许重试查询。
  if (error) {
    return (
      <PanelShell>
        <Alert
          severity="error"
          action={<Button color="inherit" size="small" onClick={() => void refetch()}>重试</Button>}
        >
          工作空间状态查询失败：{error.message}
        </Alert>
      </PanelShell>
    );
  }

  const terminalTitle = terminalStepTitle(startupCli);
  const failedFamily = serverStatus === 'FAILED' || serverStatus === 'INTERRUPTED' || serverStatus === 'CORRUPTED';

  return (
    <PanelShell>
      {failedFamily && (
        <Alert severity="error">
          {serverStatus === 'FAILED'
            ? (state?.error || '初始化失败')
            : RETRY_HINTS[serverStatus ?? ''] ?? '初始化未完成'}
        </Alert>
      )}

      {serverStatus === 'NOT_INITIALIZED'
        ? (
            <Typography variant="body2" color="text.secondary">
              尚未初始化任务。初始化将创建任务目录、生成 SSH 配置并克隆关联仓库到 agent 分支。
            </Typography>
          )
        : (
            <InitStepsCard
              state={state ?? null}
              terminalStepStatus={null}
              terminalStepTitle={terminalTitle}
            />
          )}

      {serverStatus !== 'RUNNING' && serverStatus !== 'PENDING' && (
        <Button
          variant="contained"
          disabled={initWorkspace.isPending}
          onClick={() => initWorkspace.mutate({ taskId: issueId })}
          sx={{ alignSelf: 'flex-start' }}
        >
          {initWorkspace.isPending ? '初始化中…' : failedFamily ? '重新初始化' : '初始化任务'}
        </Button>
      )}
    </PanelShell>
  );
}

/** 面板外壳：内容区居中 + 限宽列。 */
function PanelShell({ children }: { children: ReactNode }) {
  return (
    <Box sx={{ height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center', p: '16px' }}>
      <Box sx={{ width: '100%', maxWidth: '480px', display: 'flex', flexDirection: 'column', gap: '16px' }}>
        <Typography variant="h6">初始化任务</Typography>
        {children}
      </Box>
    </Box>
  );
}

function terminalStepTitle(startupCli: string | null): string {
  return startupCli != null ? '启动 Claude 任务' : '启动任务';
}
