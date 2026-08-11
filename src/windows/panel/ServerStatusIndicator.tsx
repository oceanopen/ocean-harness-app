import type { HttpServerStatus } from '@src/shared/bindings';
import { Box, ButtonBase, Typography } from '@mui/material';
import { commands } from '@src/shared/bindings';
import { EVENT_HTTP_SERVER_STATE_CHANGED } from '@src/shared/events';
import { listen } from '@tauri-apps/api/event';
import { useEffect, useState } from 'react';

import { useCommandPalette } from './commandPalette/CommandPaletteContext';

// 顶栏服务状态指示器：边框胶囊（样式与命令面板触发器一致）内含色点 + 状态文字，点击跳转到服务状态页。
// 状态一律以 Rust 推送为准（前端不做判断）：commands.httpServerStatus() 初始拉取 + http-server:state-changed 事件订阅。
// 三态色点：running 绿 / starting 橙（缓慢脉冲）/ stopped 灰；与 ServerStatusPage 配色一致。

// runState → 色点颜色（MUI token，主题感知）+ 文字标签（硬编码中文，与 ServerStatusPage 一致）+ 是否脉冲。
function statusVisual(
  runState: HttpServerStatus['runState'] | null,
): { color: string; label: string } {
  switch (runState) {
    case 'running':
      return { color: 'success.main', label: '运行中' };
    case 'starting':
      return { color: 'warning.main', label: '启动中' };
    case 'stopped':
      return { color: 'text.secondary', label: '已停止' };
    default:
      // 初始拉取未完成（status==null）。
      return { color: 'text.secondary', label: '加载中' };
  }
}

function ServerStatusIndicator() {
  const { navigate } = useCommandPalette();
  const [status, setStatus] = useState<HttpServerStatus | null>(null);

  // 初始拉取 + 事件订阅：Rust 为唯一状态源，前端只镜像（与 ServerStatusPage 同源，互不影响）。
  useEffect(() => {
    void commands.httpServerStatus().then(setStatus).catch((err: unknown) => {
      console.warn('[ServerStatusIndicator] 初始状态拉取失败:', err);
    });
    const unlistenPromise = listen<HttpServerStatus>(EVENT_HTTP_SERVER_STATE_CHANGED, (e) => {
      setStatus(e.payload);
    });
    return () => {
      unlistenPromise
        .then(fn => fn())
        .catch((err: unknown) => console.warn('[ServerStatusIndicator] unlisten failed:', err));
    };
  }, []);

  const { color, label } = statusVisual(status?.runState ?? null);

  return (
    <ButtonBase
      onClick={() => navigate('serverStatus')}
      aria-label={`服务状态：${label}，点击查看详情`}
      sx={{
        'display': 'flex',
        'alignItems': 'center',
        'justifyContent': 'center',
        'gap': 1,
        'height': 30,
        'px': 1.5,
        'borderRadius': 2,
        'border': 1,
        'borderColor': 'divider',
        'color': 'text.secondary',
        'flexShrink': 0,
        '&:hover': { bgcolor: 'action.hover' },
      }}
    >
      <Box
        sx={{
          width: 8,
          height: 8,
          borderRadius: '50%',
          bgcolor: color,
          flexShrink: 0,
        }}
      />
      <Typography variant="body2" sx={{ fontSize: 12, lineHeight: 1, color: 'text.secondary' }} noWrap>
        {label}
      </Typography>
    </ButtonBase>
  );
}

export default ServerStatusIndicator;
