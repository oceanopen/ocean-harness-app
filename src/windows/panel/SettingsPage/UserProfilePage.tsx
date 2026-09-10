import CheckCircleIcon from '@mui/icons-material/CheckCircle';
import GitHubIcon from '@mui/icons-material/GitHub';
import RemoveCircleOutlinedIcon from '@mui/icons-material/RemoveCircleOutlined';
import { Box, Button, Chip, TextField, Typography } from '@mui/material';
import { DEFAULT_GITHUB_PAT, getAppConfig, GITHUB_PAT_KEY, setAppConfig } from '@src/shared/appConfig';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';

// 个人中心（T4.1）：个人相关凭据/账号的统一归口，按「凭据卡片」组织——本期仅 GitHub
// PAT 一张卡片（MCP github 工具调用 GitHub API 的认证凭据），后续第三方平台账号、
// gitee 私有部署地址、各类 apikey 各自成卡片在此追加。
//
// 敏感值不回显：读取仅判断「已配置/未配置」，输入框独立 draft 态（留空 = 保持不变、
// 非空 = 覆盖保存），保存后清空输入。Token 明文存于本地 app.db（与配置同一存储，
// 单机单用户口径），UI 不回显以降低肩窥/截图泄露面。
function UserProfilePage() {
  const { t } = useTranslation();

  const [patConfigured, setPatConfigured] = useState(false);
  const [draftPat, setDraftPat] = useState('');

  useEffect(() => {
    getAppConfig(GITHUB_PAT_KEY).then((value) => {
      setPatConfigured((value ?? DEFAULT_GITHUB_PAT).trim() !== '');
    });
  }, []);

  const handleSave = async () => {
    const trimmed = draftPat.trim();
    if (trimmed === '') {
      return;
    }
    await setAppConfig(GITHUB_PAT_KEY, trimmed);
    setPatConfigured(true);
    setDraftPat('');
  };
  const handleClear = async () => {
    await setAppConfig(GITHUB_PAT_KEY, DEFAULT_GITHUB_PAT);
    setPatConfigured(false);
    setDraftPat('');
  };

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <Box sx={{ p: 3, flex: 1, overflow: 'auto' }}>
        {/* GitHub 凭据卡片：标题行（图标 + 名称 + 配置状态）+ Token 录入 + 说明 */}
        <Box sx={{ borderRadius: 2, border: 1, borderColor: 'divider', overflow: 'hidden' }}>
          <Box sx={{ px: 2, py: 1.5, display: 'flex', alignItems: 'center', gap: 1.5 }}>
            <GitHubIcon fontSize="small" sx={{ color: 'text.secondary' }} />
            <Typography>{t('settings:userProfile.github.row.pat')}</Typography>
            {patConfigured
              ? (
                  <Chip
                    size="small"
                    icon={<CheckCircleIcon />}
                    label={t('settings:userProfile.github.status.configured')}
                    color="success"
                    variant="outlined"
                    sx={{ ml: 'auto' }}
                  />
                )
              : (
                  <Chip
                    size="small"
                    icon={<RemoveCircleOutlinedIcon />}
                    label={t('settings:userProfile.github.status.notConfigured')}
                    color="default"
                    variant="outlined"
                    sx={{ ml: 'auto' }}
                  />
                )}
          </Box>
          <Box sx={{ px: 2, pb: 2, pt: 0.5, display: 'flex', flexDirection: 'column', gap: 1 }}>
            <TextField
              size="small"
              type="password"
              autoComplete="off"
              fullWidth
              placeholder={t('settings:userProfile.github.placeholder.pat')}
              value={draftPat}
              onChange={(e) => {
                setDraftPat(e.target.value);
              }}
            />
            <Typography variant="caption" color="text.secondary">
              {t('settings:userProfile.github.help.pat')}
            </Typography>
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
        <Button onClick={handleClear} color="inherit" disabled={!patConfigured}>
          {t('settings:userProfile.github.button.clear')}
        </Button>
        <Button onClick={handleSave} disabled={draftPat.trim() === ''} variant="contained">
          {t('settings:common.button.save')}
        </Button>
      </Box>
    </Box>
  );
}

export default UserProfilePage;
