package service

import (
	"ocean-harness/server/internal/acpdoctor"
	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/types"
	"ocean-harness/server/internal/global"
)

// Doctor 对应 /api/doctor 命名空间下的业务逻辑。
// 无本地表：SSOT 为 acpdoctor 进程内结论缓存；本 service 只做受理转发与四态投影
// （vendoring 目录来自 global.Config 可选字段，未配置在探测期落为 unhealthy 而非绕过）。
type Doctor struct {
	apis.Service
}

// Check 受理探测（异步受理模型）：agentCode 为空 = 全部 enabled 条目；在跑条目单飞 join
// 不重跑。立即返回受理清单 + 受理时刻四态快照，探测结论由前端经 getInfo 轮询跟进。
func (svc Doctor) Check(req *types.DoctorCheckRequest) (*types.DoctorCheckResponseData, error) {
	accepted, err := acpdoctor.RequestCheck(req.AgentCode, acpdoctor.Dirs{
		Resources: global.Config.AcpResourcesDir,
		Adapters:  global.Config.AcpAdaptersDir,
	}, global.Logger)
	if err != nil {
		return nil, err
	}
	return &types.DoctorCheckResponseData{
		Accepted: accepted,
		Entries:  projectDoctorStates(acpdoctor.Snapshot()),
	}, nil
}

// GetInfo 返回全部 enabled 条目的当前四态快照（轮询读端，无副作用）。
func (svc Doctor) GetInfo() ([]types.DoctorEntryStateData, error) {
	return projectDoctorStates(acpdoctor.Snapshot()), nil
}

// projectDoctorStates 四态快照 → 前端 DTO 投影（Check/GetInfo 共用）。
func projectDoctorStates(states []acpdoctor.EntryState) []types.DoctorEntryStateData {
	entries := make([]types.DoctorEntryStateData, 0, len(states))
	for _, s := range states {
		entries = append(entries, types.DoctorEntryStateData{
			AgentCode:    s.AgentCode,
			Status:       string(s.Status),
			CheckedAt:    s.CheckedAt,
			Stage:        string(s.Stage),
			Reason:       s.Reason,
			CommandCount: s.CommandCount,
			NodeVersion:  s.NodeVersion,
		})
	}
	return entries
}
