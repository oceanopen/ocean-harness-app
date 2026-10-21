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

func TestFormatIssueProfile(t *testing.T) {
	const id = "0197c0a2-7b6a-7333-8b1a-2f4e1d6c9a01"
	for slot, want := range map[int]string{
		1: "issue-" + id,
		2: "issue-" + id + "-2",
		5: "issue-" + id + "-5",
	} {
		got, err := FormatIssueProfile(id, slot)
		if err != nil || got != want {
			t.Fatalf("FormatIssueProfile(%d) = %q, %v; want %q", slot, got, err, want)
		}
	}
	for _, slot := range []int{0, 6, -1} {
		if _, err := FormatIssueProfile(id, slot); err == nil {
			t.Fatalf("FormatIssueProfile(%d) 越界未被拒绝", slot)
		}
	}
}

func TestParseIssueProfile(t *testing.T) {
	const id = "0197c0a2-7b6a-7333-8b1a-2f4e1d6c9a01"
	cases := []struct {
		profile string
		issueID string
		slot    int
		ok      bool
	}{
		{profile: "issue-" + id, issueID: id, slot: 1, ok: true},
		{profile: "issue-" + id + "-2", issueID: id, slot: 2, ok: true},
		{profile: "issue-" + id + "-5", issueID: id, slot: 5, ok: true},
		{profile: "issue-" + id + "-c9a0", issueID: id + "-c9a0", slot: 1, ok: true}, // 非单数字尾缀不拆槽位：整体为基座
		{profile: "workspace-w1"},
		{profile: "default"},
		{profile: "issue-"},
		{profile: "issue-" + id + "-1"}, // -1 非合法槽位后缀
		{profile: "issue-" + id + "-6"}, // -6 越界
		{profile: "issue-" + id + "-22", issueID: id + "-22", slot: 1, ok: true}, // 多位尾缀非槽位形态：整体为基座
	}
	for _, tc := range cases {
		gotID, gotSlot, ok := ParseIssueProfile(tc.profile)
		if ok != tc.ok || gotID != tc.issueID || gotSlot != tc.slot {
			t.Fatalf("ParseIssueProfile(%q) = (%q, %d, %v); want (%q, %d, %v)", tc.profile, gotID, gotSlot, ok, tc.issueID, tc.slot, tc.ok)
		}
	}
}

func TestProfilePrefsRoundTrip(t *testing.T) {
	dir := t.TempDir()

	// 文件缺失 = 无头缺省（新建 profile 缺省口径，D10 修订）。
	prefs, err := ReadProfilePrefs(dir)
	if err != nil || !prefs.Headless {
		t.Fatalf("缺省偏好 = %+v, %v; want headless=true", prefs, err)
	}

	// 已落盘 prefs 的既有 profile 行为不变：显式 false 回读 false。
	if err := WriteProfilePrefs(dir, ProfilePrefs{Headless: false}); err != nil {
		t.Fatalf("WriteProfilePrefs: %v", err)
	}
	prefs, err = ReadProfilePrefs(dir)
	if err != nil || prefs.Headless {
		t.Fatalf("回读偏好 = %+v, %v; want headless=false", prefs, err)
	}

	// 写入后回读 true。
	if err := WriteProfilePrefs(dir, ProfilePrefs{Headless: true}); err != nil {
		t.Fatalf("WriteProfilePrefs: %v", err)
	}
	prefs, err = ReadProfilePrefs(dir)
	if err != nil || !prefs.Headless {
		t.Fatalf("回读偏好 = %+v, %v; want headless=true", prefs, err)
	}

	// workspaceId 与 headless 同文件互不干扰：双字段一并落盘回读。
	if err := WriteProfilePrefs(dir, ProfilePrefs{Headless: false, WorkspaceID: "ws-1"}); err != nil {
		t.Fatalf("WriteProfilePrefs: %v", err)
	}
	prefs, err = ReadProfilePrefs(dir)
	if err != nil || prefs.Headless || prefs.WorkspaceID != "ws-1" {
		t.Fatalf("回读偏好 = %+v, %v; want headless=false workspaceId=ws-1", prefs, err)
	}
}
