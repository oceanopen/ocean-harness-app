import type { DoctorEntryState } from '@src/services';
import { DoctorService } from '@src/services';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { doctorKeys } from './keys';

/** 四态视图是否有未收敛的探测在跑（受理后 1s 轮询的驱动条件）。 */
export function isDoctorBusy(entries: DoctorEntryState[] | undefined): boolean {
  return !!entries?.some(entry => entry.status === 'checking' || entry.status === 'unknown');
}

/**
 * 全部 enabled 条目的四态快照。存在 checking/unknown（探测在跑或未收敛）时 1s 轮询，
 * 全部落定自动停；T1.4 启动后台探测的结论也经本 hook 呈现（重挂载即读到缓存）。
 */
export function useDoctorReports() {
  return useQuery({
    queryKey: doctorKeys.reports(),
    queryFn: () => DoctorService.getInfo(),
    refetchInterval: query => (isDoctorBusy(query.state.data) ? 1000 : false),
  });
}

/** 按 agentCode 取单条目四态视图（无此条目返回 undefined）。 */
export function useDoctorEntry(agentCode: string): DoctorEntryState | undefined {
  const { data } = useDoctorReports();
  return data?.find(entry => entry.agentCode === agentCode);
}

/** 受理探测（agentCode 缺省 = 全部 enabled）：受理即整域失效，轮询跟进收敛。 */
export function useDoctorCheck() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (agentCode?: string) => DoctorService.check({ agentCode }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: doctorKeys.root });
    },
  });
}
