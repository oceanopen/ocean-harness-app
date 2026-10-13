package appdb

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

// 环境类哨兵错误直测：未注入（空路径）与文件不存在两条前置守卫，errors.Is 可判别
// （调用方据此映射各自文案）。
func TestReadAppConfigValueEnvErrors(t *testing.T) {
	if _, err := ReadAppConfigValue("", KeyGithubPAT); !errors.Is(err, ErrAppDbNotInjected) {
		t.Fatalf("空路径 want ErrAppDbNotInjected, got %v", err)
	}
	if _, err := ReadAppConfigValue(filepath.Join(t.TempDir(), "no-such.db"), KeyGithubPAT); !errors.Is(err, ErrAppDbMissing) {
		t.Fatalf("文件不存在 want ErrAppDbMissing, got %v", err)
	}
}

// key 未设置返回 ("", nil)：空值语义由调用方区分（与本包读成功路径的契约）。
// 以带表的真实 sqlite 文件验证完整打开→查询路径（排除注入/守卫短路）。
func TestReadAppConfigValueKeyNotSet(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "app.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("打开临时 sqlite: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE app_config (key TEXT PRIMARY KEY, value TEXT NOT NULL)"); err != nil {
		t.Fatalf("建表: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("关闭临时 sqlite: %v", err)
	}

	value, err := ReadAppConfigValue(dbPath, "no_such_key")
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if value != "" {
		t.Fatalf("want empty value, got %q", value)
	}
}
