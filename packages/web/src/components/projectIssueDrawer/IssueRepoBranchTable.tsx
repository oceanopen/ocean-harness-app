import type { SxProps } from '@mui/material';
import type { IssueRepositoryBranchModel, LocalRepositoryModel } from '@src/services';
import { AddOutlined as AddOutlinedIcon, DeleteOutlined as DeleteOutlinedIcon } from '@mui/icons-material';
import { Autocomplete, Box, Button, FormControl, IconButton, MenuItem, Select, Table, TableBody, TableCell, TableHead, TableRow, TextField, Typography } from '@mui/material';
import { useLocalBranches, useLocalRepositories } from '@src/state/localRepositories';
import { useRef } from 'react';
import { useTranslation } from 'react-i18next';

// Issue 关联仓库+分支 table（多选）：每行 = 仓库下拉 + 分支 Combobox（freeSolo）+ 删除按钮，末行「新增」追加空行。
// 受控：rows / onChange 由父级持有（提交统一在父级），组件不发请求。
// 仓库选项 = 项目关联仓库中未被其他行选中的（选中的自动从其他行排除）；
// 选仓库时分支自动预填该仓库默认分支（无默认分支则留空）；项目关联仓库全部选中时新增按钮置灰。
// 项目无关联仓库时整体提示禁用。表头/按钮文案硬编码中文（i18n 约定：表单区不走路由级 i18n）。
interface IssueRepoBranchTableProps {
  // 当前项目已关联的仓库 id 列表（来自项目响应 WorkspaceProjectModel.localRepositoryIds，作仓库下拉选项源）。
  localRepositoryIds: number[];
  rows: IssueRepositoryBranchModel[];
  onChange: (rows: IssueRepositoryBranchModel[]) => void;
  disabled?: boolean;
  sx?: SxProps;
}

function IssueRepoBranchTable({
  localRepositoryIds,
  rows,
  onChange,
  disabled,
  sx,
}: IssueRepoBranchTableProps) {
  const { t } = useTranslation();
  // 仓库下拉选项 = 全局仓库列表按项目关联 id 过滤（useLocalRepositories 全局缓存，无需按项目单独取数）。
  const allReposQuery = useLocalRepositories();
  const projectRepos = (allReposQuery.data ?? []).filter(r => localRepositoryIds.includes(r.id));
  const repoMap = new Map(projectRepos.map(r => [r.id, r]));
  const noRepos = localRepositoryIds.length === 0;

  // 未被任何行选中的仓库（新增按钮可选项；全被选中 → 新增置灰）。
  const selectedRepoIds = new Set(rows.map(r => r.localRepositoryId).filter(id => id > 0));
  const availableRepos = projectRepos.filter(r => !selectedRepoIds.has(r.id));

  // 行 key：rows 由父级全量替换（无稳定行 id），用自增 key 追踪行位置——加行追加新 key、
  // 删行截断尾部（父级删行恒删被点击行并保序，仅末端场景会错位一个渲染周期，无功能影响）。
  // 渲染期同步派生（React 认可的 derive-during-render），避免数组下标作 key。
  const rowKeysRef = useRef<number[]>([]);
  if (rowKeysRef.current.length < rows.length) {
    const next = [...rowKeysRef.current];
    while (next.length < rows.length) {
      next.push(next.length === 0 ? 1 : next[next.length - 1] + 1);
    }
    rowKeysRef.current = next;
  } else if (rowKeysRef.current.length > rows.length) {
    rowKeysRef.current = rowKeysRef.current.slice(0, rows.length);
  }

  // 某行可切换的仓库选项 = 未被其他行选中的仓库 + 该行当前已选仓库。
  const repoOptionsFor = (rowIndex: number): LocalRepositoryModel[] => {
    const currentId = rows[rowIndex]?.localRepositoryId ?? 0;
    return projectRepos.filter(r => r.id === currentId || !selectedRepoIds.has(r.id));
  };

  const updateRow = (index: number, patch: Partial<IssueRepositoryBranchModel>) => {
    onChange(rows.map((row, i) => (i === index ? { ...row, ...patch } : row)));
  };

  const handleRepoChange = (index: number, repoId: number) => {
    // 切仓库：分支重置为新仓库的默认分支（无默认分支则留空）。
    updateRow(index, { localRepositoryId: repoId, repositoryBranch: repoMap.get(repoId)?.defaultBranch ?? '' });
  };

  const addRow = () => {
    onChange([...rows, { localRepositoryId: 0, repositoryBranch: '' }]);
  };

  const removeRow = (index: number) => {
    onChange(rows.filter((_, i) => i !== index));
  };

  return (
    <Box sx={sx}>
      <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>
        {t('tracker:projectIssue.detail.branch')}
      </Typography>
      <Table size="small" sx={{ tableLayout: 'fixed' }}>
        <TableHead>
          <TableRow>
            <TableCell sx={{ width: '40%' }}>仓库</TableCell>
            <TableCell>分支</TableCell>
            <TableCell sx={{ width: 56 }} aria-label="操作">操作</TableCell>
          </TableRow>
        </TableHead>
        <TableBody>
          {rows.map((row, index) => (
            <RepoBranchRow
              key={rowKeysRef.current[index]}
              row={row}
              repoOptions={repoOptionsFor(index)}
              disabled={disabled || noRepos}
              onRepoChange={repoId => handleRepoChange(index, repoId)}
              onBranchChange={branch => updateRow(index, { repositoryBranch: branch })}
              onRemove={() => removeRow(index)}
            />
          ))}
          <TableRow>
            <TableCell colSpan={3} sx={{ border: 'none', py: 1 }}>
              <Button
                size="small"
                startIcon={<AddOutlinedIcon />}
                onClick={addRow}
                disabled={disabled || noRepos || availableRepos.length === 0}
              >
                新增
              </Button>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
      {noRepos && (
        <Typography variant="caption" color="text.disabled" sx={{ display: 'block', mt: 0.5 }}>
          {t('tracker:projectIssue.detail.branchNoRepoHint')}
        </Typography>
      )}
    </Box>
  );
}

