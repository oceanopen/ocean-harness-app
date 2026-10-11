import type { ReactNode } from 'react';
import AddIcon from '@mui/icons-material/Add';
import LanguageOutlinedIcon from '@mui/icons-material/LanguageOutlined';
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  FormControlLabel,
  IconButton,
  Popover,
  Switch,
  TextField,
  Typography,
} from '@mui/material';
import { useBrowserSessionEvents, useBrowserSessionView, useEnsureBrowserSession } from '@src/state/browserSession';
import { useState } from 'react';
import PanelToolbar from '../../PanelToolbar';
import RefreshIconButton from '../../RefreshIconButton';
import BrowserSessionCard from './BrowserSessionCard';

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

interface BrowserPanelProps {
  /** 当前选中 issue id（「AI 分析此页」归因）。 */
  issueId: string;
  /** 面板区展开态（ToolPanelArea 透传）：截图轮询「收起即停」闸门。 */
  panelVisible: boolean;
}

/**
 * BrowserPanel：浏览器会话面板（工作台互斥工具 tab）——全局 SSE 订阅驱动会话投影，
 * 面板不主动 ensure（会话经「+」新建/打开或卡片「打开会话/重开」拉起，页面切走即卸载
 * 只断订阅、会话后端常驻）。头部：会话数 + 「+」新建/打开 Popover（profile 名 + 无头
 * 声明）+ 刷新。
 */
export default function BrowserPanel({ issueId, panelVisible }: BrowserPanelProps) {
  const { data: snapshot, isLoading, isFetching, error, refetch } = useBrowserSessionView();
  const ensureMutation = useEnsureBrowserSession();
  const [addAnchor, setAddAnchor] = useState<HTMLElement | null>(null);
  const [profileName, setProfileName] = useState('default');
  const [headless, setHeadless] = useState(false);

  // 全局 SSE 订阅：建连首帧快照 + 增量 sessions 帧驱动投影缓存（含 idle/failed 行回写）。
  useBrowserSessionEvents();

  const sessions = snapshot?.sessions ?? [];

  const openAdd = (anchor: HTMLElement) => {
    ensureMutation.reset();
    setProfileName('default');
    setHeadless(false);
    setAddAnchor(anchor);
  };

  const createOrOpen = () => {
    const profile = profileName.trim();
    if (profile === '') {
      return;
    }
    // headless 显式传入（D10 覆写）：面板新建动作即声明意图，后端覆写并回存 profile 偏好。
    ensureMutation.mutate({ profile, headless }, { onSuccess: () => setAddAnchor(null) });
  };

  let body: ReactNode;
  if (error != null) {
    body = (
      <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', p: 2 }}>
        <Alert
          severity="error"
          action={(
            <Button color="inherit" size="small" onClick={() => void refetch()}>
              重试
            </Button>
          )}
        >
          {errorMessage(error)}
        </Alert>
      </Box>
    );
  } else if (isLoading) {
    body = (
      <Box sx={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <CircularProgress size={20} sx={{ color: 'text.secondary' }} />
      </Box>
    );
  } else if (sessions.length === 0) {
    body = (
      <Box sx={{ flex: 1, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', gap: 1 }}>
        <LanguageOutlinedIcon sx={{ fontSize: 36, color: 'text.disabled' }} />
        <Typography variant="body2" color="text.secondary">暂无浏览器会话</Typography>
        <Typography variant="caption" color="text.secondary">
          右上角「+」新建 / 打开（会话后端常驻，切换任务不中断）
        </Typography>
      </Box>
    );
  } else {
    body = sessions.map(session => (
      <BrowserSessionCard key={session.profile} session={session} issueId={issueId} panelVisible={panelVisible} />
    ));
  }

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column', minHeight: 0 }}>
      <PanelToolbar
        left={<Typography variant="caption" color="text.secondary">{`会话 ${sessions.length}`}</Typography>}
        right={(
          <>
            <IconButton
              size="small"
              aria-label="新建/打开浏览器会话"
              onClick={e => openAdd(e.currentTarget)}
              sx={{ color: 'text.secondary' }}
            >
              <AddIcon fontSize="small" />
            </IconButton>
            <RefreshIconButton ariaLabel="刷新会话列表" fetching={isFetching} onClick={() => void refetch()} />
          </>
        )}
      />
      <Box sx={{ flex: 1, minHeight: 0, overflowY: 'auto', p: 1, display: 'flex', flexDirection: 'column', gap: 1 }}>
        {body}
      </Box>

      <Popover
        open={addAnchor != null}
        anchorEl={addAnchor}
        onClose={() => {
          if (!ensureMutation.isPending) {
            setAddAnchor(null);
          }
        }}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
        transformOrigin={{ vertical: 'top', horizontal: 'right' }}
      >
        <Box sx={{ width: 280, p: 1.5, display: 'flex', flexDirection: 'column', gap: 1 }}>
          <TextField
            size="small"
            label="Profile 名称"
            value={profileName}
            autoFocus
            onChange={e => setProfileName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                createOrOpen();
              }
            }}
          />
          <FormControlLabel
            control={<Switch size="small" checked={headless} onChange={e => setHeadless(e.target.checked)} />}
            label="无头模式（无窗口，仅截图视口）"
            sx={{ '& .MuiFormControlLabel-label': { fontSize: 12 } }}
          />
          <Typography variant="caption" color="text.secondary">
            已存在的名称直接复用会话（登录态保留）；无头/有头以本次选择覆写该 profile 偏好。
          </Typography>
          {ensureMutation.error && (
            <Alert severity="error" sx={{ fontSize: 12 }}>{errorMessage(ensureMutation.error)}</Alert>
          )}
          <Button
            variant="contained"
            size="small"
            onClick={createOrOpen}
            disabled={ensureMutation.isPending || profileName.trim() === ''}
          >
            {ensureMutation.isPending ? <CircularProgress size={14} color="inherit" /> : '新建 / 打开'}
          </Button>
        </Box>
      </Popover>
    </Box>
  );
}
