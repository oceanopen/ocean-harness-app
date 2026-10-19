import { request } from './http';

// LocalRepositoryModel：对齐后端 types.LocalRepositoryResponseData 的 JSON 形态（t_local_repositories）。
export interface RepoSubDir {
  subDir: string; // 相对仓库根目录的路径
  subDirDescription: string; // 子目录描述
}

export interface LocalRepositoryModel {
  id: number;
  name: string;
  localDir: string;
  description: string;
  subDirList: RepoSubDir[];
  remoteUrl: string;
  currentBranch: string;
  defaultBranch: string;
  lastCommitAt: number; // 毫秒时间戳；无提交为 0
  lastCommitMessage: string;
  createdAt: string;
  updatedAt: string;
}

// 仓库的一个 worktree 条目（不含主仓库目录），对齐后端 types.RepoWorktree。
export interface RepoWorktreeModel {
  dir: string;
  branch: string;
}

// getWorktreeList 返回的单仓条目（id 对应仓库 id），数量与抽屉展示共用此列表。
export interface LocalRepositoryWorktreeListModel {
  id: number;
  worktrees: RepoWorktreeModel[];
}

// POST /api/localRepository/getList 的入参（无筛选，列全部）。
export type LocalRepositoryGetListRequest = Record<string, never>;

// POST /api/localRepository/create 的入参。
export interface LocalRepositoryCreateRequest {
  name: string;
  localDir: string;
  description?: string;
  subDirList?: RepoSubDir[];
}

// POST /api/localRepository/update 的入参。
export interface LocalRepositoryUpdateRequest {
  id: number;
  name: string;
  localDir: string;
  description?: string;
  subDirList?: RepoSubDir[];
}

// POST /api/localRepository/delete 的入参。
export interface LocalRepositoryDeleteRequest {
  id: number;
}

// POST /api/localRepository/refresh 的入参（重解析单个仓库 git 信息）。
export interface LocalRepositoryRefreshRequest {
  id: number;
}

// POST /api/localRepository/getLocalBranches 的入参（列仓库本地分支，git branch）。
export interface LocalRepositoryGetLocalBranchesRequest {
  id: number;
}

// POST /api/localRepository/getWorktreeList 的入参（无筛选，列全部仓库）。
export type LocalRepositoryGetWorktreeListRequest = Record<string, never>;

export class LocalRepositoryService {
  // getList：返回全部本地仓库（按 lastCommitAt 倒序、id 升序）。
  static getList(): Promise<LocalRepositoryModel[]> {
    return request<LocalRepositoryModel[]>('POST', '/api/localRepository/getList', {});
  }

  // create：新建仓库（后端校验 + 解析 git 信息），返回新建实体。
  static create(req: LocalRepositoryCreateRequest): Promise<LocalRepositoryModel> {
    return request<LocalRepositoryModel>('POST', '/api/localRepository/create', req);
  }

  // update：更新仓库（后端校验 + 重解析 git 信息），返回更新后实体。
  static update(req: LocalRepositoryUpdateRequest): Promise<LocalRepositoryModel> {
    return request<LocalRepositoryModel>('POST', '/api/localRepository/update', req);
  }

  // delete：物理删除仓库（释放 localDir 供重新添加）。
  static delete(req: LocalRepositoryDeleteRequest): Promise<void> {
    return request<void>('POST', '/api/localRepository/delete', req);
  }

  // refresh：重解析单个仓库的 git 信息，返回新数据。
  static refresh(req: LocalRepositoryRefreshRequest): Promise<LocalRepositoryModel> {
    return request<LocalRepositoryModel>('POST', '/api/localRepository/refresh', req);
  }

  // refreshAll：重解析全部仓库的 git 信息，返回最新列表。
  static refreshAll(): Promise<LocalRepositoryModel[]> {
    return request<LocalRepositoryModel[]>('POST', '/api/localRepository/refreshAll', {});
  }

  // getLocalBranches：列出仓库的本地分支名（供 issue 分支选择器）。
  static getLocalBranches(req: LocalRepositoryGetLocalBranchesRequest): Promise<string[]> {
    return request<string[]>('POST', '/api/localRepository/getLocalBranches', req);
  }

  // getWorktreeList：并行解析全部仓库的额外 worktree（不含主仓库目录），卡片数量与抽屉列表同源。
  static getWorktreeList(): Promise<LocalRepositoryWorktreeListModel[]> {
    return request<LocalRepositoryWorktreeListModel[]>('POST', '/api/localRepository/getWorktreeList', {});
  }
}