// 单行：仓库 Select + 分支 freeSolo Autocomplete（选项 = 该行仓库的本地分支，useLocalBranches 实时查）+ 删除按钮。
interface RepoBranchRowProps {
  row: IssueRepositoryBranchModel;
  repoOptions: LocalRepositoryModel[];
  disabled?: boolean;
  onRepoChange: (repoId: number) => void;
  onBranchChange: (branch: string) => void;
  onRemove: () => void;
}

function RepoBranchRow({ row, repoOptions, disabled, onRepoChange, onBranchChange, onRemove }: RepoBranchRowProps) {
  const { t } = useTranslation();
  const branchesQuery = useLocalBranches(row.localRepositoryId);
  const branches = branchesQuery.data ?? [];
  const repoName = row.localRepositoryId !== 0 ? repoOptions.find(r => r.id === row.localRepositoryId)?.name : '';

  return (
    <TableRow>
      <TableCell sx={{ py: 1 }}>
        <FormControl size="small" fullWidth>
          <Select
            value={row.localRepositoryId}
            disabled={disabled}
            displayEmpty
            renderValue={() => (
              <Typography variant="body2" noWrap color={repoName ? undefined : 'text.disabled'}>
                {repoName || '选择仓库'}
              </Typography>
            )}
            onChange={e => onRepoChange(Number(e.target.value))}
          >
            <MenuItem value={0} disabled>选择仓库</MenuItem>
            {repoOptions.map(r => (
              <MenuItem key={r.id} value={r.id}>{r.name}</MenuItem>
            ))}
          </Select>
        </FormControl>
      </TableCell>
      <TableCell sx={{ py: 1 }}>
        <Autocomplete
          size="small"
          freeSolo
          options={branches}
          inputValue={row.repositoryBranch}
          onInputChange={(_, v) => onBranchChange(v)}
          disabled={disabled || row.localRepositoryId === 0}
          fullWidth
          renderInput={params => (
            <TextField
              {...params}
              placeholder={t('tracker:projectIssue.detail.branchPlaceholder')}
            />
          )}
        />
      </TableCell>
      <TableCell sx={{ py: 1 }}>
        <IconButton size="small" onClick={onRemove} disabled={disabled} aria-label="删除">
          <DeleteOutlinedIcon fontSize="small" />
        </IconButton>
      </TableCell>
    </TableRow>
  );
}

export default IssueRepoBranchTable;
