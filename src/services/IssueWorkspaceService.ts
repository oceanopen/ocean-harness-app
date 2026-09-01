import { request } from './http';

// ─── 类型（对齐后端 src-server/internal/dal/types/issue_workspace.go）───

/** 工作空间初始化状态（服务态与步骤/仓库级共用一套取值）。 */
export type IssueWorkspaceStatus
  = | 'NOT_INITIALIZED' // 状态文件不存在（从未初始化）
    | 'PENDING' // 待执行（步骤初始态）
    | 'RUNNING' // 后台任务执行中
    | 'SUCCESS' // 全部步骤成功（= 工作空间就绪）
    | 'FAILED' // 任一步骤失败（可重试）
    | 'CORRUPTED' // 状态文件损坏（重新 init 覆盖修复）
    | 'INTERRUPTED' // 进程中断遗留（可重试）
    | 'SKIPPED'; // 步骤/仓库级：本期占位未实现

/** 初始化步骤 key（固定顺序执行：createDirs → sshConfig → mcpConfig → cloneRepos）。 */
export const ISSUE_WORKSPACE_STEP_KEY = {
  CREATE_DIRS: 'createDirs',
  SSH_CONFIG: 'sshConfig',
  MCP_CONFIG: 'mcpConfig',
  CLONE_REPOS: 'cloneRepos',
} as const;

/** cloneRepos 步骤内的仓库级子状态。 */
export interface IssueWorkspaceRepoState {
  localRepositoryId: number;
  name: string;
  remoteUrl: string;
  baseBranch: string;
  targetBranch: string;
  status: IssueWorkspaceStatus;
  message: string;
}

/** 单个全局步骤的进度（message 为步骤级说明，如 SKIPPED 的降级原因）。 */
export interface IssueWorkspaceStep {
  key: string;
  title: string;
  status: IssueWorkspaceStatus;
  repos?: IssueWorkspaceRepoState[];
  message?: string;
}

/** 幂等 manifest 的一项（已初始化的仓库+基准分支）。 */
export interface IssueWorkspaceRepoRef {
  localRepositoryId: number;
  remoteUrl: string;
  baseBranch: string;
}

/** 状态文件（.workspace-init-state.json）全文。 */
export interface IssueWorkspaceState {
  version: number;
  issueId: string;
  baseDir: string;
  status: IssueWorkspaceStatus;
  steps: IssueWorkspaceStep[];
  manifest: IssueWorkspaceRepoRef[];
  error: string;
  createdAt: string;
  updatedAt: string;
}

/** init/status 共用响应：serverStatus 为顶层结论，state 为状态文件全文（未初始化时 null）。 */
export interface IssueWorkspaceStatusResponseData {
  serverStatus: IssueWorkspaceStatus;
  state: IssueWorkspaceState | null;
}

// POST /api/issueWorkspace/init 的入参（baseDir 须为绝对路径，service 层校验）。
export interface IssueWorkspaceInitRequest {
  issueId: string;
  baseDir: string;
}

// POST /api/issueWorkspace/status 的入参。
export interface IssueWorkspaceStatusRequest {
  issueId: string;
  baseDir: string;
}

export class IssueWorkspaceService {
  // init：受理工作空间初始化（异步执行；幂等可重入——执行中重复触发返回当前进度，
  // 已成功且关联未变直接 SUCCESS，失败重试只补失败仓库）。返回受理后的状态快照。
  static init(req: IssueWorkspaceInitRequest): Promise<IssueWorkspaceStatusResponseData> {
    return request<IssueWorkspaceStatusResponseData>('POST', '/api/issueWorkspace/init', req);
  }

  // status：查询初始化进度（读状态文件派生，不查库），供前端轮询。
  static status(req: IssueWorkspaceStatusRequest): Promise<IssueWorkspaceStatusResponseData> {
    return request<IssueWorkspaceStatusResponseData>('POST', '/api/issueWorkspace/status', req);
  }
}
