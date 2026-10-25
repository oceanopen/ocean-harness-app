import type { WorkbenchToolIssue } from '../WorkbenchTools/toolRegistry';
import SmartToyOutlinedIcon from '@mui/icons-material/SmartToyOutlined';
import { Alert, Box, Button, CircularProgress, Tooltip, Typography } from '@mui/material';
import { imBotsKeys, useImBots } from '@src/state/imBots';
import { useQueryClient } from '@tanstack/react-query';
import PanelToolbar from '../PanelToolbar';
import RefreshIconButton from '../RefreshIconButton';
import ImBotBotCard from './ImBotBotCard';

interface ImBotPanelProps {
  // 当前选中锚定（挂载方经 toolRegistry ctx 保证非空）：conversationScope + projectId/
  // containerId/id 共同决定 bot 关联过滤域。
  issue: WorkbenchToolIssue;
}

/**
 * ImBotPanel：开发工作台工具面板区的「IM 机器人」tab 内容（经 toolRegistry 挂载），双场景：
 * 任务语境 = 项目档且关联本项目、任务未限定（空 = 项目通用）或对准本任务的 bot；工作空间
 * 语境 = 工作空间档且关联本工作空间的 bot（并标注其 IM 会话是否绑定当前选中会话）。归属
 * 变更统一走「IM 机器人」页编辑抽屉，面板无写操作。数据：useImBots 共享列表缓存 + 会话明细
 * per-bot query，实时性靠头部刷新按钮手动 invalidate 整域。
 */
export default function ImBotPanel({ issue }: ImBotPanelProps) {
  const qc = useQueryClient();
  const { data: bots = [], isLoading, error, isFetching } = useImBots();
  const workspaceScope = issue.conversationScope === 'workspace';

  // 关联 bot：任务语境 = 项目档 + 本项目 + 任务未限定（空）或对准本任务；工作空间语境 =
  // 工作空间档 + 本空间；其他项目/空间档不在此列。
  const relatedBots = workspaceScope
    ? bots.filter(bot => bot.conversationType === 'workspace' && bot.workspaceId === issue.containerId)
    : bots.filter(
        bot => bot.conversationType === 'project'
          && bot.projectId === issue.projectId
          && (bot.taskId === '' || bot.taskId === issue.id),
      );
  // 离线提示：存在启用中但断连的 bot（渠道无关），标题红点 + 卡片红色徽标（后者已有）。
  const hasOfflineBot = relatedBots.some(bot => bot.enabled && bot.connState === 'disconnected');

  const refresh = () => {
    // 整域失效：列表 + 全部会话 key 一并重取（跨 bot 的 IM 侧变更经此手动回填）。
    void qc.invalidateQueries({ queryKey: imBotsKeys.root });
  };

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      {/* 头部：机器人计数 + 刷新（RefreshIconButton，isFetching 旋转） */}
      <PanelToolbar
        left={!isLoading && relatedBots.length > 0
          ? (
              <Box sx={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                <Typography variant="caption" color="text.secondary">{`机器人 ${relatedBots.length}`}</Typography>
                {hasOfflineBot && (
                  <Tooltip title="存在离线 bot，@消息将丢失直至重新连接">
                    <Box sx={{ width: '6px', height: '6px', borderRadius: '50%', bgcolor: 'error.main' }} />
                  </Tooltip>
                )}
              </Box>
            )
          : null}
        right={<RefreshIconButton onClick={refresh} fetching={isFetching} ariaLabel="刷新" />}
      />

      {/* 内容区：错误 / 加载中 / 空态引导 / bot 聚合卡列表 */}
      {error
        ? (
            <Box sx={{ p: '12px' }}>
              <Alert
                severity="error"
                action={<Button color="inherit" size="small" onClick={refresh}>重试</Button>}
              >
                机器人列表加载失败：{error.message}
              </Alert>
            </Box>
          )
        : isLoading
          ? (
              <Box sx={{ display: 'flex', justifyContent: 'center', p: '16px' }}>
                <CircularProgress size={20} />
              </Box>
            )
          : relatedBots.length === 0
            ? (
                <Box
                  sx={{
                    flex: 1,
                    display: 'flex',
                    flexDirection: 'column',
                    alignItems: 'center',
                    justifyContent: 'center',
                    gap: '8px',
                    p: '16px',
                    textAlign: 'center',
                  }}
                >
                  <SmartToyOutlinedIcon sx={{ fontSize: '48px', color: 'text.secondary' }} />
                  <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
                    {workspaceScope ? '当前工作空间暂无关联机器人' : '当前任务暂无关联机器人'}
                  </Typography>
                  <Typography variant="body2" color="text.secondary">
                    {workspaceScope
                      ? '在「IM 机器人」页编辑机器人，选择本工作空间即可接入'
                      : '在「IM 机器人」页编辑机器人，选择本项目与本任务即可接入'}
                  </Typography>
                </Box>
              )
            : (
                <Box sx={{ flex: 1, overflow: 'auto', pt: '12px', pb: '4px', px: '12px', display: 'flex', flexDirection: 'column', gap: '12px' }}>
                  {relatedBots.map(bot => (
                    <ImBotBotCard
                      key={bot.id}
                      bot={bot}
                      linkedConversationId={workspaceScope ? issue.id : undefined}
                    />
                  ))}
                </Box>
              )}
    </Box>
  );
}
