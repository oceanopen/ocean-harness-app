import HttpOutlinedIcon from '@mui/icons-material/HttpOutlined';
import {
  Box,
  Button,
  FormHelperText,
  TextField,
  Typography,
} from '@mui/material';
import {
  defaultHttpServerPort,
  getAppConfig,
  HTTP_SERVER_PORT_KEY,
  MAX_HTTP_SERVER_PORT,
  MIN_HTTP_SERVER_PORT,
  setAppConfig,
} from '@src/shared/appConfig';
import { commands } from '@src/shared/bindings';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';

// 校验草稿：空串合法（=用默认）；非空须为纯数字且落在 [MIN,MAX]。
function isPortInvalid(value: string): boolean {
  const v = value.trim();
  if (v === '') {
    return false;
  }
  if (!/^\d+$/.test(v)) {
    return true;
  }
  const n = Number.parseInt(v, 10);
  return n < MIN_HTTP_SERVER_PORT || n > MAX_HTTP_SERVER_PORT;
}

function ServiceConfigPage() {
  const { t } = useTranslation();

  // 端口以字符串草稿管理（空串=用默认）；mount 读已存值，非法则视为未设置。
  const [savedPort, setSavedPort] = useState<string>('');
  const [draftPort, setDraftPort] = useState<string>('');
  // 当前运行时默认端口（帮助文案展示具体值）：读 http_server 的 mode 本地映射。
  const [defaultPort, setDefaultPort] = useState<number>();

  useEffect(() => {
    getAppConfig(HTTP_SERVER_PORT_KEY).then((raw) => {
      const v = raw?.trim() ?? '';
      if (v !== '' && !isPortInvalid(v)) {
        setSavedPort(v);
        setDraftPort(v);
      }
    });
    commands.httpServerStatus().then(s => setDefaultPort(defaultHttpServerPort(s.mode)));
  }, []);

  const invalid = isPortInvalid(draftPort);
  const dirty = draftPort.trim() !== savedPort;

  const handleReset = () => setDraftPort('');
  const handleCancel = () => setDraftPort(savedPort);
  const handleSave = async () => {
    await setAppConfig(HTTP_SERVER_PORT_KEY, draftPort.trim());
    setSavedPort(draftPort.trim());
  };

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <Box sx={{ p: 3, flex: 1, overflow: 'auto' }}>
        <Box sx={{ borderRadius: 2, border: 1, borderColor: 'divider', overflow: 'hidden' }}>
          <Box sx={{ px: 2, py: 1.5 }}>
            <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 2 }}>
              <Box sx={{ display: 'flex', alignItems: 'center', gap: 1.5 }}>
                <HttpOutlinedIcon fontSize="small" sx={{ color: 'text.secondary' }} />
                <Typography>{t('settings:service.row.httpServerPort')}</Typography>
              </Box>
              <TextField
                size="small"
                type="number"
                value={draftPort}
                onChange={e => setDraftPort(e.target.value)}
                error={invalid}
                sx={{ width: 140 }}
              />
            </Box>
            <FormHelperText error={invalid}>{t('settings:service.help.httpServerPort', { port: defaultPort })}</FormHelperText>
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
          {t('settings:common.button.reset')}
        </Button>
        <Button onClick={handleCancel} disabled={!dirty} color="inherit">
          {t('settings:common.button.cancel')}
        </Button>
        <Button onClick={handleSave} disabled={!dirty || invalid} variant="contained">
          {t('settings:common.button.save')}
        </Button>
      </Box>
    </Box>
  );
}

export default ServiceConfigPage;
