import { SearchOutlined as SearchOutlinedIcon } from '@mui/icons-material';
import { Box, ButtonBase, Typography } from '@mui/material';
import { isMacOS } from '@src/shared/platform';
import { useTranslation } from 'react-i18next';
import { useCommandPalette } from './CommandPaletteContext';

// 顶栏命令面板触发入口：胶囊样式（搜索图标 + 占位文案 + 快捷键徽标），点击打开面板。
// 快捷键徽标按平台自适应：macOS 显 ⌘K，Windows/Linux 显 Ctrl K（判定见 shared/platform.ts）。
const SHORTCUT_BADGE = isMacOS ? '⌘+K' : 'Ctrl+K';

function CommandPaletteTrigger() {
  const { t } = useTranslation();
  const { open } = useCommandPalette();

  return (
    <ButtonBase
      onClick={open}
      aria-label={t('panel:commandPalette.searchPlaceholder')}
      sx={{
        'display': 'flex',
        'alignItems': 'center',
        'gap': 1,
        'height': 30,
        // 窄窗收窄、宽窗舒展，避免在小窗顶栏挤掉设置按钮。
        'width': { xs: 120, sm: 150 },
        'px': 1.5,
        'borderRadius': 2,
        'border': 1,
        'borderColor': 'divider',
        'color': 'text.secondary',
        'justifyContent': 'flex-start',
        'flexShrink': 0,
        '&:hover': { bgcolor: 'action.hover' },
      }}
    >
      <SearchOutlinedIcon sx={{ fontSize: 18, color: 'text.disabled' }} />
      <Typography variant="body2" sx={{ fontSize: 12, flex: 1, color: 'text.disabled', textAlign: 'left' }} noWrap>
        {t('panel:commandPalette.searchPlaceholder')}
      </Typography>
      {/* 键帽徽标：与 Dialog 底部 Kbd 同风格（monospace + 描边 + action.hover 底）。 */}
      <Box
        component="kbd"
        sx={{
          fontFamily: 'monospace',
          fontSize: 12,
          lineHeight: 1.4,
          px: 0.5,
          borderRadius: 0.5,
          border: 1,
          borderColor: 'divider',
          bgcolor: 'action.hover',
          flexShrink: 0,
        }}
      >
        {SHORTCUT_BADGE}
      </Box>
    </ButtonBase>
  );
}

export default CommandPaletteTrigger;
