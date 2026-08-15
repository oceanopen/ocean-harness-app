import FolderOpenIcon from '@mui/icons-material/FolderOpen';
import FolderOpenOutlinedIcon from '@mui/icons-material/FolderOpenOutlined';
import {
  Box,
  Button,
  IconButton,
  InputAdornment,
  TextField,
  Typography,
} from '@mui/material';
import {
  DEFAULT_WORKSPACE_BASE_DIR,
  getAppConfig,
  setAppConfig,
  WORKSPACE_BASE_DIR_KEY,
} from '@src/shared/appConfig';
import { open as openDialog } from '@tauri-apps/plugin-dialog';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';

function ProjectConfigPage() {
  const { t } = useTranslation();

  const [savedBaseDir, setSavedBaseDir] = useState(DEFAULT_WORKSPACE_BASE_DIR);
  const [draftBaseDir, setDraftBaseDir] = useState(DEFAULT_WORKSPACE_BASE_DIR);

  useEffect(() => {
    getAppConfig(WORKSPACE_BASE_DIR_KEY).then((dir) => {
      const value = dir ?? DEFAULT_WORKSPACE_BASE_DIR;
      setSavedBaseDir(value);
      setDraftBaseDir(value);
    });
  }, []);

  const dirty = draftBaseDir !== savedBaseDir;

  const handleBrowse = async () => {
    // directory: true 多选关闭，返回 string | null。
    const selected = await openDialog({ directory: true, multiple: false });
    if (typeof selected === 'string') {
      setDraftBaseDir(selected);
    }
  };

  const handleReset = () => {
    setDraftBaseDir(DEFAULT_WORKSPACE_BASE_DIR);
  };
  const handleCancel = () => {
    setDraftBaseDir(savedBaseDir);
  };
  const handleSave = async () => {
    await setAppConfig(WORKSPACE_BASE_DIR_KEY, draftBaseDir);
    setSavedBaseDir(draftBaseDir);
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
              <FolderOpenOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />
              <Typography>{t('settings:row.workspaceBaseDir')}</Typography>
            </Box>
            <TextField
              size="small"
              sx={{ width: 360 }}
              placeholder={t('settings:placeholder.workspaceBaseDir')}
              value={draftBaseDir}
              onChange={(e) => {
                setDraftBaseDir(e.target.value);
              }}
              slotProps={{
                input: {
                  sx: { paddingRight: 1 },
                  endAdornment: (
                    <InputAdornment position="end">
                      <IconButton
                        size="small"
                        onClick={handleBrowse}
                        aria-label={t('settings:row.workspaceBaseDir')}
                      >
                        <FolderOpenIcon fontSize="small" />
                      </IconButton>
                    </InputAdornment>
                  ),
                },
              }}
            />
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

export default ProjectConfigPage;
