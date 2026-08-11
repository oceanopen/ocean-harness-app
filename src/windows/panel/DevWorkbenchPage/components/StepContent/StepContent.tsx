import type { ProjectIssueResponseData } from '@src/services';
import CleanupStep from './CleanupStep';
import DevelopingStep from './DevelopingStep';
import PullRequestOpenStep from './PullRequestOpenStep';
import WorktreeInitStep from './WorktreeInitStep';

// StepContent：按「查看的」stateCode 切换右栏步骤内容（D1-D4）。
// stateCode 由 DevWorkbenchPage 传入（点击步骤条切换查看；默认 issue 当前步骤）；未知/in_progress → null（右栏空）。
export default function StepContent({ issue, projectId, stateCode }: {
  issue: ProjectIssueResponseData;
  projectId: number;
  stateCode: string | undefined;
}) {
  switch (stateCode) {
    case 'wt_init':
      return <WorktreeInitStep issue={issue} projectId={projectId} />;
    case 'developing':
      return <DevelopingStep issue={issue} projectId={projectId} />;
    case 'pr_open':
      return <PullRequestOpenStep issue={issue} projectId={projectId} />;
    case 'cleanup':
      return <CleanupStep issue={issue} projectId={projectId} />;
    default:
      return null;
  }
}
