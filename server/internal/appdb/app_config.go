/*
 * Package appdb Rust 侧共享 app.db（app_config KV 表）的 Go 只读访问：跨端 key 常量
 * 与短连接只读查询；写入方唯一（应用设置页，经 Rust），Go 各域（mcp_util/acpsession…）按需读取、无缓存。
 * 本包不依赖 global（global 持各域 Manager，反向依赖成环）——dbPath 由调用方注入（mcp_util 自取 global.Config，acpsession 经 Manager 构造注入）。
 */
package appdb

import (
	"database/sql"
	"errors"
	"fmt"
	"os"

	/*
	 * database/sql 驱动注册（driver name "sqlite"，与 initialize/sqlite.go 同源 glebarez/modernc，
	 * 不新增依赖）。仅本文件以原生 database/sql 短连接方式使用（读一行即关，无需 gorm 装配）。
	 */
	_ "github.com/glebarez/sqlite"
)

/*
 * 跨端 key 常量：对齐 Rust 侧 app_config KV 表（app/src/shared/app_config.rs，经
 * specta .constant() 单源导出到前端 bindings.ts）。Go 侧字符串常量、无生成机制，变更须两端同步。
 */
const (
	KeyGithubPAT                    = "github_pat"
	KeyAcpSessionIdleTimeoutMinutes = "acp_session_idle_timeout_minutes"
	KeyBotIncomingPolicy            = "bot_incoming_policy"
)

// 环境类哨兵错误（可被调用方 errors.Is 判别后映射为各自面向用户的文案）。
var (
	ErrAppDbNotInjected = errors.New("app.db 路径未注入（GO_SERVER_APP_DB 未配置）")
	ErrAppDbMissing     = errors.New("app.db 不存在")
)

/*
 * ReadAppConfigValue 从 Rust 共享的 app.db（app_config KV 表）只读读取一个 key；应用设置页是唯一写入方，Go 只读，key 未设置返回 ("", nil)（空值语义由调用方区分）。
 * 每次调用短连接：os.Stat 前置守卫（缺文件不创建、错误可区分）+ file: URI 的 mode=ro（只读句柄，绝不持有写锁）+ busy_timeout（容忍与 Rust 写端瞬时锁冲突；rollback journal 下读一行为微秒级）。
 * 读一行即关、无缓存——用户改设置后下一次读取即生效。
 */
func ReadAppConfigValue(dbPath, key string) (string, error) {
	if dbPath == "" {
		return "", ErrAppDbNotInjected
	}
	if _, err := os.Stat(dbPath); err != nil {
		return "", ErrAppDbMissing
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
