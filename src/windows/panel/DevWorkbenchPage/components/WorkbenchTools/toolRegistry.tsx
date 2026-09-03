// 开发工作台右侧工具注册表：右侧工具条（WorkbenchToolRail）图标与工具面板区
// （ToolPanelArea）tab 内容的唯一扩展点——后续接入浏览器/文件目录等新工具 = 在
// WORKBENCH_TOOLS 追加一项（icon/title/exclusive/render），rail 与 tab 自动出现。
//
// exclusive 语义：true = 同类工具全局单 tab（子任务列表/文件目录——内容单源）；false =
// 可并存多实例 tab（浏览器——每次打开新开一页）。状态域 openTool 按此分派。
//
// ⚠️ 工具会话架构红线（终端链路同款铁律）：render 只是「视口」——有状态的工具会话
// （浏览器页面/自动化 driver 等）必须后端常驻，前端 tab 卸载（切 issue/收面板/非激活）
// 只断渲染流不销毁会话，重挂载 reattach。纯前端持有状态的实现（如 iframe 直嵌）会
// 在切 issue 时丢失页面/自动化状态，禁止。
import type { SvgIconComponent } from '@mui/icons-material';
import type { ProjectIssueResponseData } from '@src/services';
import type { ReactNode } from 'react';
import { Checklist as ChecklistIcon } from '@mui/icons-material';
import IssueSubTaskPanel from '../IssueSubTaskPanel/IssueSubTaskPanel';

/// 工具渲染上下文：当前选中 issue（面板内容均围绕选中任务；hasSelection 已由外层保证非空）。
export interface WorkbenchToolRenderCtx {
  issue: ProjectIssueResponseData;
  projectId: number;
}

/// 工具定义：id 唯一；title 为 tab 头文案；exclusive 见文件头注释；render 渲染 tab 内容。
export interface WorkbenchToolDef {
  id: string;
  title: string;
  icon: SvgIconComponent;
  exclusive: boolean;
  render: (ctx: WorkbenchToolRenderCtx) => ReactNode;
}

export const WORKBENCH_TOOLS: readonly WorkbenchToolDef[] = [
  {
    id: 'subtask',
    title: '子任务',
    icon: ChecklistIcon,
    exclusive: true,
    render: ({ issue, projectId }) => <IssueSubTaskPanel projectId={projectId} issueId={issue.id} />,
  },
];

/// 按 id 查注册表项（rail/tab 渲染用）；未知 id 返回 undefined（渲染层过滤遗留 tab）。
export function toolDefById(id: string): WorkbenchToolDef | undefined {
  return WORKBENCH_TOOLS.find(t => t.id === id);
}
