import type { AgentCode } from '@src/shared/agentCode';
import { AgentCatalogService } from '@src/services';
import { useQuery } from '@tanstack/react-query';
import { useMemo } from 'react';
import { agentCatalogKeys } from './keys';

/** 全部 agent catalog 条目（含 disabled；SSOT 为 sidecar 内嵌离线快照）。 */
export function useAgentCatalog() {
  return useQuery({
    queryKey: agentCatalogKeys.list(),
    queryFn: () => AgentCatalogService.getList(),
  });
}

/** ACP Agent 下拉选项（value/label）。enabled 条目投影，顺序即 catalog 内嵌顺序；就绪前为空数组。 */
export interface AcpAgentOption {
  value: AgentCode;
  label: string;
}

/** ACP Agent 下拉可选面（派生值，消费方以 length === 0 判未就绪）。 */
export function useAcpAgentOptions(): AcpAgentOption[] {
  const { data } = useAgentCatalog();
  return useMemo(
    () => (data ?? []).filter(entry => entry.enabled).map(entry => ({ value: entry.code, label: entry.label })),
    [data],
  );
}

/** ACP Agent 选中态派生：未显式选择 → 首个 enabled 条目；显式值原样保留（含目录遗留值）。 */
export function useEffectiveAcpAgentCode(agentCode?: AgentCode): AgentCode {
  const options = useAcpAgentOptions();
  return agentCode ?? options[0]?.value ?? '';
}
