package mcputil

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	// database/sql 驱动注册（driver name "sqlite"，与 initialize/sqlite.go 同源 glebarez/modernc，
	// 不新增依赖）。仅本文件以原生 database/sql 短连接方式使用（读一行即关，无需 gorm 装配）。
	_ "github.com/glebarez/sqlite"

	"ocean-harness/src-server/internal/global"
)

// appConfigWorkspaceBaseDirKey 对齐 Rust 侧 app_config KV 表的 key（前端设置页写入
// workspace_base_dir，见 src-tauri/src/shared/app_config.rs）。双端字符串常量、无生成机制，
// 变更须两端同步。
const appConfigWorkspaceBaseDirKey = "workspace_base_dir"

// ReadWorkspaceBaseDir 从 Rust 共享的 app.db（app_config KV 表）只读读取 workspace 基目录，
// 供 MCP workspace_status 类工具解析状态文件路径（应用设置页是唯一写入方，Go 只读）。
//
// 每次调用短连接：os.Stat 前置守卫（缺文件不创建、错误可区分）+ file: URI 的 mode=ro
// （只读句柄，绝不持有写锁）+ busy_timeout（容忍与 Rust 写端瞬时锁冲突；rollback journal
// 下读一行为微秒级），读一行即关、无缓存——用户改设置后下一次工具调用即生效。
func ReadWorkspaceBaseDir() (string, error) {
	if global.Config == nil || global.Config.AppDbPath == "" {
		return "", errors.New("未配置 workspace 基目录（GO_SERVER_APP_DB 未注入）")
	}
	dbPath := global.Config.AppDbPath
	if _, err := os.Stat(dbPath); err != nil {
		return "", errors.New("未配置 workspace 基目录（app.db 不存在）")
	}
	// 注：路径含 "?" / "#" 等 URI 保留字会被截断（macOS app_data_dir 实际不会出现）。
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return "", fmt.Errorf("打开 app.db 失败: %w", err)
	}
	defer db.Close()

	var baseDir string
	err = db.QueryRow("SELECT value FROM app_config WHERE key = ?", appConfigWorkspaceBaseDirKey).Scan(&baseDir)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errors.New("未配置 workspace 基目录（设置页未设置）")
	}
	if err != nil {
		return "", fmt.Errorf("读取 app_config 失败: %w", err)
	}
	if strings.TrimSpace(baseDir) == "" {
		return "", errors.New("未配置 workspace 基目录（值为空）")
	}
	return baseDir, nil
}
