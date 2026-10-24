/**
 * 开发工作台右侧工具注册表：右侧工具条（WorkbenchToolRail）图标与工具面板区（ToolPanelArea）tab 内容的唯一扩展点——
 * 新增工具在 WORKBENCH_TOOLS 追加一项（icon/title/exclusive/render），rail 与 tab 自动出现；exclusive：true = 同类工具全局单 tab（内容后端全局单源），false = 可并存多实例 tab。
 * ⚠️ 架构红线（终端链路同款铁律）：render 只是「视口」——有状态的工具会话必须后端常驻，tab 卸载只断渲染流、重挂载 reattach；纯前端持有状态禁止。
 */
import type { SvgIconComponent } from '@mui/icons-material';
import type { ReactNode } from 'react';
import { FolderOutlined as FolderOutlinedIcon, ForumOutlined as ForumOutlinedIcon, Language as LanguageIcon, SmartToy as SmartToyIcon } from '@mui/icons-material';
import ClaudeSessionsViewer from '../ClaudeSessionsViewer/ClaudeSessionsViewer';
import ImBotPanel from '../ImBotPanel/ImBotPanel';
import WorkspaceFilePanel from '../WorkspaceFilePanel/WorkspaceFilePanel';
import BrowserPanel from './BrowserPanel/BrowserPanel';

/**
 * 工具渲染锚定（结构子集）：任务页 = 选中任务；工作空间页 = 选中会话（projectId 回落
 * 容器 id）。各工具的数据面仍按自身维度消费这些字段，锚定不改变查询语义。
 */
export interface WorkbenchToolIssue {
  id: string;
  projectId: string;
  name: string;
}

/**
 * 工具渲染上下文：当前选中锚定（任务或会话；可用性已由外层门禁保证非空）。
 */
export interface WorkbenchToolRenderCtx {
  issue: WorkbenchToolIssue;
  /**
   * 面板区展开态（收起 = 外层 width 0 但本组件仍挂载）：帧流类视口的暂停闸门
   * （浏览器面板实时画面「收起即停」，D4 展示侧对偶）。
   */
  panelVisible: boolean;
}

// 工具定义：id 唯一；title 为 tab 头文案；exclusive 见文件头注释；render 渲染 tab 内容。
export interface WorkbenchToolDef {
  id: string;
  title: string;
  icon: SvgIconComponent;
  exclusive: boolean;
  // 锚定维度超出工具适用域（如任务会话维度工具落在工作空间会话上）→ 工具条禁用、面板区不渲染。
  scopedOut?: (ctx: WorkbenchToolRenderCtx) => boolean;
  render: (ctx: WorkbenchToolRenderCtx) => ReactNode;
}

export const WORKBENCH_TOOLS: readonly WorkbenchToolDef[] = [
  {
    id: 'files',
    title: '文件',
    icon: FolderOutlinedIcon,
    exclusive: true,
    render: ({ issue }) => <WorkspaceFilePanel issueId={issue.id} />,
  },
  {
    id: 'imBots',
    title: 'IM 机器人',
    icon: SmartToyIcon,
    exclusive: true,
    render: ({ issue }) => <ImBotPanel issue={issue} />,
  },
  {
    id: 'claudeSessions',
    title: 'Claude 会话',
    icon: ForumOutlinedIcon,
    exclusive: true,
    // 任务会话维度工具（子会话/关联 bot 查看）：工作空间会话无子会话概念，恒不可用。
    scopedOut: () => true,
    render: ({ issue }) => <ClaudeSessionsViewer issueId={issue.id} projectId={issue.projectId} />,
  },
  {
    id: 'browser',
    title: '浏览器',
    icon: LanguageIcon,
    exclusive: true,
    render: ({ issue, panelVisible }) => (
      <BrowserPanel issueId={issue.id} panelVisible={panelVisible} />
    ),
  },
];

// 按 id 查注册表项（rail/tab 渲染用）；未知 id 返回 undefined（渲染层过滤遗留 tab）。
export function toolDefById(id: string): WorkbenchToolDef | undefined {
  return WORKBENCH_TOOLS.find(t => t.id === id);
}
