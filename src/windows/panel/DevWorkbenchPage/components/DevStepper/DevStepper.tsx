import type { ProjectIssueResponseData } from '@src/services';
import { Step, StepButton, Stepper, Typography } from '@mui/material';
import { getDevSteps } from '@src/state/devWorkbench';
import { useProjectStateViews } from '@src/state/tracker';
import { useMemo } from 'react';

// DevStepper：选中 issue 的开发步骤条（可点击切换查看）。
// 步骤序列 = started 组开发步骤（排除进行中 in_progress），按 sortOrder。
// activeStep：activeStepCode 指定（点击查看的步骤）则高亮该步骤，否则回落 issue 当前进度（currentIndex）。
// 点 StepButton → onStepClick(stateCode)，DevWorkbenchPage 据此切换右内容区查看的步骤。
interface DevStepperProps {
  issue: ProjectIssueResponseData;
  projectId: number;
  /** 当前查看的步骤 stateCode（null = 用 issue 当前进度步骤）。 */
  activeStepCode?: string | null;
  /** 点击步骤回调（切换查看）。 */
  onStepClick?: (stateCode: string) => void;
}

export default function DevStepper({ issue, projectId, activeStepCode, onStepClick }: DevStepperProps) {
  const { views } = useProjectStateViews(projectId);

  const steps = useMemo(() => getDevSteps(views), [views]);
  const currentIndex = steps.findIndex(s => s.id === issue.stateId);
  const viewingIndex = activeStepCode ? steps.findIndex(s => s.stateCode === activeStepCode) : -1;
  const activeStep = viewingIndex >= 0 ? viewingIndex : (currentIndex >= 0 ? currentIndex : -1);

  if (steps.length === 0) {
    return (
      <Typography variant="body2" color="text.secondary">该项目未配置开发步骤</Typography>
    );
  }

  return (
    <Stepper activeStep={activeStep} orientation="vertical" nonLinear>
      {steps.map(step => (
        <Step key={step.id}>
          <StepButton onClick={() => onStepClick?.(step.stateCode)}>{step.name}</StepButton>
        </Step>
      ))}
    </Stepper>
  );
}
