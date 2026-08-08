import type { ProjectIssueResponseData } from '@src/services';
import { Alert, Box, Step, StepLabel, Stepper, Typography } from '@mui/material';
import { useProjectStateViews } from '@src/state/tracker';
import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';

// DevStepper：选中 issue 的开发步骤条。
// 步骤序列 = issue 所属项目 started 组的开发步骤子 state（排除「进行中」in_progress），按 sortOrder 排序。
// 当前步骤 = issue.stateId 在序列中的位置；之前的步骤已完成、之后待办（MUI Stepper activeStep 驱动）。
// in_progress（stateId 不在序列）→ 全部待办 + 「开始开发」提示（推进到 wt_init 留模块 E）。
export default function DevStepper({ issue, projectId }: { issue: ProjectIssueResponseData; projectId: number }) {
  const { t } = useTranslation();
  const { views } = useProjectStateViews(projectId);

  const steps = useMemo(
    () => views
      .filter(v => v.stateGroupCode === 'started' && v.stateCode !== 'in_progress')
      .sort((a, b) => a.sortOrder - b.sortOrder),
    [views],
  );
  const currentIndex = steps.findIndex(s => s.id === issue.stateId);
  const isInDevFlow = currentIndex >= 0;

  if (steps.length === 0) {
    return (
      <Typography variant="body2" color="text.secondary">{t('panel:devWorkbench.noSteps')}</Typography>
    );
  }

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
      {!isInDevFlow && (
        <Alert severity="info">{t('panel:devWorkbench.startDevHint')}</Alert>
      )}
      <Stepper activeStep={isInDevFlow ? currentIndex : -1} alternativeLabel>
        {steps.map(step => (
          <Step key={step.id}>
            <StepLabel>{step.name}</StepLabel>
          </Step>
        ))}
      </Stepper>
    </Box>
  );
}
