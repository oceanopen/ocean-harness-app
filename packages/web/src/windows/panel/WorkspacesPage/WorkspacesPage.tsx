import type { WorkbenchContainer } from '@src/windows/panel/DevWorkbenchPage/components/WorkbenchContent';
import { Box } from '@mui/material';
import { useConversations } from '@src/state/conversation';
import { useWorkspaces } from '@src/state/tracker';
import WorkbenchContent from '@src/windows/panel/DevWorkbenchPage/components/WorkbenchContent';
import { strParam, WORKSPACES_CID_PARAM, WORKSPACES_WID_PARAM } from '@src/windows/panel/routes';
import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import WorkspaceCardGrid from './components/WorkspaceCardGrid';
import WorkspaceTree from './components/WorkspaceTree';

/**
 * WorkspacesPage：「工作空间」页（/workspaces）——左树 + 共用工作台内容区。
 * 选中工作空间/会话由 URL ?wid=&cid= 驱动：wid 上抛为容器（{id,name,dir,launchSettings}），
 * cid 上抛为会话选中意图。「列表」开关开 = 内容区切工作空间卡片网格（挑工作空间用），
 * 关/未选中 = 三栏工作台框架（内容区空态引导选会话）。
 */
export default function WorkspacesPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const urlWid = strParam(searchParams.get(WORKSPACES_WID_PARAM));
  const urlCid = strParam(searchParams.get(WORKSPACES_CID_PARAM));
  const { data: workspaces = [], isPending } = useWorkspaces();
  const selected = workspaces.find(w => w.id === urlWid) ?? null;
  // 「列表」开关（页面级 SSOT：左树图标高亮 + 未选中态内容区卡片网格共用）。
  const [listOpen, setListOpen] = useState(false);
  // 选中工作空间的会话列表（cid 失效清参消费；与内容区订阅同 key，缓存去重）。
  const { data: conversations = [], isPending: conversationsPending, isFetching: conversationsFetching } = useConversations('workspace', selected?.id ?? null);

  // wid 不可解析（已删除/脏 URL）：列表就绪后 replace 回 bare 页，左树随之取消高亮。
  useEffect(() => {
    if (!isPending && urlWid != null && selected == null) {
      setSearchParams({}, { replace: true });
    }
  }, [isPending, urlWid, selected, setSearchParams]);

  // cid 不可解析（会话已删除）：会话列表定态后清 cid 保留工作空间上下文。
  useEffect(() => {
    if (urlWid != null && urlCid != null && !conversationsPending && !conversationsFetching
      && !conversations.some(c => c.id === urlCid)) {
      const next = new URLSearchParams(searchParams);
      next.delete(WORKSPACES_CID_PARAM);
      setSearchParams(next, { replace: true });
    }
  }, [urlWid, urlCid, conversations, conversationsPending, conversationsFetching, searchParams, setSearchParams]);

  const container: WorkbenchContainer | null = selected == null
    ? null
    : { id: selected.id, name: selected.name, dir: selected.dir, launchSettings: selected.launchSettings };

  // 切工作空间：清 cid + 收起列表浮层（选完即进入内容区）。
  const selectWorkspace = (wid: string) => {
    const next = new URLSearchParams(searchParams);
    next.set(WORKSPACES_WID_PARAM, wid);
    next.delete(WORKSPACES_CID_PARAM);
    setListOpen(false);
    setSearchParams(next);
  };
  // 点会话行/新建会话进入：一次写 wid+cid（工作空间选中随会话归属同步）。
  const selectConversation = (wid: string, cid: string) => {
    const next = new URLSearchParams(searchParams);
    next.set(WORKSPACES_WID_PARAM, wid);
    next.set(WORKSPACES_CID_PARAM, cid);
    setListOpen(false);
    setSearchParams(next);
  };

  // 「列表」开关是内容区切卡片列表的唯一门控；关/未选中一律回三栏工作台框架。
  const showList = listOpen;

  const workspaceTree = (
    <WorkspaceTree
      selectedId={selected?.id ?? null}
      selectedConversationId={urlCid}
      listOpen={listOpen}
      onToggleList={() => setListOpen(v => !v)}
      onSelectWorkspace={ws => selectWorkspace(ws.id)}
      onSelectConversation={selectConversation}
    />
  );

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      <Box sx={{ flex: 1, minHeight: '0px' }}>
        {showList
          ? (
              <Box sx={{ display: 'flex', height: '100%', overflow: 'hidden' }}>
                {/* 列表态：左树保留，内容区为工作空间卡片网格（选中/新建即返回内容区）。 */}
                <Box sx={{ width: '260px', flexShrink: 0, borderRight: '1px solid', borderColor: 'divider' }}>
                  {workspaceTree}
                </Box>
                <Box sx={{ flex: 1, minWidth: '0px' }}>
                  <WorkspaceCardGrid
                    workspaces={workspaces}
                    loading={isPending}
                    onSelectWorkspace={ws => selectWorkspace(ws.id)}
                  />
                </Box>
              </Box>
            )
          : (
              <WorkbenchContent
                container={container}
                containerPending={isPending}
                task={null}
                selectedTaskId={null}
                tasksLoading={false}
                conversationScope="workspace"
                selectedConversationId={urlCid}
                leftTree={workspaceTree}
              />
            )}
      </Box>
    </Box>
  );
}
