package types

import "time"

// doctor 域 DTO：无本地表，SSOT 为 acpdoctor 进程内结论缓存（重启即 unknown，启动后台
// 探测重填）。手写镜像前端 DoctorService 的 TS interface（项目约定，无生成器）。

// DoctorCheckRequest 是 POST /api/doctor/check 的入参：受理探测（异步，立即返回受理
// 清单与受理时刻的四态快照）。agentCode 为空 = 全部 enabled 条目。
type DoctorCheckRequest struct {
	AgentCode string `json:"agentCode"`
}

// DoctorCheckResponseData 是 /api/doctor/check 的响应：Accepted 为本次受理的 agentCode
// 列表（已在跑的条目同样计入受理），Entries 为受理时刻的四态快照（可直接落地渲染，
// 后续以 getInfo 轮询跟进）。
type DoctorCheckResponseData struct {
	Accepted []string               `json:"accepted"`
	Entries  []DoctorEntryStateData `json:"entries"`
}

// DoctorGetInfoRequest 是 POST /api/doctor/getInfo 的入参（无筛选，全量四态快照）。
type DoctorGetInfoRequest struct{}

// DoctorEntryStateData 是单条目的四态视图：status ∈ healthy/unhealthy/unknown/checking。
// checking（受理在跑）时 CheckedAt 等结论字段为上一次结论（从未探测则为零值）；
// unknown 时结论字段为零值。
type DoctorEntryStateData struct {
	AgentCode    string    `json:"agentCode"`
	Status       string    `json:"status"`
	CheckedAt    time.Time `json:"checkedAt"`
	Stage        string    `json:"stage,omitempty"`  // unhealthy 时的失败阶段
	Reason       string    `json:"reason,omitempty"` // unhealthy 时的用户可读原因（后端生成，前端直显）
	CommandCount int       `json:"commandCount"`     // healthy 时收到的 available_commands 数量
	NodeVersion  string    `json:"nodeVersion,omitempty"`
}
