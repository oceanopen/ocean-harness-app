import type { SelectChangeEvent } from '@mui/material/Select';
import type { AcpSessionIdleTimeoutMinutes, Appearance, BrowserRefreshMs, Language } from '@src/shared/appConfig';
import type { YesNo } from '@src/shared/bindings';
import LanguageIcon from '@mui/icons-material/Language';
import PaletteOutlinedIcon from '@mui/icons-material/PaletteOutlined';
import PictureInPictureAltOutlinedIcon from '@mui/icons-material/PictureInPictureAltOutlined';
import RocketLaunchOutlinedIcon from '@mui/icons-material/RocketLaunchOutlined';
import SpeedOutlinedIcon from '@mui/icons-material/SpeedOutlined';
import TimerOutlinedIcon from '@mui/icons-material/TimerOutlined';
import {
  Box,
  Button,
  Divider,
  FormControl,
  MenuItem,
  Select,
  Switch,
  Typography,
} from '@mui/material';
import {
  ACP_SESSION_IDLE_TIMEOUT_MINUTES_KEY,
  APPEARANCE_KEY,
  BROWSER_LIVE_REFRESH_MS_KEY,
  BROWSER_PREVIEW_REFRESH_MS_KEY,
  DEFAULT_ACP_SESSION_IDLE_TIMEOUT_MINUTES,
  DEFAULT_APPEARANCE,
  DEFAULT_BROWSER_LIVE_REFRESH_MS,
  DEFAULT_BROWSER_PREVIEW_REFRESH_MS,
  DEFAULT_LANGUAGE,
  DEFAULT_LAUNCH_AT_LOGIN,
  getAppConfig,
  LANGUAGE_KEY,
  LAUNCH_AT_LOGIN_KEY,
  parseAcpSessionIdleTimeoutMinutes,
  parseBrowserRefreshMs,
  parseYesNo,
  setAppConfig,
  toYesNo,
  YES_NO,
} from '@src/shared/appConfig';
import {
  acpSessionIdleTimeoutOptions,
  appearanceOptions,
  browserRefreshMsOptions,
  languageOptions,
} from '@src/shared/settingOption';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';

