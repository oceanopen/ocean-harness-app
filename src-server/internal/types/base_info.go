// Package types 存放各接口的请求/返回 DTO（数据传输对象）。
//
// 按「路由模块」组织文件：base_info.go 汇集 /api/baseInfo 命名空间下的全部 DTO，
// 后续新增模块时按 <module>.go 新增文件，保持清晰。
package types

// ServerRunInfoRequest 是 GET /api/baseInfo/getServerRunInfo 的请求参数（当前无入参，预留扩展）。
type ServerRunInfoRequest struct{}

// SysInfo 系统信息（主机名/Go 运行时/OS/架构），ServerRunInfo 的系统信息块。
type SysInfo struct {
	Hostname  string `json:"hostname"`
	GoVersion string `json:"goVersion"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// ServerInfo 服务运行信息（运行模式/监听地址/日志与数据目录），ServerRunInfo 的服务信息块。
type ServerInfo struct {
	Mode      string `json:"mode"`
	Address   string `json:"address"`   // 服务监听地址（http://127.0.0.1:<port>）
	LogDir    string `json:"logDir"`    // 日志目录（绝对路径）
	SqliteDir string `json:"sqliteDir"` // sqlite 数据目录（绝对路径）
}

// ServerRunInfo 是 GET /api/baseInfo/getServerRunInfo 的返回数据（前端 ServerStatusPage 消费）。
// 含 SysInfo（系统信息）与 ServerInfo（服务信息）两块，JSON tag 与前端 interface 对齐。
type ServerRunInfo struct {
	SysInfo    SysInfo    `json:"sysInfo"`
	ServerInfo ServerInfo `json:"serverInfo"`
}
