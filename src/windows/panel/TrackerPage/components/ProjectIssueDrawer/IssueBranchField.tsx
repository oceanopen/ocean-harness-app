import type { SxProps } from '@mui/material';
import { Autocomplete, Box, FormControl, MenuItem, Select, TextField, Typography } from '@mui/material';
import { useLocalBranches } from '@src/state/localRepositories';
import { useProjectRepositories } from '@src/state/tracker';
import { useId } from 'react';
import { useTranslation } from 'react-i18next';

// Issue 关联分支字段：仓库下拉（选项 = 项目已关联仓库）+ 分支 Combobox（freeSolo，选项 = 该仓库本地分支）。
// 受控：仅改本地值（父级统一提交），不发请求。仓库变更时清空分支（分支按仓库不同）。
// 项目无关联仓库时整体禁用并提示；未选仓库时分支框禁用。
interface IssueBranchFieldProps {
  projectId: number;
  localRepositoryId: number;
  repositoryBranch: string;
  onChange: (localRepositoryId: number, repositoryBranch: string) => void;
  disabled?: boolean;
  sx?: SxProps;
}

function IssueBranchField({
  projectId,
  localRepositoryId,
  repositoryBranch,
  onChange,
  disabled,
  sx,
}: IssueBranchFieldProps) {
  const { t } = useTranslation();
  const reposQuery = useProjectRepositories(projectId);
  const repos = reposQuery.data ?? [];
  const noRepos = repos.length === 0;
  const branchesQuery = useLocalBranches(localRepositoryId);
  const branches = branchesQuery.data ?? [];
  const repoLabelId = useId();

  const handleRepoChange = (repoId: number) => {
    // 切仓库：分支按仓库不同，统一清空。
    if (repoId !== localRepositoryId) {
      onChange(repoId, '');
    }
  };

  return (
    <Box sx={sx}>
      <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>
        {t('tracker:projectIssue.detail.branch')}
      </Typography>
      <Box sx={{ display: 'flex', gap: 1 }}>
        <FormControl size="small" sx={{ flex: '0 0 38%' }}>
          <Select
            labelId={repoLabelId}
            value={localRepositoryId}
            disabled={disabled || noRepos}
            displayEmpty
            renderValue={(v) => {
              const id = Number(v);
              const name = id !== 0 ? repos.find(r => r.id === id)?.name : '';
              return (
                <Typography variant="body2" noWrap color={name ? undefined : 'text.disabled'}>
                  {name || (noRepos
                    ? t('tracker:projectIssue.detail.branchNoRepoOption')
                    : t('tracker:projectIssue.detail.branchRepoPlaceholder'))}
                </Typography>
              );
            }}
            onChange={e => handleRepoChange(Number(e.target.value))}
          >
            <MenuItem value={0} disabled={noRepos}>
              {noRepos
                ? t('tracker:projectIssue.detail.branchNoRepoOption')
                : t('tracker:projectIssue.detail.branchRepoPlaceholder')}
            </MenuItem>
            {repos.map(r => (
              <MenuItem key={r.id} value={r.id}>{r.name}</MenuItem>
            ))}
          </Select>
        </FormControl>
        <Autocomplete
          size="small"
          freeSolo
          options={branches}
          inputValue={repositoryBranch}
          onInputChange={(_, v) => onChange(localRepositoryId, v)}
          disabled={disabled || noRepos || localRepositoryId === 0}
          sx={{ flex: 1 }}
          renderInput={params => (
            <TextField
              {...params}
              placeholder={t('tracker:projectIssue.detail.branchPlaceholder')}
            />
          )}
        />
      </Box>
      {noRepos && (
        <Typography variant="caption" color="text.disabled" sx={{ display: 'block', mt: 0.5 }}>
          {t('tracker:projectIssue.detail.branchNoRepoHint')}
        </Typography>
      )}
    </Box>
  );
}

export default IssueBranchField;
