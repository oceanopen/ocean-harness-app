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

// 跨端 key 常量：对齐 Rust 侧 app_config KV 表（src-tauri/src/shared/app_config.rs，经
// specta .constant() 单源导出到前端 bindings.ts）。Go 侧字符串常量、无生成机制，变更须两端同步。
const (
	appConfigWorkspaceBaseDirKey = "workspace_base_dir"
	appConfigGithubPATKey        = "github_pat"
)

// 环境类哨兵错误（可被调用方 errors.Is 判别后映射为各自面向用户的文案）。
var (
	errAppDbNotInjected = errors.New("app.db 路径未注入（GO_SERVER_APP_DB 未配置）")
	errAppDbMissing     = errors.New("app.db 不存在")
)

// readAppConfigValue 从 Rust 共享的 app.db（app_config KV 表）只读读取一个 key。
// 应用设置页是唯一写入方，Go 只读。key 未设置返回 ("", nil)（空值语义由调用方区分）。
//
// 每次调用短连接：os.Stat 前置守卫（缺文件不创建、错误可区分）+ file: URI 的 mode=ro
// （只读句柄，绝不持有写锁）+ busy_timeout（容忍与 Rust 写端瞬时锁冲突；rollback journal
// 下读一行为微秒级），读一行即关、无缓存——用户改设置后下一次工具调用即生效。
func readAppConfigValue(key string) (string, error) {
	if global.Config == nil || global.Config.AppDbPath == "" {
		return "", errAppDbNotInjected
	}
	dbPath := global.Config.AppDbPath
	if _, err := os.Stat(dbPath); err != nil {
		return "", errAppDbMissing
	}
	// 注：路径含 "?" / "#" 等 URI 保留字会被截断（macOS app_data_dir 实际不会出现）。
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return "", fmt.Errorf("打开 app.db 失败: %w", err)
	}
	defer db.Close()

	var value string
	err = db.QueryRow("SELECT value FROM app_config WHERE key = ?", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("读取 app_config 失败: %w", err)
	}
	return value, nil
}

// ReadWorkspaceBaseDir 只读 workspace 基目录，供 MCP workspace_status 类工具解析状态文件
// 路径。未注入/缺文件/未设置/空白四态各有独立文案（app_config_reader_test.go 的契约）。
func ReadWorkspaceBaseDir() (string, error) {
	baseDir, err := readAppConfigValue(appConfigWorkspaceBaseDirKey)
	switch {
	case errors.Is(err, errAppDbNotInjected):
		return "", errors.New("未配置 workspace 基目录（GO_SERVER_APP_DB 未注入）")
	case errors.Is(err, errAppDbMissing):
		return "", errors.New("未配置 workspace 基目录（app.db 不存在）")
	case err != nil:
		return "", fmt.Errorf("未配置 workspace 基目录（%s）", err)
	case strings.TrimSpace(baseDir) == "":
		if baseDir == "" {
			return "", errors.New("未配置 workspace 基目录（设置页未设置）")
		}
		return "", errors.New("未配置 workspace 基目录（值为空）")
	}
	return baseDir, nil
}

// ReadGithubPAT 只读 GitHub Personal Access Token（设置 → 个人中心录入），供 github MCP
// 工具（T4.1）构造 API 客户端。未配置返回带指引的中文错误（引导用户去设置页录入）。
func ReadGithubPAT() (string, error) {
	pat, err := readAppConfigValue(appConfigGithubPATKey)
	if err != nil {
		return "", fmt.Errorf("读取 GitHub PAT 失败: %s", err)
	}
	if strings.TrimSpace(pat) == "" {
		return "", errors.New("未配置 GitHub Personal Access Token（请在 设置 → 个人中心 → GitHub 录入）")
	}
	return pat, nil
}
