import type { ProjectStateItem, StateGroup, StateMeta } from '@src/services';
import { DragDropContext, Draggable, Droppable } from '@hello-pangea/dnd';
import { DragIndicator as DragIndicatorIcon } from '@mui/icons-material';
import { Box, Checkbox, CircularProgress, Divider, Radio, Typography } from '@mui/material';
import { useStateCatalog } from '@src/state/tracker';
import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';

// 分组顺序固定（对齐 state_group 工作流语义，与列表分组顺序一致）。
const GROUP_ORDER: StateGroup[] = ['backlog', 'unstarted', 'started', 'completed', 'cancelled'];

const keyOf = (groupCode: StateGroup, code: string) => `${groupCode}|${code}`;

interface ProjectStateManageProps {
  // 当前状态全量列表（受控）。每条引用目录 (stateGroupCode, stateCode)，含 sortOrder/isDefault。
  states: ProjectStateItem[];
  onChange: (states: ProjectStateItem[]) => void;
  disabled?: boolean;
}

// 项目编辑表单内的「状态管理」模块（docs/issue.md §4）。
// 从状态目录（第 2 层）按 group 渲染可勾选项；每 group 首项（目录 sortOrder 升序第一项）强制勾选
// 不可取消，且每 group ≥1（末项亦禁用取消）；默认状态全局单选，仅各 group 首项可设（非首项 radio 置灰）。
// started 组开发步骤（进行中除外）可拖序。勾选/拖序/改默认均经 onChange 全量回吐 ProjectStateItem[]
// （sortOrder 按 GROUP_ORDER × 组内顺序全局递增重排）。
function ProjectStateManage({ states, onChange, disabled }: ProjectStateManageProps) {
  const { t } = useTranslation();
  const catalog = useStateCatalog();

  // 从 states 派生：选中集合 / 默认项 key / started 开发步骤顺序（按 sortOrder 升序的 code 序列）。
  const selected = useMemo(() => new Set(states.map(it => keyOf(it.stateGroupCode, it.stateCode))), [states]);
  const defaultKey = useMemo(() => {
    const d = states.find(it => it.isDefault === 'Y');
    return d ? keyOf(d.stateGroupCode, d.stateCode) : '';
  }, [states]);
  const devStepOrder = useMemo(
    () => states
      .filter(it => it.stateGroupCode === 'started' && it.stateCode !== 'in_progress')
      .sort((a, b) => a.sortOrder - b.sortOrder)
      .map(it => it.stateCode),
    [states],
  );

  const groups = catalog.data?.groups ?? [];
  const statesByGroup = useMemo(() => {
    const m = new Map<StateGroup, StateMeta[]>();
    for (const g of GROUP_ORDER) {
      m.set(g, (catalog.data?.states ?? []).filter(s => s.groupCode === g).sort((a, b) => a.sortOrder - b.sortOrder));
    }
    return m;
  }, [catalog.data]);

  // isFirstInGroup：m 是否其所在 group 的目录首项（sortOrder 升序第一项）。首项强制勾选不可取消，
  // 且仅首项可设默认（非首项 radio 置灰）。基于固定目录序，与用户启用集合无关（"仅约束新操作"）。
  const isFirstInGroup = (m: StateMeta) => (statesByGroup.get(m.groupCode) ?? [])[0]?.code === m.code;

  // buildOutput：按 GROUP_ORDER × 组内顺序（started 开发步骤按 devStepOrder、进行中置首）重排，
  // sortOrder 全局递增（1000 步进），isDefault 按 defaultKey 标记。
  function buildOutput(nextSelected: Set<string>, nextDefault: string, nextDevStepOrder: string[]): ProjectStateItem[] {
    const out: ProjectStateItem[] = [];
    let order = 1000;
    for (const g of GROUP_ORDER) {
      const picked = (statesByGroup.get(g) ?? [])
        .filter(m => nextSelected.has(keyOf(m.groupCode, m.code)))
        .sort((a, b) => {
          if (g === 'started') {
            const rank = (code: string) => (code === 'in_progress' ? -1 : nextDevStepOrder.indexOf(code));
            return rank(a.code) - rank(b.code);
          }
          return a.sortOrder - b.sortOrder;
        });
      for (const m of picked) {
        const key = keyOf(m.groupCode, m.code);
        out.push({
          stateGroupCode: g,
          stateCode: m.code,
          sortOrder: order,
          isDefault: key === nextDefault ? 'Y' : 'N',
        });
        order += 1000;
      }
    }
    return out;
  }

  const toggle = (meta: StateMeta) => {
    const key = keyOf(meta.groupCode, meta.code);
    const next = new Set(selected);
    if (next.has(key)) {
      // 首项强制勾选不可取消；且每 group ≥1（末项禁用取消）。
      const inGroup = states.filter(it => it.stateGroupCode === meta.groupCode).length;
      if (isFirstInGroup(meta) || inGroup <= 1) {
        return;
      }
      next.delete(key);
      // 取消的恰是默认项 → 默认转给该 group 剩余首项。
      let nextDefault = defaultKey;
      if (defaultKey === key) {
        const remaining = (statesByGroup.get(meta.groupCode) ?? [])
          .filter(m => next.has(keyOf(m.groupCode, m.code)))
          .map(m => keyOf(m.groupCode, m.code));
        nextDefault = remaining[0] ?? '';
      }
      onChange(buildOutput(next, nextDefault, devStepOrder));
    } else {
      next.add(key);
      onChange(buildOutput(next, defaultKey, devStepOrder));
    }
  };

  const setDefault = (key: string) => {
    onChange(buildOutput(selected, key, devStepOrder));
  };

  const reorderStep = (from: number, to: number) => {
    const next = [...devStepOrder];
    const [moved] = next.splice(from, 1);
    next.splice(to, 0, moved);
    onChange(buildOutput(selected, defaultKey, next));
  };

  if (!catalog.data) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', py: 2 }}>
        <CircularProgress size={24} />
      </Box>
    );
  }

  const devStepMetas = (statesByGroup.get('started') ?? []).filter(m => m.code !== 'in_progress');

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5, border: 1, borderColor: 'divider', borderRadius: 1, p: 1.5 }}>
      <Typography variant="caption" color="text.secondary">
        {t('tracker:workspaceProject.stateManage.title')}
      </Typography>
      <Divider />
      {GROUP_ORDER.map((g) => {
        const groupMeta = groups.find(x => x.code === g);
        const metas = statesByGroup.get(g) ?? [];
        const inGroup = states.filter(it => it.stateGroupCode === g).length;
        return (
          <Box key={g} sx={{ display: 'flex', flexDirection: 'column', gap: 0.5 }}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.75 }}>
              <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: groupMeta?.color ?? 'text.disabled' }} />
              <Typography variant="subtitle2">{groupMeta?.name ?? g}</Typography>
            </Box>
            {metas.map((m) => {
              const key = keyOf(m.groupCode, m.code);
              const checked = selected.has(key);
              const isFirst = isFirstInGroup(m); // 目录首项：强制勾选不可取消
              const disableUncheck = checked && (isFirst || inGroup <= 1); // 首项 / 末项禁用取消
              return (
                <Box key={key} sx={{ display: 'flex', alignItems: 'center', pl: 2.5 }}>
                  <Checkbox size="small" checked={checked} disabled={disabled || disableUncheck} onChange={() => toggle(m)} />
                  <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: m.color, ml: 0.5 }} />
                  <Typography variant="body2" sx={{ ml: 0.75 }}>{m.name}</Typography>
                  {/* 默认状态 radio：同 name 互斥，全局恰一；仅各 group 首项（且已勾选）可设默认，非首项置灰 */}
                  <Radio
                    size="small"
                    name="stateManageDefault"
                    checked={key === defaultKey}
                    onChange={() => setDefault(key)}
                    disabled={disabled || !isFirst || !checked}
                    sx={{ ml: 'auto' }}
                  />
                </Box>
              );
            })}
            {g === 'started' && devStepOrder.length > 0 && (
              <Box sx={{ mt: 0.5, ml: 2.5, border: 1, borderColor: 'divider', borderRadius: 1, p: 1.5, display: 'flex', flexDirection: 'column', gap: 1.5 }}>
                <Typography variant="caption" color="text.secondary">
                  {t('tracker:workspaceProject.stateManage.stepOrder')}
                </Typography>
                <Divider />
                <DragDropContext onDragEnd={(r) => {
                  if (r.destination && r.source.index !== r.destination.index) {
                    reorderStep(r.source.index, r.destination.index);
                  }
                }}
                >
                  <Droppable droppableId="devSteps">
                    {provided => (
                      <Box ref={provided.innerRef} {...provided.droppableProps} sx={{ mt: 0.5, display: 'flex', flexDirection: 'column', gap: 0.5 }}>
                        {devStepOrder.map((code, idx) => {
                          const meta = devStepMetas.find(m => m.code === code);
                          if (!meta) {
                            return null;
                          }
                          return (
                            <Draggable key={code} draggableId={code} index={idx}>
                              {p => (
                                <Box
                                  ref={p.innerRef}
                                  {...p.draggableProps}
                                  {...p.dragHandleProps}
                                  sx={{
                                    'display': 'flex',
                                    'alignItems': 'center',
                                    'gap': 0.75,
                                    'px': 1,
                                    'py': 0.75,
                                    'borderRadius': 1,
                                    'bgcolor': 'action.hover',
                                    'cursor': 'grab',
                                    '&:active': { cursor: 'grabbing' },
                                    '&:hover': { bgcolor: 'action.selected' },
                                  }}
                                >
                                  <DragIndicatorIcon fontSize="small" color="action" />
                                  <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: meta.color }} />
                                  <Typography variant="body2">{meta.name}</Typography>
                                </Box>
                              )}
                            </Draggable>
                          );
                        })}
                        {provided.placeholder}
                      </Box>
                    )}
                  </Droppable>
                </DragDropContext>
              </Box>
            )}
          </Box>
        );
      })}
    </Box>
  );
}

export default ProjectStateManage;
