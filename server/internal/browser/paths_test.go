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
		"default":                            true,
		"site-acc_1":                         true,
		"a":                                  true,
		"Google-2":                           true,
		"":                                   false,
		"-leading":                           false, // 前导连字符（防命令行 flag 注入语义）
		"_leading":                           false,
		".hidden":                            false,
		"has/slash":                          false, // 路径穿越
		"../escape":                          false,
		"..":                                 false,
		"has space":                          false,
		"中文":                                 false,
		"con":                                false, // Windows 保留设备名
		"NUL":                                false, // 大小写不敏感
		"com1":                               false,
		"lpt9":                               false,
		"console":                            true,  // 含保留名前缀但非整名，合法
		"toolong" + strings.Repeat("x", 128): false, // >128
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

func TestFormatConversationProfile(t *testing.T) {
	const taskID = "0197c0a2-7b6a-7333-8b1a-2f4e1d6c9a01"
	const convID = "0197c0a2-7b6a-7333-8b1a-2f4e1d6c9a02"
	for slot, want := range map[int]string{
		1:  "issue-" + taskID + SessionSegment + convID + "-01",
		7:  "issue-" + taskID + SessionSegment + convID + "-07",
		10: "issue-" + taskID + SessionSegment + convID + "-10",
	} {
		got, err := FormatConversationProfile(ConversationOwnerTask, taskID, convID, slot)
		if err != nil || got != want {
			t.Fatalf("FormatConversationProfile(%d) = %q, %v; want %q", slot, got, err, want)
		}
	}
	// workspace 族与双 uuid 全名合法（长度上限覆盖点）。
	got, err := FormatConversationProfile(ConversationOwnerWorkspace, "w1", convID, 2)
	if err != nil || got != "workspace-w1"+SessionSegment+convID+"-02" {
		t.Fatalf("workspace 族 format = %q, %v", got, err)
	}
	for _, slot := range []int{0, 11, -1} {
		if _, err := FormatConversationProfile(ConversationOwnerTask, taskID, convID, slot); err == nil {
			t.Fatalf("FormatConversationProfile(%d) 越界未被拒绝", slot)
		}
	}
	if _, err := FormatConversationProfile("bot", taskID, convID, 1); err == nil {
		t.Fatal("未知归属型别未被拒绝")
	}
}

func TestParseConversationProfile(t *testing.T) {
	const taskID = "0197c0a2-7b6a-7333-8b1a-2f4e1d6c9a01"
	const convID = "0197c0a2-7b6a-7333-8b1a-2f4e1d6c9a02"
	base := "issue-" + taskID + SessionSegment
	cases := []struct {
		profile string
		prefix  string
		ownerID string
		convID  string
		slot    int
		ok      bool
	}{
		{profile: base + convID + "-01", prefix: TaskProfilePrefix, ownerID: taskID, convID: convID, slot: 1, ok: true},
		{profile: base + convID + "-10", prefix: TaskProfilePrefix, ownerID: taskID, convID: convID, slot: 10, ok: true},
		{profile: "workspace-w1" + SessionSegment + "c2-07", prefix: WorkspaceProfilePrefix, ownerID: "w1", convID: "c2", slot: 7, ok: true},
		{profile: base + convID + "-c9a0"}, // 非两位数字尾缀不拆槽位：判非会话实例
		{profile: "issue-" + taskID},       // 无会话段
		{profile: "workspace-w1"},          // 无会话段
		{profile: "default"},
		{profile: "issue-"},
		{profile: base + convID},                              // 无 -NN 后缀
		{profile: base + convID + "-00"},                      // 越界
		{profile: base + convID + "-11"},                      // 越界
		{profile: base + convID + "-1"},                       // 单数字尾
		{profile: base},                                       // 空 conversationId
		{profile: "custom" + SessionSegment + convID + "-01"}, // 无命名空间前缀
		{profile: "issue-" + SessionSegment + convID + "-01"}, // 空 ownerID
	}
	for _, tc := range cases {
		gotPrefix, gotOwner, gotConv, gotSlot, ok := ParseConversationProfile(tc.profile)
		if ok != tc.ok || gotPrefix != tc.prefix || gotOwner != tc.ownerID || gotConv != tc.convID || gotSlot != tc.slot {
			t.Fatalf("ParseConversationProfile(%q) = (%q, %q, %q, %d, %v); want (%q, %q, %q, %d, %v)",
				tc.profile, gotPrefix, gotOwner, gotConv, gotSlot, ok, tc.prefix, tc.ownerID, tc.convID, tc.slot, tc.ok)
		}
	}
}

func TestConversationQuotaBase(t *testing.T) {
	const taskID = "0197c0a2-7b6a-7333-8b1a-2f4e1d6c9a01"
	const convID = "0197c0a2-7b6a-7333-8b1a-2f4e1d6c9a02"
	base := "issue-" + taskID + SessionSegment + convID
	// 槽位名、越界两位尾数（-00/-11/-99）、无后缀名同归一会话配额组。
	for _, profile := range []string{
		base + "-01",
		base + "-10",
		base + "-00",
		base + "-11",
		base + "-99",
		base,
	} {
		got, ok := conversationQuotaBase(profile)
		if !ok || got != base {
			t.Fatalf("conversationQuotaBase(%q) = (%q, %v); want %q", profile, got, ok, base)
		}
	}
	for _, profile := range []string{
		"default",
		"issue-" + taskID,
		"workspace-w1",
		"custom" + SessionSegment + convID + "-01", // 无命名空间前缀
		"issue-" + SessionSegment + convID + "-01", // 空 ownerID
		"issue-" + taskID + SessionSegment,         // 空 conversationId
	} {
		if got, ok := conversationQuotaBase(profile); ok {
			t.Fatalf("conversationQuotaBase(%q) = %q, want ok=false", profile, got)
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

	// 显式 true 与缺省 true 行为一致（结构体仅 headless 一字段，读写闭环）。
	if err := WriteProfilePrefs(dir, ProfilePrefs{Headless: true}); err != nil {
		t.Fatalf("WriteProfilePrefs: %v", err)
	}
	prefs, err = ReadProfilePrefs(dir)
	if err != nil || !prefs.Headless {
		t.Fatalf("回读偏好 = %+v, %v; want headless=true", prefs, err)
	}
}
