import type { SelectChangeEvent } from '@mui/material/Select';
import type { AcpSessionIdleTimeoutMinutes, Appearance, Language } from '@src/shared/appConfig';
import LanguageIcon from '@mui/icons-material/Language';
import PaletteOutlinedIcon from '@mui/icons-material/PaletteOutlined';
import TimerOutlinedIcon from '@mui/icons-material/TimerOutlined';
import {
  Box,
  Button,
  Divider,
  FormControl,
  MenuItem,
  Select,
  Typography,
} from '@mui/material';
import {
  ACP_SESSION_IDLE_TIMEOUT_MINUTES_KEY,
  APPEARANCE_KEY,
  DEFAULT_ACP_SESSION_IDLE_TIMEOUT_MINUTES,
  DEFAULT_APPEARANCE,
  DEFAULT_LANGUAGE,
  getAppConfig,
  LANGUAGE_KEY,
  parseAcpSessionIdleTimeoutMinutes,
  setAppConfig,
} from '@src/shared/appConfig';
import {
  acpSessionIdleTimeoutOptions,
  appearanceOptions,
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

  useEffect(() => {
    Promise.all([
      getAppConfig(LANGUAGE_KEY),
      getAppConfig(APPEARANCE_KEY),
      getAppConfig(ACP_SESSION_IDLE_TIMEOUT_MINUTES_KEY),
    ]).then(([lang, appearance, idleMinutes]) => {
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
    });
  }, []);

  const dirty
    = draftLanguage !== savedLanguage
      || draftAppearance !== savedAppearance
      || draftAcpIdle !== savedAcpIdle;

  const handleReset = () => {
    setDraftLanguage(DEFAULT_LANGUAGE);
    setDraftAppearance(DEFAULT_APPEARANCE);
    setDraftAcpIdle(DEFAULT_ACP_SESSION_IDLE_TIMEOUT_MINUTES);
  };
  const handleCancel = () => {
    setDraftLanguage(savedLanguage);
    setDraftAppearance(savedAppearance);
    setDraftAcpIdle(savedAcpIdle);
  };
  const handleSave = async () => {
    await Promise.all([
      setAppConfig(LANGUAGE_KEY, draftLanguage),
      setAppConfig(APPEARANCE_KEY, draftAppearance),
      setAppConfig(ACP_SESSION_IDLE_TIMEOUT_MINUTES_KEY, String(draftAcpIdle)),
    ]);
    setSavedLanguage(draftLanguage);
    setSavedAppearance(draftAppearance);
    setSavedAcpIdle(draftAcpIdle);
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
