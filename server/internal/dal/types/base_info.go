/*
 * Package types 存放各接口的请求/返回 DTO（数据传输对象）：按「路由模块」组织文件，base_info.go 汇集 /api/baseInfo 命名空间下的全部 DTO，后续新增模块时按 <module>.go 新增文件。
 */
package types

// ServerRunInfoRequest 是 GET /api/baseInfo/getServerRunInfo 的请求参数（当前无入参，预留扩展）。
type ServerRunInfoRequest struct{}

// SysInfo 系统信息（主机名/Go 运行时/OS/架构）
type SysInfo struct {
	Hostname  string `json:"hostname"`
	GoVersion string `json:"goVersion"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// ServerInfo 服务运行信息（运行模式/监听地址/日志与数据目录）
type ServerInfo struct {
	Mode      string `json:"mode"`
	Address   string `json:"address"`   // 服务监听地址（http://127.0.0.1:<port>）
	LogDir    string `json:"logDir"`    // 日志目录（绝对路径）
	SqliteDir string `json:"sqliteDir"` // sqlite 数据目录（绝对路径）
}

// RuntimeInfo 内置运行时版本信息（随包分发的 vendored 运行时；缺失为空串，展示方标注「未打包」）。
type RuntimeInfo struct {
	NodeVersion   string `json:"nodeVersion"`   // 内嵌 node 版本（runtime/node/<semver> 目录名）
	ClaudeVersion string `json:"claudeVersion"` // vendored claude --version 输出
}

// ServerRunInfoResponseData 是 GET /api/baseInfo/getServerRunInfo 的返回数据。
type ServerRunInfoResponseData struct {
	SysInfo     SysInfo     `json:"sysInfo"`
	ServerInfo  ServerInfo  `json:"serverInfo"`
	RuntimeInfo RuntimeInfo `json:"runtimeInfo"`
}
