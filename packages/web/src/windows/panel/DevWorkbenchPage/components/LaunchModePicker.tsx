import type { DoctorEntryState } from '@src/services';
import type { ReactNode } from 'react';
import { Box, Button, Typography } from '@mui/material';
import { AGENT_CODE } from '@src/shared/agentCode';
import { useDoctorCheck, useDoctorEntry } from '@src/state/doctor';
import { useState } from 'react';
import TerminalPaneRoot from './TerminalPanes/TerminalPaneRoot';

/** 临场启动方式（none 档 issue 就绪后的三选一；仅本次有效，不回写 workspace 配置）。 */
type PickedLaunch = 'terminal-auto' | 'terminal-manual';

interface LaunchModePickerProps {
  /** 终端启动 Agent 展示名（来自 workspace autoCommand 解析，一期 Claude Code）。 */
  terminalAgentLabel: string;
  /** ACP 会话 Agent 展示名（来自 workspace agentCode，一期 Claude Code）。 */
  acpAgentLabel: string;
  onPick: (pick: PickedLaunch) => void;
}

/**
 * ACP 选项的置灰提示（T1.4 doctor 四态派生）：检测中/未探测 → 检测进行时；healthy →
 * T1.6 上线预告；unhealthy → 后端生成的用户可读原因直显（node 过低/未登录等）。
 */
function acpDoctorHint(entry: DoctorEntryState | undefined): string {
  switch (entry?.status) {
    case 'healthy':
      return 'ACP 会话视图即将上线';
    case 'unhealthy':
      return entry.reason ?? 'ACP 运行环境不可用';
    default: // checking / unknown / 快照未就绪：一律按检测进行时呈现（轮询驱动收敛）
      return '正在检测 ACP 运行环境…';
  }
}

/**
 * LaunchModePicker：启动方式临场选择面板（workspace 启动模式 = none 时，issue 初始化
 * 就绪后替代终端树渲染）。三选一：终端自动（直启所选 Agent）/ 终端手动（裸 shell +
 * 工具栏按钮拉起）/ ACP 会话（T1.6 前置灰，置灰提示随 doctor 四态派生并附重新检测）。
 * 选择仅本次有效——不回写配置、不记忆，每次打开重新决定；想固定方式应修改 workspace
 * 启动设置。
 */
export default function LaunchModePicker({ terminalAgentLabel, acpAgentLabel, onPick }: LaunchModePickerProps) {
  const doctorEntry = useDoctorEntry(AGENT_CODE.claudeAcp);
  const checkDoctor = useDoctorCheck();
  return (
    <PanelShell>
      <Typography variant="h6">选择启动方式</Typography>
      <Typography variant="body2" color="text.secondary">
        工作空间已就绪。本工作空间的启动设置为「不执行任何操作」，请为本次会话选择启动方式（不记忆，下次打开重新选择）。
      </Typography>
      <LaunchOption
        title={`终端 - 自动启动 ${terminalAgentLabel}`}
        description="打开主终端并直接启动，无 shell 中转"
        onClick={() => onPick('terminal-auto')}
      />
      <LaunchOption
        title="终端 - 手动启动"
        description="打开裸 shell 终端，经终端工具条自行选择并启动 Agent"
        onClick={() => onPick('terminal-manual')}
      />
      <LaunchOption
        title={`ACP 方式启动 ${acpAgentLabel}`}
        description="issue 主窗口以 ACP 会话视图运行"
        disabled
        disabledHint={acpDoctorHint(doctorEntry)}
        onClick={() => {}}
        footer={(
          // 重新检测：受理单条目探测（在跑则 join），受理期间按钮 loading（受理与
          // 探测异步解耦，收敛经四态轮询自动呈现）。
          <Button
            size="small"
            onClick={() => checkDoctor.mutate(AGENT_CODE.claudeAcp)}
            loading={checkDoctor.isPending}
          >
            重新检测
          </Button>
        )}
      />
    </PanelShell>
  );
}

/**
 * 单个启动选项行：主标题 + 次行说明；disabled 时置灰并给提示（替代点击行为说明）。
 * footer 为选项行下方的可选附属操作区（与主按钮同级，绝不嵌套进 Button）。
 */
function LaunchOption({
  title,
  description,
  onClick,
  disabled = false,
  disabledHint,
  footer,
}: {
  title: string;
  description: string;
  onClick: () => void;
  disabled?: boolean;
  disabledHint?: string;
  footer?: ReactNode;
}) {
  return (
    <Box>
      <Button
        variant="outlined"
        fullWidth
        disabled={disabled}
        onClick={onClick}
        sx={{ display: 'block', textAlign: 'left', py: 1.5, px: 2 }}
      >
        <Typography variant="body2" sx={{ fontWeight: 600 }}>{title}</Typography>
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.5 }}>
          {disabled ? disabledHint ?? description : description}
        </Typography>
      </Button>
      {footer && <Box sx={{ display: 'flex', justifyContent: 'flex-end', mt: 0.5 }}>{footer}</Box>}
    </Box>
  );
}

/** 面板外壳：内容区居中 + 限宽列（与 WorkspaceInitGate 同款视觉）。 */
function PanelShell({ children }: { children: ReactNode }) {
  return (
    <Box sx={{ height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center', p: 2 }}>
      <Box sx={{ width: '100%', maxWidth: 480, display: 'flex', flexDirection: 'column', gap: 2 }}>
        {children}
      </Box>
    </Box>
  );
}

interface TerminalLaunchFlowProps {
  issueId: string;
  workspaceDir: string | null;
  /** workspace 启动模式（DevWorkbenchPage 解析下传；null = 列表解析中，等同 none 等待）。 */
  launchMode: 'none' | 'terminal-manual' | 'terminal-auto' | 'acp' | null;
  /** 终端启动 Agent 命令（workspace autoCommand，一期 claude）。 */
  autoCommand: string | undefined;
  /** 终端 Agent 展示名（面板文案）。 */
  terminalAgentLabel: string;
  /** ACP Agent 展示名（面板文案）。 */
  acpAgentLabel: string;
}

/**
 * TerminalLaunchFlow：issue 就绪后的启动分流容器（key 随 issue 挂载——临场选择态随
 * key 重置，无需 effect）。none 先出选择面板，选择后渲染终端树（自动档带直启值、
 * 手动档裸 shell）；terminal-manual 直接裸 shell 终端（工具栏按钮拉起）；terminal-auto
 * 直接直启；acp 一期终端侧按不自动启动处理（T1.6 接管为 ACP 会话视图）。
 * startupCli 按「配置/临场选择」最终值派生。
 */
export function TerminalLaunchFlow({
  issueId,
  workspaceDir,
  launchMode,
  autoCommand,
  terminalAgentLabel,
  acpAgentLabel,
}: TerminalLaunchFlowProps) {
  const [picked, setPicked] = useState<PickedLaunch | null>(null);

  const mode = launchMode ?? 'none';
  const startupCli: string | null = mode === 'terminal-auto' || picked === 'terminal-auto'
    ? autoCommand ?? 'claude'
    : null;

  if (mode === 'none' && picked == null) {
    return (
      <LaunchModePicker
        terminalAgentLabel={terminalAgentLabel}
        acpAgentLabel={acpAgentLabel}
        onPick={setPicked}
      />
    );
  }
  return <TerminalPaneRoot issueId={issueId} workspaceDir={workspaceDir} startupCli={startupCli} />;
}
