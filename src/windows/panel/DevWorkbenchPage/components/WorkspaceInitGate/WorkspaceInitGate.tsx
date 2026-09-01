import type { IssueWorkspaceState, IssueWorkspaceStatus, IssueWorkspaceStep } from '@src/services';
import type { TerminalStartupCodeCli } from '@src/shared/appConfig';
import type { ReactNode } from 'react';
import {
  Cancel as CancelIcon,
  CheckCircle as CheckCircleIcon,
  RadioButtonUnchecked as RadioButtonUncheckedIcon,
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
import {
  DEFAULT_TERMINAL_STARTUP_CODE_CLI,
  parseTerminalStartupCodeCli,
  TERMINAL_STARTUP_CODE_CLI_KEY,
} from '@src/shared/appConfig';
import { commands } from '@src/shared/bindings';
import { useConfigValue } from '@src/shared/useConfigValue';
import { useInitIssueWorkspace, useIssueWorkspaceStatus } from '@src/state/issueWorkspace';
import { useEffect, useRef, useState } from 'react';

// 启动 CLI decode：parse 内含回落（非法/缺失 → none）。模块级保证引用稳定（useConfigValue 要求）。
function decodeStartupCodeCli(raw: string | null): TerminalStartupCodeCli {
  return parseTerminalStartupCodeCli(raw);
}

// 可重试终态的提示文案（FAILED 的原因在 state.error，另行展示）。
const RETRY_HINTS: Record<string, string> = {
  INTERRUPTED: '上次初始化被中断（进程退出遗留），可重新初始化',
  CORRUPTED: '状态文件损坏，重新初始化将覆盖修复',
};

/** 步骤/仓库状态图标：PENDING 灰圈 / RUNNING 转圈 / SUCCESS 绿勾 / FAILED 红叉 / SKIPPED 灰杠。 */
function StepStatusIcon({ status }: { status: IssueWorkspaceStatus }) {
  switch (status) {
    case 'RUNNING':
      return <CircularProgress size={16} />;
    case 'SUCCESS':
      return <CheckCircleIcon sx={{ fontSize: 18, color: 'success.main' }} />;
    case 'FAILED':
      return <CancelIcon sx={{ fontSize: 18, color: 'error.main' }} />;
    case 'SKIPPED':
      return <RemoveCircleOutlinedIcon sx={{ fontSize: 18, color: 'text.disabled' }} />;
    default:
      return <RadioButtonUncheckedIcon sx={{ fontSize: 18, color: 'text.disabled' }} />;
  }
}

/** 单个步骤行：状态图标 + 标题（SKIPPED 追加「暂未接入」）+ 次行说明（失败原因/降级原因）。 */
function StepRow({
  status,
  title,
  note,
  indented,
}: { status: IssueWorkspaceStatus; title: string; note?: string | null; indented?: boolean }) {
  return (
    <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1, pl: indented ? 3.5 : 0 }}>
      <Box sx={{ width: 18, height: 18, flexShrink: 0, mt: '2px', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <StepStatusIcon status={status} />
      </Box>
      <Box sx={{ minWidth: 0 }}>
        <Typography variant="body2">{title}{status === 'SKIPPED' ? '（暂未接入）' : ''}</Typography>
        {note
          ? <Typography variant="caption" color="text.secondary" sx={{ display: 'block', wordBreak: 'break-all' }}>{note}</Typography>
          : null}
      </Box>
    </Box>
  );
}

interface WorkspaceInitGateProps {
  issueId: string;
  /** 工作空间根目录（appConfig workspace_base_dir）；空串 = 未设置，面板引导去设置页。 */
  baseDir: string;
  /** 工作空间就绪后渲染的内容（终端 split 树）。 */
  children: ReactNode;
}

/**
 * WorkspaceInitGate：工作空间初始化闸门（T1.5）——选中 issue 后先走三段式初始化引导，
 * SUCCESS 才渲染终端。占位整个右侧内容区（期间仅顶部 issue 标题栏与本面板）：
 *  ① 初始化工作空间目录（后端 createDirs，无则创建有则秒过）
 *  ② 工作空间初始化（sshConfig / mcpConfig / cloneRepos 工程化步骤，cloneRepos 展开仓库级进度）
 *  ③ 启动 Claude 终端 / 启动终端（文案按 startupCodeCli 配置区分）
 * 右上角「初始化」按钮（DevWorkbenchPage）与面板按钮共用 useInitIssueWorkspace——同一 query key
 * 订阅，任一触发面板自动切换状态。
 */
export default function WorkspaceInitGate({ issueId, baseDir, children }: WorkspaceInitGateProps) {
  const { data: statusResp, isLoading, error, refetch } = useIssueWorkspaceStatus(issueId, baseDir);
  const initWorkspace = useInitIssueWorkspace();
  const startupCli = useConfigValue(TERMINAL_STARTUP_CODE_CLI_KEY, decodeStartupCodeCli, DEFAULT_TERMINAL_STARTUP_CODE_CLI);
  const serverStatus = statusResp?.serverStatus;
  const state = statusResp?.state;

  // ③ 项过渡：初始化刚跑完（本组件存活期内见证 非 SUCCESS → SUCCESS）时，步骤面板短暂
  // 浮层（步骤③转圈）再隐去，让「启动终端」可感知。挂载即 SUCCESS（切回已初始化 issue
  // 缓存命中/页面恢复）不算——直接终端零浮层。置位用渲染期重置（上轮不一致才 set，同
  // usePtySession lastAttachKeyRef 范式，规避 effect 内同步 setState）；「到时熄灭」走异步定时器。
  const [terminalTransition, setTerminalTransition] = useState(false);
  const prevStatusRef = useRef<IssueWorkspaceStatus | undefined>(undefined);
  if (serverStatus !== prevStatusRef.current) {
    const prev = prevStatusRef.current;
    prevStatusRef.current = serverStatus;
    if (serverStatus === 'SUCCESS' && prev !== undefined) {
      setTerminalTransition(true); // 仅「已观察过非 SUCCESS 后到达 SUCCESS」播放
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

  // baseDir 未设置：引导去设置页（同 EmbeddedTerminal 先例，不算步骤项）。
  if (!baseDir) {
    return (
      <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: 2, p: 2 }}>
        <Typography variant="body2" color="text.secondary">工作空间根目录未设置，无法初始化</Typography>
        <Button variant="outlined" size="small" onClick={() => void commands.showSettingsWindow('projectConfig')}>
          前往设置
        </Button>
      </Box>
    );
  }

  // 就绪：渲染终端；过渡期浮层展示步骤清单（pointerEvents none，终端即刻可交互）。
  if (serverStatus === 'SUCCESS') {
    return (
      <Box sx={{ position: 'relative', height: '100%' }}>
        {children}
        {terminalTransition && (
          <Box
            sx={{
              position: 'absolute',
              inset: 0,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              pointerEvents: 'none',
            }}
          >
            <Box sx={{ width: '100%', maxWidth: 480, p: 2 }}>
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
              尚未初始化工作空间。初始化将创建工作目录、生成 SSH 配置并克隆关联仓库到 agent 分支。
            </Typography>
          )
        : (
            <InitStepsCard
              state={state ?? null}
              terminalStepStatus="PENDING"
              terminalStepTitle={terminalTitle}
            />
          )}

      {serverStatus !== 'RUNNING' && serverStatus !== 'PENDING' && (
        <Button
          variant="contained"
          disabled={initWorkspace.isPending}
          onClick={() => initWorkspace.mutate({ issueId, baseDir })}
          sx={{ alignSelf: 'flex-start' }}
        >
          {initWorkspace.isPending ? '初始化中…' : failedFamily ? '重新初始化' : '初始化工作空间'}
        </Button>
      )}
    </PanelShell>
  );
}

/** 面板外壳：内容区居中 + 限宽列。 */
function PanelShell({ children }: { children: ReactNode }) {
  return (
    <Box sx={{ height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center', p: 2 }}>
      <Box sx={{ width: '100%', maxWidth: 480, display: 'flex', flexDirection: 'column', gap: 2 }}>
        <Typography variant="h6">工作空间初始化</Typography>
        {children}
      </Box>
    </Box>
  );
}

function terminalStepTitle(startupCli: TerminalStartupCodeCli): string {
  return startupCli !== 'none' ? '启动 Claude 终端' : '启动终端';
}

/**
 * 三段式步骤清单卡片：① createDirs / ② 其余工程化步骤（cloneRepos 展开仓库级） / ③ 终端。
 * state 为 null（查询成功但无状态文件）时仅渲染说明性骨架。
 */
function InitStepsCard({
  state,
  terminalStepStatus,
  terminalStepTitle: step3Title,
}: {
  state: IssueWorkspaceState | null;
  terminalStepStatus: IssueWorkspaceStatus;
  terminalStepTitle: string;
}) {
  const steps = state?.steps ?? [];
  const dirStep = steps.find(s => s.key === ISSUE_WORKSPACE_STEP_KEY.CREATE_DIRS);
  const engSteps = steps.filter(s => s.key !== ISSUE_WORKSPACE_STEP_KEY.CREATE_DIRS);
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5 }}>
      {dirStep && <StepRow status={dirStep.status} title="初始化工作空间目录" note={dirStep.message} />}
      {engSteps.length > 0 && (
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
          <Typography variant="caption" color="text.secondary" sx={{ pl: 3.5 }}>工作空间初始化</Typography>
          {engSteps.map(step => (
            <EngStepRow key={step.key} step={step} />
          ))}
        </Box>
      )}
      <StepRow status={terminalStepStatus} title={step3Title} />
    </Box>
  );
}

/** 工程化步骤行：全局步骤 + cloneRepos 的仓库级子列表（缩进，含失败原因）。 */
function EngStepRow({ step }: { step: IssueWorkspaceStep }) {
  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
      <StepRow status={step.status} title={step.title} note={step.message} />
      {step.key === ISSUE_WORKSPACE_STEP_KEY.CLONE_REPOS
        && (step.repos ?? []).map(repo => (
          <StepRow key={repo.localRepositoryId} status={repo.status} title={repo.name} note={repo.message} indented />
        ))}
    </Box>
  );
}
