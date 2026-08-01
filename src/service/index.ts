// src/service 统一导出入口。
//
// 页面与组件统一从此处导入 service 与类型，例如：
//   import { WorkspaceService, type WorkspaceModel } from '@src/service';
//
// 底层 request 仅 service 内部使用，不从此处导出——页面不直接发请求，
// 一切请求经 XxxService 收敛，便于后续接入 cache 与跨页面复用。

// ===== Service（含静态方法）=====
export { BaseInfoService } from './BaseInfoService';
export type { ServerInfo, ServerRunInfoRequest, ServerRunInfoResponseData, SysInfo } from './BaseInfoService';

export { ProjectIssueService } from './ProjectIssueService';
export type {
  Priority,
  ProjectIssueCreateRequest,
  ProjectIssueDeleteRequest,
  ProjectIssueGetListRequest,
  ProjectIssueMoveRequest,
  ProjectIssueResponseData,
  ProjectIssueUpdateRequest,
} from './ProjectIssueService';

export { ProjectStateService } from './ProjectStateService';
export type { ProjectStateGetListRequest, ProjectStateModel, StateGroup } from './ProjectStateService';

export { WorkspaceLabelService } from './WorkspaceLabelService';
export type {
  WorkspaceLabelCreateRequest,
  WorkspaceLabelDeleteRequest,
  WorkspaceLabelGetListRequest,
  WorkspaceLabelModel,
  WorkspaceLabelUpdateRequest,
} from './WorkspaceLabelService';

export { WorkspaceProjectService } from './WorkspaceProjectService';
export type {
  WorkspaceProjectCreateRequest,
  WorkspaceProjectDeleteRequest,
  WorkspaceProjectGetListRequest,
  WorkspaceProjectModel,
  WorkspaceProjectUpdateRequest,
} from './WorkspaceProjectService';

export { WorkspaceService } from './WorkspaceService';
export type {
  WorkspaceCreateRequest,
  WorkspaceDeleteRequest,
  WorkspaceGetListRequest,
  WorkspaceModel,
  WorkspaceUpdateRequest,
} from './WorkspaceService';
