import type { SelectChangeEvent } from '@mui/material/Select';
import type { Appearance, Language } from '@src/shared/appConfig';
import LanguageIcon from '@mui/icons-material/Language';
import PaletteOutlinedIcon from '@mui/icons-material/PaletteOutlined';
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
  APPEARANCE_KEY,
  DEFAULT_APPEARANCE,
  DEFAULT_LANGUAGE,
  getAppConfig,
  LANGUAGE_KEY,
  setAppConfig,
} from '@src/shared/appConfig';
import {
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

  useEffect(() => {
    Promise.all([
      getAppConfig(LANGUAGE_KEY),
      getAppConfig(APPEARANCE_KEY),
    ]).then(([lang, appearance]) => {
      if (lang === 'system' || lang === 'zh-CN' || lang === 'en') {
        setSavedLanguage(lang);
        setDraftLanguage(lang);
      }
      if (appearance === 'system' || appearance === 'light' || appearance === 'dark') {
        setSavedAppearance(appearance);
        setDraftAppearance(appearance);
      }
    });
  }, []);

  const dirty
    = draftLanguage !== savedLanguage || draftAppearance !== savedAppearance;

  const handleReset = () => {
    setDraftLanguage(DEFAULT_LANGUAGE);
    setDraftAppearance(DEFAULT_APPEARANCE);
  };
  const handleCancel = () => {
    setDraftLanguage(savedLanguage);
    setDraftAppearance(savedAppearance);
  };
  const handleSave = async () => {
    await Promise.all([
      setAppConfig(LANGUAGE_KEY, draftLanguage),
      setAppConfig(APPEARANCE_KEY, draftAppearance),
    ]);
    setSavedLanguage(draftLanguage);
    setSavedAppearance(draftAppearance);
  };

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <Box sx={{ p: 3, flex: 1, overflow: 'auto' }}>
        <Box
          sx={{
            borderRadius: 2,
            border: 1,
            borderColor: 'divider',
            overflow: 'hidden',
          }}
        >
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
              <LanguageIcon fontSize="small" sx={{ color: 'text.secondary' }} />
              <Typography>{t('settings:row.language')}</Typography>
            </Box>
            <FormControl size="small" sx={{ minWidth: 140 }}>
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
              px: 2,
              py: 1.5,
              gap: 2,
            }}
          >
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
              <PaletteOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />
              <Typography>{t('settings:row.appearance')}</Typography>
            </Box>
            <FormControl size="small" sx={{ minWidth: 140 }}>
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

export default AppConfigPage;
