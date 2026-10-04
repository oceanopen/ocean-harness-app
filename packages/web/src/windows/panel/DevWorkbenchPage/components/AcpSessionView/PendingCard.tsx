import type { AcpPendingView } from '@src/services';
import { Box, Button, Checkbox, FormControlLabel, FormGroup, Radio, RadioGroup, Stack, Switch, TextField, Typography } from '@mui/material';
import { useRespondAcpElicitation, useRespondAcpPermission } from '@src/state/acpSession';
import { Fragment, useState } from 'react';
import { collectElicitationContent, renderElicitationPlan } from './elicitationForm';
import ToolCallItem from './ToolCallItem';

// 挂起交互卡片：permission 审批（工具详情 + 选项按钮组）/ elicitation 表单（message +
// requestedSchema 子集渲染 + 提交/跳过）。移除由 SSE pendingClosed 帧驱动（应答成功才
// 发帧，失败挂起原样保留），本地无乐观删除；应答中整卡禁用 + 所点按钮 loading（等待态
// 可见优先）。url/未知形态兜底：只呈现 message + 跳过。
export default function PendingCard({ issueId, pending }: { issueId: string; pending: AcpPendingView }) {
  if (pending.kind === 'permission') {
    return <PermissionCard issueId={issueId} pending={pending} />;
  }
  return <ElicitationCard issueId={issueId} pending={pending} />;
}

/** 卡片外壳：左侧警示条 + 标题行。 */
function CardShell({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <Box sx={{ border: 1, borderColor: 'divider', borderLeftWidth: 3, borderLeftColor: 'warning.main', borderRadius: 1, p: 1.25, bgcolor: 'background.paper' }}>
      <Typography variant="caption" sx={{ display: 'block', mb: 0.75, fontWeight: 600 }}>{title}</Typography>
      {children}
    </Box>
  );
}

/**
 * 应答结果行：收敛哨兵文案（该审批已/该表单已——另一端先答或随回合结算，T2.4）中性
 * 呈现（无失败前缀、warning 色），其余维持失败红前缀文案。前缀契约 SSOT 是服务端
 * acpsession 的 PendingAlreadyHandledError.Error() 文案模板（view.go，双侧已声明），
 * 服务端改模板需双端同步。挂起不久将由 pendingClosed 帧移除，只提示不阻断。
 */
function RespondError({ message }: { message: string }) {
  const converged = message.startsWith('该审批已') || message.startsWith('该表单已');
  return (
    <Typography variant="caption" color={converged ? 'warning.main' : 'error'} sx={{ display: 'block', mt: 0.75 }}>
      {converged ? message : `应答失败：${message}`}
    </Typography>
  );
}

function PermissionCard({ issueId, pending }: { issueId: string; pending: AcpPendingView }) {
  const respond = useRespondAcpPermission();
  const [choosing, setChoosing] = useState<string | null>(null);
  const options = pending.options ?? [];

  const choose = (optionId: string) => {
    setChoosing(optionId);
    respond.mutate(
      { issueId, pendingId: pending.pendingId, optionId },
      { onSettled: () => setChoosing(null) },
    );
  };

  return (
    <CardShell title="Agent 请求授权">
      {pending.toolCall && (
        <Box sx={{ mb: 1 }}>
          <ToolCallItem toolCall={pending.toolCall.toolCallId ? pending.toolCall : { ...pending.toolCall, toolCallId: `pending:${pending.pendingId}` }} />
        </Box>
      )}
      <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: 'wrap' }}>
        {options.map(option => (
          <Button
            key={option.optionId}
            size="small"
            disabled={respond.isPending}
            loading={choosing === option.optionId && respond.isPending}
            variant={option.kind.endsWith('_always') ? 'contained' : 'outlined'}
            color={option.kind.startsWith('allow') ? 'success' : 'error'}
            onClick={() => choose(option.optionId)}
          >
            {option.name}
            {option.kind.endsWith('_always') && '（记住）'}
          </Button>
        ))}
      </Stack>
      {options.length === 0 && <Typography variant="body2" color="text.secondary">该请求未携带选项</Typography>}
      {respond.isError && <RespondError message={respond.error.message} />}
    </CardShell>
  );
}

