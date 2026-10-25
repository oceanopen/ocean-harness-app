import type { WorkbenchContainer } from '@src/windows/panel/DevWorkbenchPage/components/WorkbenchContent';
import { Box } from '@mui/material';
import { useProjects } from '@src/state/projects';
import { useProjectTasks } from '@src/state/tracker';
import WorkbenchContent from '@src/windows/panel/DevWorkbenchPage/components/WorkbenchContent';
import { PROJECTS_PID_PARAM, PROJECTS_TID_PARAM, strParam } from '@src/windows/panel/routes';
import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import ProjectCardGrid from './components/ProjectCardGrid';
import ProjectTree from './components/ProjectTree';

/**
 * ProjectsPage：「项目任务」页（/projects）——左树 + 共用工作台内容区。
 * 选中任务由 URL ?pid=&tid= 驱动：container = 任务所属项目，task = 选中任务对象；
 * 未选任务态由左树「列表」开关驱动内容区（开=项目卡片网格，关=引导空态），
 * 内容区恒由 T6.2 任务主会话编排承载（conversationScope='task'）。
 */
export default function ProjectsPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const urlPid = strParam(searchParams.get(PROJECTS_PID_PARAM));
  const urlTid = strParam(searchParams.get(PROJECTS_TID_PARAM));
  const { data: projects = [], isPending: projectsPending } = useProjects();
  // 任务列表（pid 即列表缓存 key；null 时不发请求）。
  const { data: tasks = [], isPending: tasksPending } = useProjectTasks(urlPid);
  const project = projects.find(p => p.id === urlPid) ?? null;
  const task = tasks.find(i => i.id === urlTid) ?? null;
  // 「列表」开关（页面级 SSOT：左树图标高亮 + 未选任务态内容区卡片网格共用）。
  const [listOpen, setListOpen] = useState(false);

  // pid 不可解析（项目已删除/脏 URL）：项目列表就绪后清参回 bare 页。
  useEffect(() => {
    if (!projectsPending && urlPid != null && project == null) {
      setSearchParams({}, { replace: true });
    }
  }, [projectsPending, urlPid, project, setSearchParams]);

  // tid 不可解析（任务已删除/移出）：仅清 tid 保留项目上下文。
  useEffect(() => {
    if (urlPid != null && !tasksPending && urlTid != null && task == null) {
      const next = new URLSearchParams(searchParams);
      next.delete(PROJECTS_TID_PARAM);
      setSearchParams(next, { replace: true });
    }
  }, [urlPid, tasksPending, urlTid, task, searchParams, setSearchParams]);

  const container: WorkbenchContainer | null = project == null
    ? null
    : { id: project.id, name: project.name, dir: project.dir, launchSettings: project.launchSettings };

  // 点项目卡片：仅写 pid 清 tid（项目选中经卡片/树均可，任务选择经左树完成）。
  const selectProject = (pid: string) => {
    const next = new URLSearchParams(searchParams);
    next.set(PROJECTS_PID_PARAM, pid);
    next.delete(PROJECTS_TID_PARAM);
    setSearchParams(next);
  };
  // 点任务行/新建任务：写 pid+tid（内容区回落 T6.2 主会话编排）。
  const selectTask = (pid: string, tid: string) => {
    const next = new URLSearchParams(searchParams);
    next.set(PROJECTS_PID_PARAM, pid);
    next.set(PROJECTS_TID_PARAM, tid);
    setSearchParams(next);
  };

  const projectTree = (
    <ProjectTree
      selectedProjectId={project?.id ?? null}
      selectedTaskId={task?.id ?? null}
      listOpen={listOpen}
      onToggleList={() => setListOpen(v => !v)}
      onSelectTask={selectTask}
      onSelectProject={selectProject}
    />
  );

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      <Box sx={{ flex: 1, minHeight: '0px' }}>
        {/* 「列表」开关：icon 打开 = 内容区出项目卡片网格；关闭 = 三栏工作台框架
        （未选任务时内容区空态引导，右侧工具区框架恒在——对齐工作空间页交互）。 */}
        {listOpen
          ? (
              <Box sx={{ display: 'flex', height: '100%', overflow: 'hidden' }}>
                <Box sx={{ width: '260px', flexShrink: 0, borderRight: '1px solid', borderColor: 'divider' }}>
                  {projectTree}
                </Box>
                <Box sx={{ flex: 1, minWidth: '0px' }}>
                  <ProjectCardGrid
                    projects={projects}
                    loading={projectsPending}
                    selectedProjectId={project?.id ?? null}
                    onSelectProject={p => selectProject(p.id)}
                  />
                </Box>
              </Box>
            )
          : (
              <WorkbenchContent
                container={container}
                containerPending={projectsPending}
                task={task}
                selectedTaskId={urlTid}
                tasksLoading={tasksPending}
                conversationScope="task"
                leftTree={projectTree}
                onClearSelection={() => {
                  // 清任务选中、保留项目容器上下文（归档/删除后回落列表开关态）。
                  const next = new URLSearchParams(searchParams);
                  next.delete(PROJECTS_TID_PARAM);
                  setSearchParams(next, { replace: true });
                }}
              />
            )}
      </Box>
    </Box>
  );
}
