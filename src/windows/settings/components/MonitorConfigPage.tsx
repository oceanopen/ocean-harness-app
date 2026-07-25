import type { SelectChangeEvent } from '@mui/material/Select';
import type { Iterm2SplitDirection } from '@src/shared/app_config';
import CallSplitOutlinedIcon from '@mui/icons-material/CallSplitOutlined';
import PlayArrowOutlinedIcon from '@mui/icons-material/PlayArrowOutlined';
import SensorsOutlinedIcon from '@mui/icons-material/SensorsOutlined';
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
  getAppConfig,
  ITERM2_SPLIT_DIRECTION_KEY,
  MAX_POLL_INTERVAL_SECS,
  MIN_POLL_INTERVAL_SECS,
  POLL_INTERVAL_SECS_KEY,
  setAppConfig,
  TERMINAL_POST_OPEN_COMMAND_KEY,
} from '@src/shared/app_config';
import { iterm2SplitDirectionOptions } from '@src/shared/settingOption';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';

function MonitorConfigPage() {
  const { t } = useTranslation();

  const [savedInterval, setSavedInterval] = useState<number>(DEFAULT_POLL_INTERVAL_SECS);
  const [draftInterval, setDraftInterval] = useState<number>(DEFAULT_POLL_INTERVAL_SECS);
  const [savedSplitDirection, setSavedSplitDirection] = useState<Iterm2SplitDirection>(DEFAULT_ITERM2_SPLIT_DIRECTION);
  const [draftSplitDirection, setDraftSplitDirection] = useState<Iterm2SplitDirection>(DEFAULT_ITERM2_SPLIT_DIRECTION);
  const [savedTerminalPostOpenCommand, setSavedTerminalPostOpenCommand] = useState<string>(DEFAULT_TERMINAL_POST_OPEN_COMMAND);
  const [draftTerminalPostOpenCommand, setDraftTerminalPostOpenCommand] = useState<string>(DEFAULT_TERMINAL_POST_OPEN_COMMAND);

  useEffect(() => {
    Promise.all([
      getAppConfig(POLL_INTERVAL_SECS_KEY),
      getAppConfig(ITERM2_SPLIT_DIRECTION_KEY),
      getAppConfig(TERMINAL_POST_OPEN_COMMAND_KEY),
    ]).then(([interval, splitDirection, terminalPostOpenCommand]) => {
      const parsed = interval != null ? Number.parseInt(interval, 10) : Number.NaN;
      if (Number.isFinite(parsed)) {
        // DB 可能存越界或非 step 倍数（直接改 DB / 旧脏数据），clamp 到合法范围。
        const clamped = Math.min(Math.max(parsed, MIN_POLL_INTERVAL_SECS), MAX_POLL_INTERVAL_SECS);
        setSavedInterval(clamped);
        setDraftInterval(clamped);
      }
      if (splitDirection === 'horizontal' || splitDirection === 'vertical') {
        setSavedSplitDirection(splitDirection);
        setDraftSplitDirection(splitDirection);
      }
      if (terminalPostOpenCommand != null) {
        setSavedTerminalPostOpenCommand(terminalPostOpenCommand);
        setDraftTerminalPostOpenCommand(terminalPostOpenCommand);
      }
    });
  }, []);

  const dirty = draftInterval !== savedInterval || draftSplitDirection !== savedSplitDirection || draftTerminalPostOpenCommand !== savedTerminalPostOpenCommand;

  const handleReset = () => {
    setDraftInterval(DEFAULT_POLL_INTERVAL_SECS);
    setDraftSplitDirection(DEFAULT_ITERM2_SPLIT_DIRECTION);
    setDraftTerminalPostOpenCommand(DEFAULT_TERMINAL_POST_OPEN_COMMAND);
  };
  const handleCancel = () => {
    setDraftInterval(savedInterval);
    setDraftSplitDirection(savedSplitDirection);
    setDraftTerminalPostOpenCommand(savedTerminalPostOpenCommand);
  };
  const handleSave = async () => {
    await Promise.all([
      setAppConfig(POLL_INTERVAL_SECS_KEY, String(draftInterval)),
      setAppConfig(ITERM2_SPLIT_DIRECTION_KEY, draftSplitDirection),
      setAppConfig(TERMINAL_POST_OPEN_COMMAND_KEY, draftTerminalPostOpenCommand),
    ]);
    setSavedInterval(draftInterval);
    setSavedSplitDirection(draftSplitDirection);
    setSavedTerminalPostOpenCommand(draftTerminalPostOpenCommand);
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
        <Box sx={{ borderRadius: 2, border: 1, borderColor: 'divider', overflow: 'hidden' }}>
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
                <Typography>{t('settings:row.pollInterval')}</Typography>
              </Box>
              <Typography
                sx={{ minWidth: 48, textAlign: 'right', fontVariantNumeric: 'tabular-nums' }}
              >
                {draftInterval}
                {t('settings:unit.seconds')}
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

            <FormHelperText>{t('settings:help.pollInterval')}</FormHelperText>
          </Box>

          <Divider />

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
              <Typography>{t('settings:row.iterm2SplitDirection')}</Typography>
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

          <Divider />

          <Box sx={{ px: 2, py: 2 }}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
              <PlayArrowOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />
              <Typography>{t('settings:row.terminalPostOpenCommand')}</Typography>
            </Box>
            <TextField
              size="small"
              fullWidth
              placeholder="add_proxy"
              value={draftTerminalPostOpenCommand}
              onChange={e => setDraftTerminalPostOpenCommand(e.target.value)}
              sx={{ mt: 1, mb: 1 }}
            />
            <FormHelperText>{t('settings:help.terminalPostOpenCommand')}</FormHelperText>
          </Box>
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
          {t('settings:button.reset')}
        </Button>
        <Button onClick={handleCancel} disabled={!dirty} color="inherit">
          {t('settings:button.cancel')}
        </Button>
        <Button onClick={handleSave} disabled={!dirty} variant="contained">
          {t('settings:button.save')}
        </Button>
      </Box>
    </Box>
  );
}

export default MonitorConfigPage;
