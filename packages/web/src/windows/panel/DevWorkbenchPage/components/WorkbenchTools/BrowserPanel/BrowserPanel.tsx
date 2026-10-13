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
  /** 面板区展开态（ToolPanelArea 透传）：迷你预览帧流「收起即停」闸门之一。 */
  panelVisible: boolean;
}

/**
 * BrowserPanel：浏览器会话面板（工作台互斥工具 tab）——整高会话列表（紧凑卡：迷你
 * 预览节流流纯展示、点击激活）。激活经 useBrowserStageStore 上抛：激活 profile 接管
 * 工作台终端内容区浮层（BrowserStageOverlay，挂载于 DevWorkbenchPage，与面板解耦——
 * 收面板/切工具 tab 不影响浮层）。单 profile 单订阅纪律：激活会话的实时订阅归浮层，
 * 其卡内预览停订；未激活会话由各自卡的迷你预览订阅 2s 节流流。全局 SSE 订阅驱动会话
 * 投影，面板不主动 ensure（会话经「+」新建/打开或卡片「打开会话/重开」拉起，页面切
 * 走即卸载只断订阅、会话后端常驻）。头部：会话数 + 「+」新建/打开 Popover（profile
 * 名 + 无头声明）+ 刷新。
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
    // 默认无头（对齐后端 D10 新建 profile 缺省）：面板实时画面即观察面，桌面窗口按需
    // 显式切换（人工介入通道）。
    setHeadless(true);
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
            label="无头模式（无窗口，面板实时画面）"
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
