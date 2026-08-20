import type { SelectChangeEvent } from '@mui/material/Select';
import type { Iterm2SplitDirection, TerminalCursorStyle, TerminalFontSize, TerminalLineHeight, TerminalScrollbackRows, TerminalStartupCodeCli } from '@src/shared/appConfig';
import type { YesNo } from '@src/shared/bindings';
import type { TerminalThemeId } from '@src/windows/panel/DevWorkbenchPage/components/EmbeddedTerminal/terminalTheme';
import CallSplitOutlinedIcon from '@mui/icons-material/CallSplitOutlined';
import FormatSizeOutlinedIcon from '@mui/icons-material/FormatSizeOutlined';
import HistoryOutlinedIcon from '@mui/icons-material/HistoryOutlined';
import PaletteOutlinedIcon from '@mui/icons-material/PaletteOutlined';
import PlayArrowOutlinedIcon from '@mui/icons-material/PlayArrowOutlined';
import SensorsOutlinedIcon from '@mui/icons-material/SensorsOutlined';
import TerminalOutlinedIcon from '@mui/icons-material/TerminalOutlined';
import VisibilityOutlinedIcon from '@mui/icons-material/VisibilityOutlined';
import {
  Box,
  Button,
  Divider,
  FormControl,
  FormHelperText,
  MenuItem,
  Select,
  Slider,
  TextField,
  Typography,
} from '@mui/material';
import {
  DEFAULT_ITERM2_SPLIT_DIRECTION,
  DEFAULT_POLL_INTERVAL_SECS,
  DEFAULT_TERMINAL_CURSOR_BLINK,
  DEFAULT_TERMINAL_CURSOR_STYLE,
  DEFAULT_TERMINAL_FONT_SIZE,
  DEFAULT_TERMINAL_LINE_HEIGHT,
  DEFAULT_TERMINAL_POST_OPEN_COMMAND,
  DEFAULT_TERMINAL_SCROLLBACK_ROWS,
  DEFAULT_TERMINAL_STARTUP_CODE_CLI,
  getAppConfig,
  ITERM2_SPLIT_DIRECTION_KEY,
  MAX_POLL_INTERVAL_SECS,
  MIN_POLL_INTERVAL_SECS,
  parseTerminalCursorStyle,
  parseTerminalFontSize,
  parseTerminalLineHeight,
  parseTerminalScrollbackRows,
  parseTerminalStartupCodeCli,
  parseYesNo,
  POLL_INTERVAL_SECS_KEY,
  setAppConfig,
  TERMINAL_CURSOR_BLINK_KEY,
  TERMINAL_CURSOR_STYLE_KEY,
  TERMINAL_CURSOR_STYLE_OPTIONS,
  TERMINAL_FONT_SIZE_KEY,
  TERMINAL_FONT_SIZE_OPTIONS,
  TERMINAL_LINE_HEIGHT_KEY,
  TERMINAL_LINE_HEIGHT_OPTIONS,
  TERMINAL_POST_OPEN_COMMAND_KEY,
  TERMINAL_SCROLLBACK_ROWS_KEY,
  TERMINAL_SCROLLBACK_ROWS_OPTIONS,
  TERMINAL_STARTUP_CODE_CLI_KEY,
  TERMINAL_THEME_KEY,
  YES_NO,
} from '@src/shared/appConfig';
import { iterm2SplitDirectionOptions, terminalStartupCodeCliOptions } from '@src/shared/settingOption';
import { buildTerminalTheme, DEFAULT_TERMINAL_THEME_ID, parseTerminalThemeId, TERMINAL_THEME_IDS } from '@src/windows/panel/DevWorkbenchPage/components/EmbeddedTerminal/terminalTheme';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import TerminalPreview from './TerminalPreview';

// 模块段卡片标题栏：灰底横条（action.hover）+ 图标 + 小节名，两个模块段各一张
// 独立卡片，区隔「编程工具终端监听」与「应用嵌入终端」两组配置。
function SectionHeader({ icon, label }: { icon: React.ReactNode; label: string }) {
  return (
    <Box
      sx={{
        display: 'flex',
        alignItems: 'center',
        gap: 1.5,
        px: 2,
        py: 1.5,
        bgcolor: 'action.hover',
        borderBottom: 1,
        borderColor: 'divider',
      }}
    >
      {icon}
      <Typography variant="body2" sx={{ fontWeight: 600 }}>
        {label}
      </Typography>
    </Box>
  );
}

