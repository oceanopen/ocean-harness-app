package mcputil

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/glebarez/sqlite"

	"ocean-harness/src-server/internal/config"
	"ocean-harness/src-server/internal/global"
)

// createTestAppDb 在临时目录按 Rust 侧 schema（app_config KV 表）造一个 app.db 并写入给定
// 键值（kv 为 nil 时不插入目标 key，模拟"设置页未设置"）。
func createTestAppDb(t *testing.T, kv map[string]string) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	db, err := sql.Open("sqlite", dbPath) // 测试构造用读写连接（生产读取走 mode=ro）
	if err != nil {
		t.Fatalf("open test app.db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE IF NOT EXISTS app_config (key TEXT PRIMARY KEY, value TEXT NOT NULL)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	for k, v := range kv {
		if _, err := db.Exec("INSERT OR REPLACE INTO app_config (key, value) VALUES (?, ?)", k, v); err != nil {
			t.Fatalf("insert %q: %v", k, err)
		}
	}
	return dbPath
}

func TestReadWorkspaceBaseDir(t *testing.T) {
	origConfig := global.Config
	t.Cleanup(func() { global.Config = origConfig })

	t.Run("正常读取", func(t *testing.T) {
		global.Config = &config.Config{AppDbPath: createTestAppDb(t, map[string]string{
			"workspace_base_dir": "/Users/foo/workspaces",
		})}
		got, err := ReadWorkspaceBaseDir()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "/Users/foo/workspaces" {
			t.Fatalf("got %q, want /Users/foo/workspaces", got)
		}
	})

	t.Run("AppDbPath 未注入", func(t *testing.T) {
		global.Config = &config.Config{}
		if _, err := ReadWorkspaceBaseDir(); err == nil || err.Error() != "未配置 workspace 基目录（GO_SERVER_APP_DB 未注入）" {
			t.Fatalf("want 未注入 error, got %v", err)
		}
	})

	t.Run("app.db 文件不存在", func(t *testing.T) {
		global.Config = &config.Config{AppDbPath: filepath.Join(t.TempDir(), "absent.db")}
		if _, err := ReadWorkspaceBaseDir(); err == nil || err.Error() != "未配置 workspace 基目录（app.db 不存在）" {
			t.Fatalf("want 不存在 error, got %v", err)
		}
	})

	t.Run("key 未设置", func(t *testing.T) {
		global.Config = &config.Config{AppDbPath: createTestAppDb(t, map[string]string{
			"language": "zh-CN", // 有其他 key、无 workspace_base_dir
		})}
		if _, err := ReadWorkspaceBaseDir(); err == nil || err.Error() != "未配置 workspace 基目录（设置页未设置）" {
			t.Fatalf("want 未设置 error, got %v", err)
		}
	})

	t.Run("值为空白", func(t *testing.T) {
		global.Config = &config.Config{AppDbPath: createTestAppDb(t, map[string]string{
			"workspace_base_dir": "  ",
		})}
		if _, err := ReadWorkspaceBaseDir(); err == nil || err.Error() != "未配置 workspace 基目录（值为空）" {
			t.Fatalf("want 值为空 error, got %v", err)
		}
	})
}
