// Package service 处理具体业务逻辑，与 HTTP 层（controller）解耦。
package service

import (
	"fmt"
	"os"
	"runtime"

	"we-claude-terminal/go-server/internal/dal/types"
	"we-claude-terminal/go-server/internal/global"
)

// BaseInfoService 对应 /api/baseInfo 命名空间下的业务逻辑。
type BaseInfoService struct{}

// NewBaseInfoService 构造 BaseInfoService。
func NewBaseInfoService() *BaseInfoService {
	return &BaseInfoService{}
}

// GetServerRunInfo 采集系统信息（SysInfo）+ 服务运行信息（ServerInfo）。
// 服务地址、日志/数据目录一并放在 ServerInfo 返回，前端按接口展示（不依赖 Rust IPC）。
func (s *BaseInfoService) GetServerRunInfo(_ *types.ServerRunInfoRequest) (*types.ServerRunInfo, error) {
	hostname, _ := os.Hostname()
	return &types.ServerRunInfo{
		SysInfo: types.SysInfo{
			Hostname:  hostname,
			GoVersion: runtime.Version(),
			OS:        runtime.GOOS,
			Arch:      runtime.GOARCH,
		},
		ServerInfo: types.ServerInfo{
			Mode:      global.Config.Mode,
			Address:   fmt.Sprintf("http://127.0.0.1:%d", global.Config.Port),
			LogDir:    global.Config.LogDir,
			SqliteDir: global.Config.SqliteDir,
		},
	}, nil
}
