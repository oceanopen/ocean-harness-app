import { request } from './http';

// issue 开发流程 worktree 元数据（对应 Go /api/tracker/issueWorktree/*）。
// P1 桩（Module G）：createWorktree 后端派生假 worktree 路径写记录（不真调 git worktree add）；真实现见 docs/worktree_term.md §6。

// worktree 物理生命周期状态（与 issue 开发阶段 init/developing/pull_request/cleanup 正交）。
export type IssueWorktreeStatus = 'active' | 'stale' | 'removed';

// IssueWorktreeModel worktree 记录。worktreeId = `${repoId}::${absPath}`，Go 与 Rust 共享键。
export interface IssueWorktreeModel {
  id: number;
  worktreeId: string;
  issueId: number;
  localRepositoryId: number;
  worktreePath: string; // P1 桩为派生占位路径
  worktreeBranch: string;
  baseBranch: string;
  status: IssueWorktreeStatus;
  createdAt: string;
  deletedAt?: string | null;
}

// POST /api/tracker/issueWorktree/createWorktree 入参。
export interface IssueWorktreeCreateWorktreeRequest {
  issueId: number;
  localRepositoryId: number;
  baseBranch?: string; // 基准分支（P1 桩仅存档）
  worktreeBranch: string; // 开发分支名
}

// POST /api/tracker/issueWorktree/removeWorktree 入参。前置：前端已调 pty_stop_for_worktree 停 PTY（§9.3）。
export interface IssueWorktreeRemoveWorktreeRequest {
  worktreeId: string;
}

// POST /api/tracker/issueWorktree/updateWorktree 的入参（比对仓库+基准分支+开发分支三项，任一不同删旧建新）。
export interface IssueWorktreeUpdateWorktreeRequest {
  id: number;
  localRepositoryId: number;
  baseBranch?: string;
  worktreeBranch: string;
}

// POST /api/tracker/issueWorktree/getList 入参。
export interface IssueWorktreeGetListRequest {
  issueId: number;
}

export class IssueWorktreeService {
  // createWorktree：为 issue 创建 worktree 记录（P1 桩：派生假路径，不真调 git），返回 active 记录（幂等）。
  static createWorktree(req: IssueWorktreeCreateWorktreeRequest): Promise<IssueWorktreeModel> {
    return request<IssueWorktreeModel>('POST', '/api/tracker/issueWorktree/createWorktree', req);
  }

  // removeWorktree：软删 worktree 记录（status=removed）。前置：前端已停 PTY。P1 桩不真删目录。
  static removeWorktree(req: IssueWorktreeRemoveWorktreeRequest): Promise<void> {
    return request<void>('POST', '/api/tracker/issueWorktree/removeWorktree', req);
  }

  // getList：列某 issue 的 active worktree（前端作 worktreePath/worktreeId 的 SSOT）。
  static getList(req: IssueWorktreeGetListRequest): Promise<IssueWorktreeModel[]> {
    return request<IssueWorktreeModel[]>('POST', '/api/tracker/issueWorktree/getList', req);
  }

  // updateWorktree：更新已有 worktree（分支变了删旧重建，不变 no-op），返回更新后的实体。
  static updateWorktree(req: IssueWorktreeUpdateWorktreeRequest): Promise<IssueWorktreeModel> {
    return request<IssueWorktreeModel>('POST', '/api/tracker/issueWorktree/updateWorktree', req);
  }
}
