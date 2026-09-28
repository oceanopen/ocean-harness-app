import type { PluginMarketplaceModel } from '@src/services';
import {
  Autorenew as AutorenewIcon,
  DeleteOutlined as DeleteOutlinedIcon,
  ExtensionOutlined as ExtensionOutlinedIcon,
  StorefrontOutlined as StorefrontOutlinedIcon,
} from '@mui/icons-material';
import {
  Box,
  Button,
  Chip,
  Paper,
  Tooltip,
  Typography,
} from '@mui/material';
import { clamp2Sx, truncateSx } from '../styles';

// sourceType → 展示徽标文案（Go 侧归一为 local | github | git，其余透传 CLI 原始值）。
function sourceTypeLabel(sourceType: string): string {
  switch (sourceType) {
    case 'local':
      return '本地目录';
    case 'github':
      return 'GitHub';
    case 'git':
      return 'Git';
    default:
      return sourceType;
  }
}

// cli id → 展示名（多 CLI 扩展时在此登记新工具的展示名）。
function cliLabel(cli: string): string {
  return cli === 'claude' ? 'Claude Code' : cli;
}

// 单张插件市场卡片：Header 市场名 + 来源/CLI 徽标；Content 描述与注册位置；
// Actions 打开插件列表 / 更新此市场 / 移除。卡片 height:100% 对齐网格内同行高度。
interface MarketplaceCardProps {
  mk: PluginMarketplaceModel;
  updating: boolean;
  onOpen: (mk: PluginMarketplaceModel) => void;
  onUpdate: (mk: PluginMarketplaceModel) => void;
  onRemove: (mk: PluginMarketplaceModel) => void;
}

function MarketplaceCard({ mk, updating, onOpen, onUpdate, onRemove }: MarketplaceCardProps) {
  return (
    <Paper variant="outlined" sx={{ height: '100%', p: 2, display: 'flex', flexDirection: 'column', gap: 1 }}>
      {/* 头部：市场名 + 来源徽标 */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, minWidth: 0 }}>
        <Typography variant="subtitle1" sx={{ fontWeight: 600, minWidth: 0, ...truncateSx }} title={mk.name}>
          {mk.name}
        </Typography>
        <Box sx={{ flex: 1 }} />
        <Tooltip title={`来源：${mk.sourceType}`}>
          <Chip size="small" variant="outlined" icon={<StorefrontOutlinedIcon sx={{ fontSize: 15 }} />} label={sourceTypeLabel(mk.sourceType)} />
        </Tooltip>
      </Box>

      {/* 描述（清单 description，可空） */}
      {mk.description && (
        <Typography variant="caption" sx={{ color: 'text.secondary', ...clamp2Sx }}>
          {mk.description}
        </Typography>
      )}

      {/* 支持的 CLI 徽标行：清单目录探测结果；扫描异常以警示徽标替代 */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75, flexWrap: 'wrap' }}>
        {mk.scanError
          ? (
              <Tooltip title={mk.scanError}>
                <Chip size="small" color="warning" variant="outlined" label="清单异常" />
              </Tooltip>
            )
          : mk.supportedClis.map(cli => (
              <Chip
                key={cli}
                size="small"
                variant="outlined"
                color="primary"
                icon={<ExtensionOutlinedIcon sx={{ fontSize: 15 }} />}
                label={cliLabel(cli)}
              />
            ))}
        <Box sx={{ flex: 1 }} />
        <Typography variant="caption" sx={{ color: 'text.disabled' }}>
          {`${mk.plugins.length} 个插件`}
        </Typography>
      </Box>

      {/* 注册位置（claude 侧 installLocation：本地源=原路径；克隆源=clone 目录） */}
      <Typography variant="caption" sx={{ color: 'text.disabled', fontFamily: 'monospace', ...truncateSx }} title={mk.installLocation}>
        {mk.sourceDetail || mk.installLocation}
      </Typography>

      {/* 底部操作栏 */}
      <Box sx={{ mt: 'auto', pt: 1, display: 'flex', gap: 1, alignItems: 'center' }}>
        <Button size="small" variant="contained" onClick={() => onOpen(mk)}>
          插件列表
        </Button>
        <Box sx={{ flex: 1 }} />
        <Button
          size="small"
          onClick={() => onUpdate(mk)}
          disabled={updating}
          startIcon={(
            <AutorenewIcon
              sx={{
                'fontSize': 15,
                'animation': updating ? 'spin 0.8s linear infinite' : undefined,
                '@keyframes spin': {
                  from: { transform: 'rotate(0deg)' },
                  to: { transform: 'rotate(360deg)' },
                },
              }}
            />
          )}
        >
          更新
        </Button>
        <Button size="small" color="error" onClick={() => onRemove(mk)} startIcon={<DeleteOutlinedIcon sx={{ fontSize: 15 }} />}>
          移除
        </Button>
      </Box>
    </Paper>
  );
}

export default MarketplaceCard;
