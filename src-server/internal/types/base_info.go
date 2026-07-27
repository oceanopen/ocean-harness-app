// Package types 存放各接口的请求/返回 DTO（数据传输对象）。
//
// 按「路由模块」组织文件：base_info.go 汇集 /api/baseInfo 命名空间下的全部 DTO，
// 后续新增模块时按 <module>.go 新增文件，保持清晰。
package types

// SysInfoRequest 是 GET /api/baseInfo/getSysInfo 的请求参数（当前无入参，预留扩展）。
type SysInfoRequest struct{}

// SysInfoResponseData 是 GET /api/baseInfo/getSysInfo 的返回数据（前端 ServerStatusPage 消费）。
// JSON tag 与前端 SysInfoData interface 对齐（src/windows/panel/ServerStatusPage.tsx）。
type SysInfoResponseData struct {
	Hostname  string `json:"hostname"`
	GoVersion string `json:"goVersion"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Mode      string `json:"mode"`
}
