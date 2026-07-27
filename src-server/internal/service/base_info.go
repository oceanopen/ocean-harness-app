// Package service 处理具体业务逻辑，与 HTTP 层（controller）解耦。
package service

import (
	"os"
	"runtime"

	"we-claude-terminal/go-server/internal/global"
	"we-claude-terminal/go-server/internal/types"
)

// BaseInfoService 对应 /api/baseInfo 命名空间下的业务逻辑。
type BaseInfoService struct{}

// NewBaseInfoService 构造 BaseInfoService。
func NewBaseInfoService() *BaseInfoService {
	return &BaseInfoService{}
}

// GetSysInfo 采集系统信息 + 当前运行模式（mode 取自全局 Config）。
func (s *BaseInfoService) GetSysInfo(_ *types.SysInfoRequest) (*types.SysInfoResponseData, error) {
	hostname, _ := os.Hostname()
	return &types.SysInfoResponseData{
		Hostname:  hostname,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Mode:      global.Config.Mode,
	}, nil
}