function ElicitationCard({ issueId, pending }: { issueId: string; pending: AcpPendingView }) {
  const respond = useRespondAcpElicitation();
  const request = pending.request;
  const plan = renderElicitationPlan(request?.requestedSchema);
  const [values, setValues] = useState<Record<string, unknown>>({});
  // 仅 form 模式可提交（accept）；url/未知形态只保留跳过——url 模式的 accept 语义是
  // 「已在站外完成」，应用内无法支撑。
  const canAccept = request?.mode === 'form' && plan.length > 0;

  const submit = (action: 'accept' | 'decline') => {
    respond.mutate({
      issueId,
      pendingId: pending.pendingId,
      action,
      ...(action === 'accept' ? { content: collectElicitationContent(plan, values) } : {}),
    });
  };

  return (
    <CardShell title="Agent 请求输入">
      {request?.message && (
        <Typography variant="body2" sx={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word', mb: canAccept ? 1 : 0 }}>
          {request.message}
        </Typography>
      )}
      {canAccept && <FormFields plan={plan} values={values} onChange={setValues} disabled={respond.isPending} />}
      {request?.mode === 'url' && request.url && (
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', wordBreak: 'break-all' }}>
          链接地址：{request.url}（请在浏览器中完成后回来跳过或按 Agent 指引操作）
        </Typography>
      )}
      <Stack direction="row" spacing={1} useFlexGap sx={{ mt: 1 }}>
        {canAccept && (
          <Button size="small" variant="contained" onClick={() => submit('accept')} loading={respond.isPending} disabled={respond.isPending}>
            提交
          </Button>
        )}
        <Button size="small" variant="text" onClick={() => submit('decline')} disabled={respond.isPending}>
          跳过
        </Button>
      </Stack>
      {canAccept && (
        <Typography variant="caption" color="text.disabled" sx={{ display: 'block', mt: 0.5 }}>
          未填字段按跳过处理；跳过即向 agent 表示未提供信息
        </Typography>
      )}
      {respond.isError && <RespondError message={respond.error.message} />}
    </CardShell>
  );
}

/** 渲染计划 → 表单控件（值统一存 values，收集语义见 collectElicitationContent）。 */
function FormFields({ plan, values, onChange, disabled }: {
  plan: ReturnType<typeof renderElicitationPlan>;
  values: Record<string, unknown>;
  onChange: (values: Record<string, unknown>) => void;
  disabled: boolean;
}) {
  const set = (key: string, value: unknown) => onChange({ ...values, [key]: value });
  return (
    <Stack spacing={1.25}>
      {plan.map((field) => {
        const heading = field.title && (
          <Typography variant="body2" sx={{ fontWeight: 600 }}>{field.title}</Typography>
        );
        const hint = field.description && (
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.25 }}>{field.description}</Typography>
        );
        return (
          <Box key={field.key}>
            {heading}
            {field.widget === 'radio' && (
              <RadioGroup value={(values[field.key] as string) ?? ''} onChange={e => set(field.key, e.target.value)} sx={{ mt: 0.5 }}>
                {field.options?.map(option => (
                  <Fragment key={option.value}>
                    <FormControlLabel value={option.value} control={<Radio size="small" disabled={disabled} />} label={option.label ?? option.value} sx={{ my: -0.25 }} />
                    {option.description && (
                      <Typography variant="caption" color="text.disabled" sx={{ ml: 4.25, mt: -0.5, display: 'block' }}>{option.description}</Typography>
                    )}
                  </Fragment>
                ))}
              </RadioGroup>
            )}
            {field.widget === 'checkbox' && (
              <FormGroup sx={{ mt: 0.5 }}>
                {field.options?.map((option) => {
                  const picks = Array.isArray(values[field.key]) ? (values[field.key] as string[]) : [];
                  return (
                    <FormControlLabel
                      key={option.value}
                      control={(
                        <Checkbox
                          size="small"
                          disabled={disabled}
                          checked={picks.includes(option.value)}
                          onChange={e => set(field.key, e.target.checked ? [...picks, option.value] : picks.filter(v => v !== option.value))}
                        />
                      )}
                      label={option.label ?? option.value}
                      sx={{ my: -0.25 }}
                    />
                  );
                })}
              </FormGroup>
            )}
            {field.widget === 'text' && (
              <TextField
                size="small"
                fullWidth
                disabled={disabled}
                value={(values[field.key] as string) ?? ''}
                onChange={e => set(field.key, e.target.value)}
                sx={{ mt: 0.5 }}
              />
            )}
            {field.widget === 'number' && (
              <TextField
                size="small"
                fullWidth
                type="number"
                disabled={disabled}
                value={values[field.key] === undefined || values[field.key] === null ? '' : String(values[field.key])}
                onChange={(e) => {
                  const raw = e.target.value;
                  set(field.key, raw === '' ? undefined : Number(raw));
                }}
                sx={{ mt: 0.5 }}
              />
            )}
            {field.widget === 'switch' && (
              <FormControlLabel
                control={<Switch size="small" disabled={disabled} checked={values[field.key] === true} onChange={e => set(field.key, e.target.checked)} />}
                label={field.title ? undefined : '启用'}
                sx={{ mt: 0.25 }}
              />
            )}
            {field.widget === 'unknown' && (
              <Typography variant="caption" color="text.disabled" sx={{ display: 'block', mt: 0.25 }}>
                该字段类型暂不支持在线填写，留空提交即按跳过处理
              </Typography>
            )}
            {field.widget !== 'radio' && hint}
            {field.widget === 'radio' && !field.options?.some(o => o.description) && hint}
          </Box>
        );
      })}
    </Stack>
  );
}
