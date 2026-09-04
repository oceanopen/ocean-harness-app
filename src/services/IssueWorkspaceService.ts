import { request, serverUrl } from './http';

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

/**
 * 初始化步骤 key（固定顺序执行：createDirs → sshConfig → cloneRepos）。
 *  注：mcpConfig 步骤已取消——MCP 配置由 ocean-harness 插件捆绑提供（T1.3 方案变更）。
 */
export const ISSUE_WORKSPACE_STEP_KEY = {
  CREATE_DIRS: 'createDirs',
  SSH_CONFIG: 'sshConfig',
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

/** 归档/取消动作（T3.2）：archive → issue 置 DONE；cancel → issue 置 CANCELLED。 */
export type IssueWorkspaceArchiveAction = 'archive' | 'cancel';

/**
 * POST /api/issueWorkspace/archive 的入参。两段式：force=false 仅安全检查（未提交变更 +
 * 未推送提交）返回警告不执行；force=true 跳过检查直接执行（删目录 + 后端流转状态，
 * 前端二次确认后携带——执行段前须 ptyShutdownIssue 关闭该 issue 全部终端会话）。
 */
export interface IssueWorkspaceArchiveRequest {
  issueId: string;
  baseDir: string;
  action: IssueWorkspaceArchiveAction;
  force: boolean;
}

/** archive 响应：force=false 时 executed 恒 false，warnings 为空 = 检查干净。 */
export interface IssueWorkspaceArchiveResponseData {
  executed: boolean;
  warnings: string[];
}

// ─── 文件浏览（T5.1 本期：列表 + 预览，对齐后端 types/issue_workspace_files.go）───

/**
 * 文件内容传输类型（后端定夺，前后端单一真相）：text（UTF-8 全文，呈现分派在前端按
 * 扩展名）/ image（元信息，字节经 fileRaw URL 直连）/ binary（检出非文本，提示态）/
 * tooLarge（超文本预览上限，合法响应态）。
 */
export type IssueWorkspaceFileContentKind
  = | 'text'
    | 'image'
    | 'binary'
    | 'tooLarge';

/** POST /api/issueWorkspace/getFileTree 的入参。 */
export interface IssueWorkspaceFileTreeRequest {
  issueId: string;
  baseDir: string;
}

/**
 * 文件树节点（扁平表成员）。path 为相对 {baseDir}/{issueId}/ 的正斜杠路径，前端直接
 * 用作树 key 与 getFileContent 的 path 入参；目录 size 恒 0。
 */
export interface IssueWorkspaceFileNode {
  path: string;
  name: string;
  isDir: boolean;
  size: number;
}

/** getFileTree 响应：一次性全树扁平节点表 + 截断标志（节点数超后端上限）。 */
export interface IssueWorkspaceFileTreeResponseData {
  nodes: IssueWorkspaceFileNode[];
  truncated: boolean;
}

/** POST /api/issueWorkspace/getFileContent 的入参（path 即树节点 path）。 */
export interface IssueWorkspaceFileContentRequest {
  issueId: string;
  baseDir: string;
  path: string;
}

/** getFileContent 响应：各 kind 字段占用——text→content / image→mimeType（字节走 fileRaw）/ 其余仅 size。 */
export interface IssueWorkspaceFileContentResponseData {
  kind: IssueWorkspaceFileContentKind;
  size: number;
  mimeType?: string;
  content?: string;
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

  // archive：归档/取消工作空间（T3.2，两段式契约见请求类型注释）。
  static archive(req: IssueWorkspaceArchiveRequest): Promise<IssueWorkspaceArchiveResponseData> {
    return request<IssueWorkspaceArchiveResponseData>('POST', '/api/issueWorkspace/archive', req);
  }

  // getFileTree：一次性返回工作空间全部文件/目录扁平节点表（前端纯函数组树）。
  static fileTree(req: IssueWorkspaceFileTreeRequest): Promise<IssueWorkspaceFileTreeResponseData> {
    return request<IssueWorkspaceFileTreeResponseData>('POST', '/api/issueWorkspace/getFileTree', req);
  }

  // getFileContent：读取单个文件内容并定夺传输 kind（text/image/binary/tooLarge）。
  static fileContent(req: IssueWorkspaceFileContentRequest): Promise<IssueWorkspaceFileContentResponseData> {
    return request<IssueWorkspaceFileContentResponseData>('POST', '/api/issueWorkspace/getFileContent', req);
  }

  // fileRawUrl：图片原始字节直连 URL（<img src>，类静态资源；base 解析同 request）。
  // v 为缓存刷新参数（重验/重开时变化强制重新加载）。
  static fileRawUrl(req: { issueId: string; baseDir: string; path: string; v?: string | number }): Promise<string> {
    const query: Record<string, string> = { issueId: req.issueId, baseDir: req.baseDir, path: req.path };
    if (req.v != null) {
      query.v = String(req.v);
    }
    return serverUrl('/api/issueWorkspace/fileRaw', query);
  }
}
