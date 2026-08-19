import type { SelectChangeEvent } from '@mui/material/Select';
import type { Iterm2SplitDirection, TerminalStartupCodeCli } from '@src/shared/appConfig';
import CallSplitOutlinedIcon from '@mui/icons-material/CallSplitOutlined';
import PlayArrowOutlinedIcon from '@mui/icons-material/PlayArrowOutlined';
import SensorsOutlinedIcon from '@mui/icons-material/SensorsOutlined';
import TerminalOutlinedIcon from '@mui/icons-material/TerminalOutlined';
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
  DEFAULT_TERMINAL_POST_OPEN_COMMAND,
  DEFAULT_TERMINAL_STARTUP_CODE_CLI,
  getAppConfig,
  ITERM2_SPLIT_DIRECTION_KEY,
  MAX_POLL_INTERVAL_SECS,
  MIN_POLL_INTERVAL_SECS,
  parseTerminalStartupCodeCli,
  POLL_INTERVAL_SECS_KEY,
  setAppConfig,
  TERMINAL_POST_OPEN_COMMAND_KEY,
  TERMINAL_STARTUP_CODE_CLI_KEY,
} from '@src/shared/appConfig';
import { iterm2SplitDirectionOptions, terminalStartupCodeCliOptions } from '@src/shared/settingOption';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';

// 模块段卡片标题栏：灰底横条（action.hover）+ 图标 + 小节名，两个模块段各一张
// 独立卡片，区隔「编程工具终端监听」与「应用嵌入终端」两组配置。
// 字体大小等模块 3 再入「应用嵌入终端」卡片。
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

// 终端配置（合并页）：编程工具终端监听（原 MonitorConfigPage 三行）+ 应用嵌入终端
// （启动自动运行 CLI）。draft/saved/dirty 页级一套，四配置共管。
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

  useEffect(() => {
    Promise.all([
      getAppConfig(POLL_INTERVAL_SECS_KEY),
      getAppConfig(ITERM2_SPLIT_DIRECTION_KEY),
      getAppConfig(TERMINAL_POST_OPEN_COMMAND_KEY),
      getAppConfig(TERMINAL_STARTUP_CODE_CLI_KEY),
    ]).then(([interval, splitDirection, terminalPostOpenCommand, startupCli]) => {
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
    });
  }, []);

  const dirty = draftInterval !== savedInterval
    || draftSplitDirection !== savedSplitDirection
    || draftTerminalPostOpenCommand !== savedTerminalPostOpenCommand
    || draftStartupCli !== savedStartupCli;

  const handleReset = () => {
    setDraftInterval(DEFAULT_POLL_INTERVAL_SECS);
    setDraftSplitDirection(DEFAULT_ITERM2_SPLIT_DIRECTION);
    setDraftTerminalPostOpenCommand(DEFAULT_TERMINAL_POST_OPEN_COMMAND);
    setDraftStartupCli(DEFAULT_TERMINAL_STARTUP_CODE_CLI);
  };
  const handleCancel = () => {
    setDraftInterval(savedInterval);
    setDraftSplitDirection(savedSplitDirection);
    setDraftTerminalPostOpenCommand(savedTerminalPostOpenCommand);
    setDraftStartupCli(savedStartupCli);
  };
  const handleSave = async () => {
    await Promise.all([
      setAppConfig(POLL_INTERVAL_SECS_KEY, String(draftInterval)),
      setAppConfig(ITERM2_SPLIT_DIRECTION_KEY, draftSplitDirection),
      setAppConfig(TERMINAL_POST_OPEN_COMMAND_KEY, draftTerminalPostOpenCommand),
      setAppConfig(TERMINAL_STARTUP_CODE_CLI_KEY, draftStartupCli),
    ]);
    setSavedInterval(draftInterval);
    setSavedSplitDirection(draftSplitDirection);
    setSavedTerminalPostOpenCommand(draftTerminalPostOpenCommand);
    setSavedStartupCli(draftStartupCli);
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

            <Box sx={{ px: 2, py: 1.5 }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
                <PlayArrowOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />
                <Typography>{t('settings:terminal.row.terminalPostOpenCommand')}</Typography>
              </Box>
              <TextField
                size="small"
                fullWidth
                placeholder="add_proxy"
                value={draftTerminalPostOpenCommand}
                onChange={e => setDraftTerminalPostOpenCommand(e.target.value)}
                sx={{ mt: 1, mb: 1 }}
              />
              <FormHelperText>{t('settings:terminal.help.terminalPostOpenCommand')}</FormHelperText>
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
            <Box sx={{ px: 2, py: 2 }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
                <TerminalOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />
                <Typography>{t('settings:terminal.row.startupCodeCli')}</Typography>
              </Box>
              <FormControl size="small" sx={{ minWidth: 180, mt: 1, mb: 1 }}>
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
              <FormHelperText>{t('settings:terminal.help.startupCodeCli')}</FormHelperText>
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
