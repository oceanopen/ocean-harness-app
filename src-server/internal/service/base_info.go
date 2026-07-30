// Package service 处理具体业务逻辑，与 HTTP 层（controller）解耦。
package service

import (
	"fmt"
	"os"
	"runtime"

	"we-claude-terminal/go-server/internal/apis"
	"we-claude-terminal/go-server/internal/dal/types"
	"we-claude-terminal/go-server/internal/global"
)

// BaseInfo 对应 /api/baseInfo 命名空间下的业务逻辑（系统信息采集，无 DB 读写，svc.Orm 不用）。
type BaseInfo struct {
	apis.Service
}

// GetServerRunInfo 采集系统信息（SysInfo）+ 服务运行信息（ServerInfo）。
// 服务地址、日志/数据目录一并放在 ServerInfo 返回，前端按接口展示（不依赖 Rust IPC）。
func (svc BaseInfo) GetServerRunInfo(_ *types.ServerRunInfoRequest) (*types.ServerRunInfoResponseData, error) {
	hostname, _ := os.Hostname()
	return &types.ServerRunInfoResponseData{
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