// 单个模块段卡片容器（标题栏 + 内容）。
function SectionCard({ header, children }: { header: React.ReactNode; children: React.ReactNode }) {
  return (
    <Box sx={{ borderRadius: 2, border: 1, borderColor: 'divider', overflow: 'hidden' }}>
      {header}
      {children}
    </Box>
  );
}

// 主题下拉显示名：i18n 词条（terminal_05 目录常量 key 化）。
function themeDisplayName(id: TerminalThemeId, t: (key: string) => string): string {
  return t(`settings:terminal.option.theme.${id}`);
}

// 终端配置（合并页）：编程工具终端监听（原 MonitorConfigPage 三行）+ 应用嵌入终端
// （启动自动运行 CLI / 字体大小）。draft/saved/dirty 页级一套，五配置共管。
function TerminalConfigPage() {
  const { t } = useTranslation();

  const [savedInterval, setSavedInterval] = useState<number>(DEFAULT_POLL_INTERVAL_SECS);
  const [draftInterval, setDraftInterval] = useState<number>(DEFAULT_POLL_INTERVAL_SECS);
  const [savedSplitDirection, setSavedSplitDirection] = useState<Iterm2SplitDirection>(DEFAULT_ITERM2_SPLIT_DIRECTION);
  const [draftSplitDirection, setDraftSplitDirection] = useState<Iterm2SplitDirection>(DEFAULT_ITERM2_SPLIT_DIRECTION);
  const [savedTerminalPostOpenCommand, setSavedTerminalPostOpenCommand] = useState<string>(DEFAULT_TERMINAL_POST_OPEN_COMMAND);
  const [draftTerminalPostOpenCommand, setDraftTerminalPostOpenCommand] = useState<string>(DEFAULT_TERMINAL_POST_OPEN_COMMAND);
  const [savedStartupCli, setSavedStartupCli] = useState<TerminalStartupCodeCli>(DEFAULT_TERMINAL_STARTUP_CODE_CLI);
  const [draftStartupCli, setDraftStartupCli] = useState<TerminalStartupCodeCli>(DEFAULT_TERMINAL_STARTUP_CODE_CLI);
  const [savedFontSize, setSavedFontSize] = useState<TerminalFontSize>(DEFAULT_TERMINAL_FONT_SIZE);
  const [draftFontSize, setDraftFontSize] = useState<TerminalFontSize>(DEFAULT_TERMINAL_FONT_SIZE);
  const [savedScrollbackRows, setSavedScrollbackRows] = useState<TerminalScrollbackRows>(DEFAULT_TERMINAL_SCROLLBACK_ROWS);
  const [draftScrollbackRows, setDraftScrollbackRows] = useState<TerminalScrollbackRows>(DEFAULT_TERMINAL_SCROLLBACK_ROWS);
  const [savedThemeId, setSavedThemeId] = useState<TerminalThemeId>(DEFAULT_TERMINAL_THEME_ID);
  const [draftThemeId, setDraftThemeId] = useState<TerminalThemeId>(DEFAULT_TERMINAL_THEME_ID);
  const [savedCursorStyle, setSavedCursorStyle] = useState<TerminalCursorStyle>(DEFAULT_TERMINAL_CURSOR_STYLE);
  const [draftCursorStyle, setDraftCursorStyle] = useState<TerminalCursorStyle>(DEFAULT_TERMINAL_CURSOR_STYLE);
  const [savedCursorBlink, setSavedCursorBlink] = useState<YesNo>(DEFAULT_TERMINAL_CURSOR_BLINK);
  const [draftCursorBlink, setDraftCursorBlink] = useState<YesNo>(DEFAULT_TERMINAL_CURSOR_BLINK);
  const [savedLineHeight, setSavedLineHeight] = useState<TerminalLineHeight>(DEFAULT_TERMINAL_LINE_HEIGHT);
  const [draftLineHeight, setDraftLineHeight] = useState<TerminalLineHeight>(DEFAULT_TERMINAL_LINE_HEIGHT);

  useEffect(() => {
    Promise.all([
      getAppConfig(POLL_INTERVAL_SECS_KEY),
      getAppConfig(ITERM2_SPLIT_DIRECTION_KEY),
      getAppConfig(TERMINAL_POST_OPEN_COMMAND_KEY),
      getAppConfig(TERMINAL_STARTUP_CODE_CLI_KEY),
      getAppConfig(TERMINAL_FONT_SIZE_KEY),
      getAppConfig(TERMINAL_SCROLLBACK_ROWS_KEY),
      getAppConfig(TERMINAL_THEME_KEY),
      getAppConfig(TERMINAL_CURSOR_STYLE_KEY),
      getAppConfig(TERMINAL_CURSOR_BLINK_KEY),
      getAppConfig(TERMINAL_LINE_HEIGHT_KEY),
    ]).then(([interval, splitDirection, terminalPostOpenCommand, startupCli, fontSize, scrollbackRows, themeId, cursorStyle, cursorBlink, lineHeight]) => {
      const parsed = interval != null ? Number.parseInt(interval, 10) : Number.NaN;
      if (Number.isFinite(parsed)) {
        // DB 可能存越界或非 step 倍数（直接改 DB / 旧脏数据），clamp 到合法范围。
        const clamped = Math.min(Math.max(parsed, MIN_POLL_INTERVAL_SECS), MAX_POLL_INTERVAL_SECS);
        setSavedInterval(clamped);
        setDraftInterval(clamped);
      }
      if (splitDirection === 'horizontal' || splitDirection === 'vertical' || splitDirection === 'none') {
        setSavedSplitDirection(splitDirection);
        setDraftSplitDirection(splitDirection);
      }
      if (terminalPostOpenCommand != null) {
        setSavedTerminalPostOpenCommand(terminalPostOpenCommand);
        setDraftTerminalPostOpenCommand(terminalPostOpenCommand);
      }
      const cli = parseTerminalStartupCodeCli(startupCli);
      setSavedStartupCli(cli);
      setDraftStartupCli(cli);
      const size = parseTerminalFontSize(fontSize);
      setSavedFontSize(size);
      setDraftFontSize(size);
      const rows = parseTerminalScrollbackRows(scrollbackRows);
      setSavedScrollbackRows(rows);
      setDraftScrollbackRows(rows);
      const theme = parseTerminalThemeId(themeId);
      setSavedThemeId(theme);
      setDraftThemeId(theme);
      const style = parseTerminalCursorStyle(cursorStyle);
      setSavedCursorStyle(style);
      setDraftCursorStyle(style);
      const blink = parseYesNo(cursorBlink, DEFAULT_TERMINAL_CURSOR_BLINK);
      setSavedCursorBlink(blink);
      setDraftCursorBlink(blink);
      const height = parseTerminalLineHeight(lineHeight);
      setSavedLineHeight(height);
      setDraftLineHeight(height);
    });
  }, []);

  const dirty = draftInterval !== savedInterval
    || draftSplitDirection !== savedSplitDirection
    || draftTerminalPostOpenCommand !== savedTerminalPostOpenCommand
    || draftStartupCli !== savedStartupCli
    || draftFontSize !== savedFontSize
    || draftScrollbackRows !== savedScrollbackRows
    || draftThemeId !== savedThemeId
    || draftCursorStyle !== savedCursorStyle
    || draftCursorBlink !== savedCursorBlink
    || draftLineHeight !== savedLineHeight;

  const handleReset = () => {
    setDraftInterval(DEFAULT_POLL_INTERVAL_SECS);
    setDraftSplitDirection(DEFAULT_ITERM2_SPLIT_DIRECTION);
    setDraftTerminalPostOpenCommand(DEFAULT_TERMINAL_POST_OPEN_COMMAND);
    setDraftStartupCli(DEFAULT_TERMINAL_STARTUP_CODE_CLI);
    setDraftFontSize(DEFAULT_TERMINAL_FONT_SIZE);
    setDraftScrollbackRows(DEFAULT_TERMINAL_SCROLLBACK_ROWS);
    setDraftThemeId(DEFAULT_TERMINAL_THEME_ID);
    setDraftCursorStyle(DEFAULT_TERMINAL_CURSOR_STYLE);
    setDraftCursorBlink(DEFAULT_TERMINAL_CURSOR_BLINK);
    setDraftLineHeight(DEFAULT_TERMINAL_LINE_HEIGHT);
  };
  const handleCancel = () => {
    setDraftInterval(savedInterval);
    setDraftSplitDirection(savedSplitDirection);
    setDraftTerminalPostOpenCommand(savedTerminalPostOpenCommand);
    setDraftStartupCli(savedStartupCli);
    setDraftFontSize(savedFontSize);
    setDraftScrollbackRows(savedScrollbackRows);
    setDraftThemeId(savedThemeId);
    setDraftCursorStyle(savedCursorStyle);
    setDraftCursorBlink(savedCursorBlink);
    setDraftLineHeight(savedLineHeight);
  };
  const handleSave = async () => {
    await Promise.all([
      setAppConfig(POLL_INTERVAL_SECS_KEY, String(draftInterval)),
      setAppConfig(ITERM2_SPLIT_DIRECTION_KEY, draftSplitDirection),
      setAppConfig(TERMINAL_POST_OPEN_COMMAND_KEY, draftTerminalPostOpenCommand),
      setAppConfig(TERMINAL_STARTUP_CODE_CLI_KEY, draftStartupCli),
      setAppConfig(TERMINAL_FONT_SIZE_KEY, String(draftFontSize)),
      setAppConfig(TERMINAL_SCROLLBACK_ROWS_KEY, String(draftScrollbackRows)),
      setAppConfig(TERMINAL_THEME_KEY, draftThemeId),
      setAppConfig(TERMINAL_CURSOR_STYLE_KEY, draftCursorStyle),
      setAppConfig(TERMINAL_CURSOR_BLINK_KEY, draftCursorBlink),
      setAppConfig(TERMINAL_LINE_HEIGHT_KEY, String(draftLineHeight)),
    ]);
    setSavedInterval(draftInterval);
    setSavedSplitDirection(draftSplitDirection);
    setSavedTerminalPostOpenCommand(draftTerminalPostOpenCommand);
    setSavedStartupCli(draftStartupCli);
    setSavedFontSize(draftFontSize);
    setSavedScrollbackRows(draftScrollbackRows);
    setSavedThemeId(draftThemeId);
    setSavedCursorStyle(draftCursorStyle);
    setSavedCursorBlink(draftCursorBlink);
    setSavedLineHeight(draftLineHeight);
  };

  const marks = [
    { value: MIN_POLL_INTERVAL_SECS, label: `${MIN_POLL_INTERVAL_SECS}` },
    { value: 30, label: '30' },
    { value: DEFAULT_POLL_INTERVAL_SECS, label: `${DEFAULT_POLL_INTERVAL_SECS}` },
    { value: MAX_POLL_INTERVAL_SECS, label: `${MAX_POLL_INTERVAL_SECS}` },
  ];

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <Box sx={{ p: 3, flex: 1, overflow: 'auto' }}>
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          <SectionCard
            header={(
              <SectionHeader
                icon={<SensorsOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />}
                label={t('settings:terminal.section.monitorListen')}
              />
            )}
          >
            <Box sx={{ px: 2, py: 2 }}>
              <Box
                sx={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  gap: 2,
                }}
              >
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
                  <SensorsOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />
                  <Typography>{t('settings:terminal.row.pollInterval')}</Typography>
                </Box>
                <Typography
                  sx={{ minWidth: 48, textAlign: 'right', fontVariantNumeric: 'tabular-nums' }}
                >
                  {draftInterval}
                  {t('settings:terminal.unit.seconds')}
                </Typography>
              </Box>

              <Slider
                value={draftInterval}
                onChange={(_, v) => setDraftInterval(v as number)}
                min={MIN_POLL_INTERVAL_SECS}
                max={MAX_POLL_INTERVAL_SECS}
                step={5}
                marks={marks}
                sx={{ mt: 1 }}
              />

              <FormHelperText>{t('settings:terminal.help.pollInterval')}</FormHelperText>
            </Box>

            <Divider sx={{ my: 1.5 }} />

            <Box
              sx={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                px: 2,
                py: 1.5,
                gap: 2,
              }}
            >
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
                <CallSplitOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />
                <Typography>{t('settings:terminal.row.iterm2SplitDirection')}</Typography>
              </Box>
              <FormControl size="small" sx={{ minWidth: 140 }}>
                <Select
                  value={draftSplitDirection}
                  onChange={(e: SelectChangeEvent<Iterm2SplitDirection>) =>
                    setDraftSplitDirection(e.target.value as Iterm2SplitDirection)}
                >
                  {iterm2SplitDirectionOptions.map(opt => (
                    <MenuItem key={opt.value} value={opt.value}>
                      {t(opt.labelKey)}
                    </MenuItem>
                  ))}
                </Select>
              </FormControl>
            </Box>

            <Divider sx={{ my: 1.5 }} />

            {/* 追加命令：标签左、输入框右（同一行——短命令右侧放得下，不为它单占一行）。
                卡片末行：上方有 Divider my 叠加，下方贴卡片边缘——pb 补齐同视觉节奏 */}
            <Box
              sx={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                px: 2,
                pt: 1.5,
                pb: 3,
                gap: 2,
              }}
            >
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5, flexShrink: 0 }}>
                <PlayArrowOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />
                <Typography>{t('settings:terminal.row.terminalPostOpenCommand')}</Typography>
              </Box>
              <TextField
                size="small"
                placeholder="add_proxy"
                value={draftTerminalPostOpenCommand}
                onChange={e => setDraftTerminalPostOpenCommand(e.target.value)}
                sx={{ width: 300, flexShrink: 0 }}
              />
            </Box>

          </SectionCard>

          <SectionCard
            header={(
              <SectionHeader
                icon={<TerminalOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />}
                label={t('settings:terminal.section.embeddedTerminal')}
              />
            )}
          >
            {/* 启动自动运行：标签左、下拉右（与其他下拉行同构）。卡片首行：下方有
                Divider my 叠加，上方贴卡片头——pt 补齐同视觉节奏 */}
            <Box
              sx={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                px: 2,
                pt: 3,
                pb: 1.5,
                gap: 2,
              }}
            >
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
                <TerminalOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />
                <Typography>{t('settings:terminal.row.startupCodeCli')}</Typography>
              </Box>
              <Box>
                <FormControl size="small" sx={{ minWidth: 180 }}>
                  <Select
                    value={draftStartupCli}
                    onChange={(e: SelectChangeEvent<TerminalStartupCodeCli>) =>
                      setDraftStartupCli(parseTerminalStartupCodeCli(e.target.value))}
                  >
                    {terminalStartupCodeCliOptions.map(opt => (
                      <MenuItem key={opt.value || 'none'} value={opt.value}>
                        {t(opt.labelKey)}
                      </MenuItem>
                    ))}
                  </Select>
                </FormControl>
              </Box>
            </Box>

            <Divider sx={{ my: 1.5 }} />

            <Box
              sx={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                px: 2,
                py: 1.5,
                gap: 2,
              }}
            >
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
                <FormatSizeOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />
                <Typography>{t('settings:terminal.row.fontSize')}</Typography>
              </Box>
              <FormControl size="small" sx={{ minWidth: 100 }}>
                <Select
                  value={draftFontSize}
                  onChange={(e: SelectChangeEvent<TerminalFontSize>) =>
                    setDraftFontSize(parseTerminalFontSize(String(e.target.value)))}
                >
                  {TERMINAL_FONT_SIZE_OPTIONS.map(size => (
                    <MenuItem key={size} value={size}>{size}</MenuItem>
                  ))}
                </Select>
              </FormControl>
            </Box>

            <Divider sx={{ my: 1.5 }} />

            <Box
              sx={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                px: 2,
                py: 1.5,
                gap: 2,
              }}
            >
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
                <HistoryOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />
                <Typography>{t('settings:terminal.row.scrollbackRows')}</Typography>
              </Box>
              <FormControl size="small" sx={{ minWidth: 120 }}>
                <Select
                  value={draftScrollbackRows}
                  onChange={(e: SelectChangeEvent<TerminalScrollbackRows>) =>
                    setDraftScrollbackRows(parseTerminalScrollbackRows(String(e.target.value)))}
                >
                  {TERMINAL_SCROLLBACK_ROWS_OPTIONS.map(rows => (
                    <MenuItem key={rows} value={rows}>{rows}</MenuItem>
                  ))}
                </Select>
              </FormControl>
            </Box>
            <FormHelperText sx={{ px: 2, pb: 1.5 }}>{t('settings:terminal.help.scrollbackRows')}</FormHelperText>

            <Divider sx={{ my: 1.5 }} />

            {/* 主题（terminal_05）：用户自选暗色主题，不跟随 app 明暗 */}
            <Box
              sx={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                px: 2,
                py: 1.5,
                gap: 2,
              }}
            >
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
                <PaletteOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />
                <Typography>{t('settings:terminal.row.theme')}</Typography>
              </Box>
              <FormControl size="small" sx={{ minWidth: 180 }}>
                <Select
                  value={draftThemeId}
                  onChange={(e: SelectChangeEvent<TerminalThemeId>) =>
                    setDraftThemeId(parseTerminalThemeId(e.target.value))}
                >
                  {TERMINAL_THEME_IDS.map(id => (
                    <MenuItem key={id} value={id}>{themeDisplayName(id, t)}</MenuItem>
                  ))}
                </Select>
              </FormControl>
            </Box>

            <Divider sx={{ my: 1.5 }} />

            {/* 光标样式 + 闪烁（terminal_05） */}
            <Box
              sx={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                px: 2,
                py: 1.5,
                gap: 2,
              }}
            >
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
                <TerminalOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />
                <Typography>{t('settings:terminal.row.cursorStyle')}</Typography>
              </Box>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                <FormControl size="small" sx={{ minWidth: 110 }}>
                  <Select
                    value={draftCursorBlink}
                    onChange={(e: SelectChangeEvent<YesNo>) =>
                      setDraftCursorBlink(parseYesNo(e.target.value, DEFAULT_TERMINAL_CURSOR_BLINK))}
                  >
                    <MenuItem value={YES_NO.YES}>{t('settings:terminal.option.cursorBlinkOn')}</MenuItem>
                    <MenuItem value={YES_NO.NO}>{t('settings:terminal.option.cursorBlinkOff')}</MenuItem>
                  </Select>
                </FormControl>
                <FormControl size="small" sx={{ minWidth: 130 }}>
                  <Select
                    value={draftCursorStyle}
                    onChange={(e: SelectChangeEvent<TerminalCursorStyle>) =>
                      setDraftCursorStyle(parseTerminalCursorStyle(e.target.value))}
                  >
                    {TERMINAL_CURSOR_STYLE_OPTIONS.map(style => (
                      <MenuItem key={style} value={style}>
                        {t(`settings:terminal.option.cursorStyle.${style}`)}
                      </MenuItem>
                    ))}
                  </Select>
                </FormControl>
              </Box>
            </Box>

            <Divider sx={{ my: 1.5 }} />

            {/* 行高（terminal_05） */}
            <Box
              sx={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                px: 2,
                py: 1.5,
                gap: 2,
              }}
            >
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
                <FormatSizeOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />
                <Typography>{t('settings:terminal.row.lineHeight')}</Typography>
              </Box>
              <FormControl size="small" sx={{ minWidth: 110 }}>
                <Select
                  value={draftLineHeight}
                  onChange={(e: SelectChangeEvent<TerminalLineHeight>) =>
                    setDraftLineHeight(parseTerminalLineHeight(String(e.target.value)))}
                >
                  {TERMINAL_LINE_HEIGHT_OPTIONS.map(height => (
                    <MenuItem key={height} value={height}>{height.toFixed(1)}</MenuItem>
                  ))}
                </Select>
              </FormControl>
            </Box>

            <Divider sx={{ my: 1.5 }} />

            {/* 实时预览（terminal_05）：36x15 真 xterm，随 draft 值变（未保存即所见）。
                行结构与其他设置项同构（标题行 + 内容），预览方块在标题下方。 */}
            <Box sx={{ px: 2, py: 1.5 }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
                <VisibilityOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />
                <Typography>{t('settings:terminal.row.preview')}</Typography>
              </Box>
              <Box sx={{ mt: 1, mb: 1, display: 'flex', justifyContent: 'center' }}>
                <TerminalPreview
                  theme={buildTerminalTheme(draftThemeId)}
                  fontSize={draftFontSize}
                  cursorStyle={draftCursorStyle}
                  cursorBlink={draftCursorBlink === YES_NO.YES}
                  lineHeight={draftLineHeight}
                />
              </Box>
            </Box>
          </SectionCard>
        </Box>
      </Box>

      <Box
        sx={{
          p: 2,
          borderTop: 1,
          borderColor: 'divider',
          display: 'flex',
          justifyContent: 'flex-end',
          gap: 1,
        }}
      >
        <Button onClick={handleReset} color="inherit">
          {t('settings:common.button.reset')}
        </Button>
        <Button onClick={handleCancel} disabled={!dirty} color="inherit">
          {t('settings:common.button.cancel')}
        </Button>
        <Button onClick={handleSave} disabled={!dirty} variant="contained">
          {t('settings:common.button.save')}
        </Button>
      </Box>
    </Box>
  );
}

export default TerminalConfigPage;