function AppConfigPage() {
  const { t } = useTranslation();

  const [savedLanguage, setSavedLanguage] = useState<Language>(DEFAULT_LANGUAGE);
  const [draftLanguage, setDraftLanguage] = useState<Language>(DEFAULT_LANGUAGE);
  const [savedAppearance, setSavedAppearance] = useState<Appearance>(DEFAULT_APPEARANCE);
  const [draftAppearance, setDraftAppearance] = useState<Appearance>(DEFAULT_APPEARANCE);
  const [savedAcpIdle, setSavedAcpIdle] = useState<AcpSessionIdleTimeoutMinutes>(DEFAULT_ACP_SESSION_IDLE_TIMEOUT_MINUTES);
  const [draftAcpIdle, setDraftAcpIdle] = useState<AcpSessionIdleTimeoutMinutes>(DEFAULT_ACP_SESSION_IDLE_TIMEOUT_MINUTES);
  const [savedLiveRefresh, setSavedLiveRefresh] = useState<BrowserRefreshMs>(DEFAULT_BROWSER_LIVE_REFRESH_MS);
  const [draftLiveRefresh, setDraftLiveRefresh] = useState<BrowserRefreshMs>(DEFAULT_BROWSER_LIVE_REFRESH_MS);
  const [savedPreviewRefresh, setSavedPreviewRefresh] = useState<BrowserRefreshMs>(DEFAULT_BROWSER_PREVIEW_REFRESH_MS);
  const [draftPreviewRefresh, setDraftPreviewRefresh] = useState<BrowserRefreshMs>(DEFAULT_BROWSER_PREVIEW_REFRESH_MS);
  const [savedLaunchAtLogin, setSavedLaunchAtLogin] = useState<YesNo>(DEFAULT_LAUNCH_AT_LOGIN);
  const [draftLaunchAtLogin, setDraftLaunchAtLogin] = useState<YesNo>(DEFAULT_LAUNCH_AT_LOGIN);

  useEffect(() => {
    Promise.all([
      getAppConfig(LANGUAGE_KEY),
      getAppConfig(APPEARANCE_KEY),
      getAppConfig(ACP_SESSION_IDLE_TIMEOUT_MINUTES_KEY),
      getAppConfig(BROWSER_LIVE_REFRESH_MS_KEY),
      getAppConfig(BROWSER_PREVIEW_REFRESH_MS_KEY),
      getAppConfig(LAUNCH_AT_LOGIN_KEY),
    ]).then(([lang, appearance, idleMinutes, liveRefresh, previewRefresh, launchAtLogin]) => {
      if (lang === 'system' || lang === 'zh-CN' || lang === 'en') {
        setSavedLanguage(lang);
        setDraftLanguage(lang);
      }
      if (appearance === 'system' || appearance === 'light' || appearance === 'dark') {
        setSavedAppearance(appearance);
        setDraftAppearance(appearance);
      }
      const acpIdle = parseAcpSessionIdleTimeoutMinutes(idleMinutes);
      setSavedAcpIdle(acpIdle);
      setDraftAcpIdle(acpIdle);
      const live = parseBrowserRefreshMs(liveRefresh, DEFAULT_BROWSER_LIVE_REFRESH_MS);
      setSavedLiveRefresh(live);
      setDraftLiveRefresh(live);
      const preview = parseBrowserRefreshMs(previewRefresh, DEFAULT_BROWSER_PREVIEW_REFRESH_MS);
      setSavedPreviewRefresh(preview);
      setDraftPreviewRefresh(preview);
      const launch = parseYesNo(launchAtLogin, DEFAULT_LAUNCH_AT_LOGIN);
      setSavedLaunchAtLogin(launch);
      setDraftLaunchAtLogin(launch);
    });
  }, []);

  const dirty
    = draftLanguage !== savedLanguage
      || draftAppearance !== savedAppearance
      || draftAcpIdle !== savedAcpIdle
      || draftLiveRefresh !== savedLiveRefresh
      || draftPreviewRefresh !== savedPreviewRefresh
      || draftLaunchAtLogin !== savedLaunchAtLogin;

  const handleReset = () => {
    setDraftLanguage(DEFAULT_LANGUAGE);
    setDraftAppearance(DEFAULT_APPEARANCE);
    setDraftAcpIdle(DEFAULT_ACP_SESSION_IDLE_TIMEOUT_MINUTES);
    setDraftLiveRefresh(DEFAULT_BROWSER_LIVE_REFRESH_MS);
    setDraftPreviewRefresh(DEFAULT_BROWSER_PREVIEW_REFRESH_MS);
    setDraftLaunchAtLogin(DEFAULT_LAUNCH_AT_LOGIN);
  };
  const handleCancel = () => {
    setDraftLanguage(savedLanguage);
    setDraftAppearance(savedAppearance);
    setDraftAcpIdle(savedAcpIdle);
    setDraftLiveRefresh(savedLiveRefresh);
    setDraftPreviewRefresh(savedPreviewRefresh);
    setDraftLaunchAtLogin(savedLaunchAtLogin);
  };
  const handleSave = async () => {
    await Promise.all([
      setAppConfig(LANGUAGE_KEY, draftLanguage),
      setAppConfig(APPEARANCE_KEY, draftAppearance),
      setAppConfig(ACP_SESSION_IDLE_TIMEOUT_MINUTES_KEY, String(draftAcpIdle)),
      setAppConfig(BROWSER_LIVE_REFRESH_MS_KEY, String(draftLiveRefresh)),
      setAppConfig(BROWSER_PREVIEW_REFRESH_MS_KEY, String(draftPreviewRefresh)),
      setAppConfig(LAUNCH_AT_LOGIN_KEY, draftLaunchAtLogin),
    ]);
    setSavedLanguage(draftLanguage);
    setSavedAppearance(draftAppearance);
    setSavedAcpIdle(draftAcpIdle);
    setSavedLiveRefresh(draftLiveRefresh);
    setSavedPreviewRefresh(draftPreviewRefresh);
    setSavedLaunchAtLogin(draftLaunchAtLogin);
  };

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <Box sx={{ p: '24px', flex: 1, overflow: 'auto' }}>
        <Box
          sx={{
            borderRadius: '8px',
            border: '1px solid',
            borderColor: 'divider',
            overflow: 'hidden',
          }}
        >
          <Box
            sx={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              px: '16px',
              py: '12px',
              gap: '16px',
            }}
          >
            <Box sx={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
              <LanguageIcon fontSize="small" sx={{ color: 'text.secondary' }} />
              <Typography>{t('settings:app.row.language')}</Typography>
            </Box>
            <FormControl size="small" sx={{ minWidth: '140px' }}>
              <Select
                value={draftLanguage}
                onChange={(e: SelectChangeEvent<Language>) =>
                  setDraftLanguage(e.target.value as Language)}
              >
                {languageOptions.map(opt => (
                  <MenuItem key={opt.value} value={opt.value}>
                    {t(opt.labelKey)}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>
          </Box>

          <Divider />

          <Box
            sx={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              px: '16px',
              py: '12px',
              gap: '16px',
            }}
          >
            <Box sx={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
              <PaletteOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />
              <Typography>{t('settings:app.row.appearance')}</Typography>
            </Box>
            <FormControl size="small" sx={{ minWidth: '140px' }}>
              <Select
                value={draftAppearance}
                onChange={(e: SelectChangeEvent<Appearance>) =>
                  setDraftAppearance(e.target.value as Appearance)}
              >
                {appearanceOptions.map(opt => (
                  <MenuItem key={opt.value} value={opt.value}>
                    {t(opt.labelKey)}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>
          </Box>

          <Divider />

          <Box
            sx={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              px: '16px',
              py: '12px',
              gap: '16px',
            }}
          >
            <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: '12px' }}>
              <TimerOutlinedIcon fontSize="small" sx={{ color: 'text.secondary', mt: '4px' }} />
              <Box>
                <Typography>{t('settings:app.row.acpIdleTimeout')}</Typography>
                <Typography variant="caption" color="text.secondary">
                  {t('settings:app.help.acpIdleTimeout')}
                </Typography>
              </Box>
            </Box>
            <FormControl size="small" sx={{ minWidth: '140px' }}>
              <Select
                value={draftAcpIdle}
                onChange={(e: SelectChangeEvent<AcpSessionIdleTimeoutMinutes>) =>
                  setDraftAcpIdle(e.target.value as AcpSessionIdleTimeoutMinutes)}
              >
                {acpSessionIdleTimeoutOptions.map(opt => (
                  <MenuItem key={opt.value} value={opt.value}>
                    {t(opt.labelKey, { minutes: opt.value })}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>
          </Box>

          <Divider />

          <Box
            sx={{
              display: 'flex',
              alignItems: 'flex-start',
              justifyContent: 'space-between',
              px: '16px',
              py: '12px',
              gap: '16px',
            }}
          >
            <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: '12px' }}>
              <SpeedOutlinedIcon fontSize="small" sx={{ color: 'text.secondary', mt: '4px' }} />
              <Box>
                <Typography>{t('settings:app.row.browserLiveRefresh')}</Typography>
                <Typography variant="caption" color="text.secondary">
                  {t('settings:app.help.browserRefresh')}
                </Typography>
              </Box>
            </Box>
            <FormControl size="small" sx={{ minWidth: '140px' }}>
              <Select
                value={draftLiveRefresh}
                onChange={(e: SelectChangeEvent<BrowserRefreshMs>) =>
                  setDraftLiveRefresh(e.target.value as BrowserRefreshMs)}
              >
                {browserRefreshMsOptions.map(opt => (
                  <MenuItem key={opt.value} value={opt.value}>
                    {t(opt.labelKey, { seconds: opt.value / 1000 })}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>
          </Box>

          <Divider />

          <Box
            sx={{
              display: 'flex',
              alignItems: 'flex-start',
              justifyContent: 'space-between',
              px: '16px',
              py: '12px',
              gap: '16px',
            }}
          >
            <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: '12px' }}>
              <PictureInPictureAltOutlinedIcon fontSize="small" sx={{ color: 'text.secondary', mt: '4px' }} />
              <Box>
                <Typography>{t('settings:app.row.browserPreviewRefresh')}</Typography>
                <Typography variant="caption" color="text.secondary">
                  {t('settings:app.help.browserRefresh')}
                </Typography>
              </Box>
            </Box>
            <FormControl size="small" sx={{ minWidth: '140px' }}>
              <Select
                value={draftPreviewRefresh}
                onChange={(e: SelectChangeEvent<BrowserRefreshMs>) =>
                  setDraftPreviewRefresh(e.target.value as BrowserRefreshMs)}
              >
                {browserRefreshMsOptions.map(opt => (
                  <MenuItem key={opt.value} value={opt.value}>
                    {t(opt.labelKey, { seconds: opt.value / 1000 })}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>
          </Box>

          <Divider />

          <Box
            sx={{
              display: 'flex',
              alignItems: 'flex-start',
              justifyContent: 'space-between',
              px: '16px',
              py: '12px',
              gap: '16px',
            }}
          >
            <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: '12px' }}>
              <RocketLaunchOutlinedIcon fontSize="small" sx={{ color: 'text.secondary', mt: '4px' }} />
              <Box>
                <Typography>{t('settings:app.row.launchAtLogin')}</Typography>
                <Typography variant="caption" color="text.secondary">
                  {t('settings:app.help.launchAtLogin')}
                </Typography>
              </Box>
            </Box>
            <Switch
              size="small"
              checked={draftLaunchAtLogin === YES_NO.YES}
              onChange={e => setDraftLaunchAtLogin(toYesNo(e.target.checked))}
            />
          </Box>
        </Box>
      </Box>

      <Box
        sx={{
          p: '16px',
          borderTop: '1px solid',
          borderColor: 'divider',
          display: 'flex',
          justifyContent: 'flex-end',
          gap: '8px',
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

export default AppConfigPage;
