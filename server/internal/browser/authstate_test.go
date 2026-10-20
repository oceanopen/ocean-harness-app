package browser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var mergeNow = time.Unix(1700000000, 0)

func TestMergeStorageStateCookies(t *testing.T) {
	shared := StorageState{Cookies: []StorageCookie{
		{Name: "a", Domain: ".x.com", Path: "/", Expires: -1, Value: "old"},
		{Name: "b", Domain: ".x.com", Path: "/", Expires: -1, Value: "keep"},
		{Name: "dead", Domain: ".x.com", Path: "/", Expires: float64(mergeNow.Unix() - 100)},
	}}
	engine := StorageState{Cookies: []StorageCookie{
		{Name: "a", Domain: ".x.com", Path: "/", Expires: -1, Value: "new"},
		{Name: "c", Domain: ".y.com", Path: "/", Expires: -1, Value: "fresh"},
		{Name: "dead2", Domain: ".y.com", Path: "/", Expires: float64(mergeNow.Unix() - 1)},
	}}
	merged := mergeStorageState(shared, engine, mergeNow)

	if len(merged.Cookies) != 3 {
		t.Fatalf("cookie 数 = %d, want 3: %+v", len(merged.Cookies), merged.Cookies)
	}
	if merged.Cookies[0].Value != "new" {
		t.Fatalf("同名 cookie 未被引擎侧覆盖: %+v", merged.Cookies[0])
	}
	if merged.Cookies[1].Value != "keep" {
		t.Fatalf("共享文件独有 cookie 未保留: %+v", merged.Cookies[1])
	}
	if merged.Cookies[2].Value != "fresh" {
		t.Fatalf("引擎新增 cookie 未追加: %+v", merged.Cookies[2])
	}
}

func TestMergeStorageStateOrigins(t *testing.T) {
	shared := StorageState{Origins: []StorageOrigin{
		{Origin: "https://a.com", LocalStorage: []StorageKV{
			{Name: "k1", Value: "old"},
			{Name: "k2", Value: "keep"},
		}},
		{Origin: "https://gone.com", LocalStorage: []StorageKV{{Name: "k", Value: "v"}}},
	}}
	engine := StorageState{Origins: []StorageOrigin{
		{Origin: "https://a.com", LocalStorage: []StorageKV{{Name: "k1", Value: "new"}}},
		{Origin: "https://b.com", LocalStorage: []StorageKV{{Name: "k3", Value: "fresh"}}},
	}}
	merged := mergeStorageState(shared, engine, mergeNow)

	if len(merged.Origins) != 3 {
		t.Fatalf("origin 数 = %d, want 3: %+v", len(merged.Origins), merged.Origins)
	}
	kvs := merged.Origins[0].LocalStorage
	if len(kvs) != 2 || kvs[0].Value != "new" || kvs[1].Value != "keep" {
		t.Fatalf("localStorage 合并错误: %+v", kvs)
	}
	if merged.Origins[2].Origin != "https://b.com" {
		t.Fatalf("引擎新增 origin 未追加: %+v", merged.Origins)
	}
}

func TestReadSharedState(t *testing.T) {
	// 文件缺失：exists=false。
	state, exists, err := readSharedState(filepath.Join(t.TempDir(), "nope.json"))
	if exists || err != nil || len(state.Cookies) != 0 {
		t.Fatalf("缺失文件读取 = %+v, %v, %v; want 空态 exists=false", state, exists, err)
	}

	// 损坏 JSON：按空态 + 错误（错误文案不含文件内容）。
	corrupt := filepath.Join(t.TempDir(), "corrupt.json")
	os.WriteFile(corrupt, []byte("{broken"), 0o600)
	state, exists, err = readSharedState(corrupt)
	if !exists || err == nil || len(state.Cookies) != 0 {
		t.Fatalf("损坏文件读取 = %+v, %v, %v; want 空态 exists=true err!=nil", state, exists, err)
	}
	if strings.Contains(err.Error(), "{broken") {
		t.Fatalf("错误文案泄漏文件内容: %v", err)
	}
}

func TestWriteSharedStateAtomicRoundTrip(t *testing.T) {
	withTestRoot(t)
	path, err := AuthStatePath("ws-1")
	if err != nil {
		t.Fatalf("AuthStatePath: %v", err)
	}
	want := StorageState{
		Cookies: []StorageCookie{{Name: "a", Domain: ".x.com", Path: "/", Expires: -1, SameSite: "Lax"}},
		Origins: []StorageOrigin{{Origin: "https://a.com", LocalStorage: []StorageKV{{Name: "k", Value: "v"}}}},
	}
	if err := writeSharedStateAtomic(path, want); err != nil {
		t.Fatalf("writeSharedStateAtomic: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("共享文件未生成: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("权限 = %o, want 600", perm)
	}
	got, exists, err := readSharedState(path)
	if err != nil || !exists || len(got.Cookies) != 1 || len(got.Origins) != 1 || got.Origins[0].LocalStorage[0].Value != "v" {
		t.Fatalf("round-trip = %+v, %v, %v", got, exists, err)
	}

	// 残留 tmp 检查：写入完成后目录内只应有共享文件本体。
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("目录残留临时文件: %d 项", len(entries))
	}
}

func TestAuthStatePath(t *testing.T) {
	root := withTestRoot(t)
	path, err := AuthStatePath("a1-_X")
	if err != nil {
		t.Fatalf("AuthStatePath: %v", err)
	}
	if want := filepath.Join(root, "auth-states", "a1-_X.json"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	if _, err := AuthStatePath("../escape"); err == nil {
		t.Fatal("路径穿越 workspace id 未被拒绝")
	}
}

func TestParseProfileNamespace(t *testing.T) {
	issueID := "0197abcd-0000-7000-8000-000000000000"
	for profile, want := range map[string][2]string{
		"issue-" + issueID: {IssueProfilePrefix, issueID},
		"workspace-ws1":    {WorkspaceProfilePrefix, "ws1"},
		"default":          {"", ""},
		"issue-":           {"", ""},
		"custom-profile":   {"", ""},
		"prefixissue-x":    {"", ""},
		"workspace-":       {"", ""},
	} {
		prefix, id := ParseProfileNamespace(profile)
		if prefix != want[0] || id != want[1] {
			t.Errorf("ParseProfileNamespace(%q) = (%q, %q), want (%q, %q)", profile, prefix, id, want[0], want[1])
		}
	}
}
