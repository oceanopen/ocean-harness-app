package browser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withTestRoot 将 browser 根目录指向临时目录（收尾恢复），返回该根。
func withTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	SetRoot(root)
	t.Cleanup(func() { SetRoot("") })
	return root
}

func TestValidateProfileName(t *testing.T) {
	for name, ok := range map[string]bool{
		"default":                           true,
		"site-acc_1":                        true,
		"a":                                 true,
		"Google-2":                          true,
		"":                                  false,
		"-leading":                          false, // 前导连字符（防命令行 flag 注入语义）
		"_leading":                          false,
		".hidden":                           false,
		"has/slash":                         false, // 路径穿越
		"../escape":                         false,
		"..":                                false,
		"has space":                         false,
		"中文":                                false,
		"con":                               false, // Windows 保留设备名
		"NUL":                               false, // 大小写不敏感
		"com1":                              false,
		"lpt9":                              false,
		"console":                           true,  // 含保留名前缀但非整名，合法
		"toolong" + strings.Repeat("x", 64): false, // >64
	} {
		err := ValidateProfileName(name)
		if (err == nil) != ok {
			t.Errorf("ValidateProfileName(%q) err = %v, want ok=%v", name, err, ok)
		}
	}
}

func TestProfileDirUnderRoot(t *testing.T) {
	root := withTestRoot(t)
	dir, err := ProfileDir("default")
	if err != nil {
		t.Fatalf("ProfileDir: %v", err)
	}
	if want := filepath.Join(root, "profiles", "default"); dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
	if _, err := ProfileDir("../escape"); err == nil {
		t.Fatal("路径穿越 profile 名未被拒绝")
	}
}

func TestDownloadsDirCreated(t *testing.T) {
	root := withTestRoot(t)
	dir, err := DownloadsDir("default")
	if err != nil {
		t.Fatalf("DownloadsDir: %v", err)
	}
	if want := filepath.Join(root, "downloads", "default"); dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("下载目录未创建: %v", err)
	}
}

func TestProfilePrefsRoundTrip(t *testing.T) {
	dir := t.TempDir()

	// 文件缺失 = 有头缺省零值。
	prefs, err := ReadProfilePrefs(dir)
	if err != nil || prefs.Headless {
		t.Fatalf("缺省偏好 = %+v, %v; want 零值", prefs, err)
	}

	// 写入后回读。
	if err := WriteProfilePrefs(dir, ProfilePrefs{Headless: true}); err != nil {
		t.Fatalf("WriteProfilePrefs: %v", err)
	}
	prefs, err = ReadProfilePrefs(dir)
	if err != nil || !prefs.Headless {
		t.Fatalf("回读偏好 = %+v, %v; want headless=true", prefs, err)
	}
}
