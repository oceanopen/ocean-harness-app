package browser

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

/*
 * 共享登录态编排测试：经可控假引擎验证 spawn 注入时序（先于 ready）、hash 跳重、
 * 缺失/失败降级、Close 导出合并回写。
 */

// authSeedShared 预置工作空间共享登录态文件（返回路径）。
func authSeedShared(t *testing.T, wsID string, state StorageState) string {
	t.Helper()
	path, err := AuthStatePath(wsID)
	if err != nil {
		t.Fatalf("AuthStatePath: %v", err)
	}
	if err := writeSharedStateAtomic(path, state); err != nil {
		t.Fatalf("seed 共享文件: %v", err)
	}
	return path
}

// authReadShared 读回共享登录态文件（fatal on error）。
func authReadShared(t *testing.T, path string) StorageState {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读共享文件: %v", err)
	}
	var state StorageState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("解析共享文件: %v", err)
	}
	return state
}

// withAuthResolver 注入 issue→workspace resolver（收尾恢复 nil）。
func withAuthResolver(t *testing.T, fn func(ctx context.Context, issueID string) (string, error)) {
	t.Helper()
	SetIssueWorkspaceResolver(fn)
	t.Cleanup(func() { SetIssueWorkspaceResolver(nil) })
}

// authToolRecorder 记账 storage 工具调用（engineStateJSON 非空时模拟引擎写 staging）。
type authToolRecorder struct {
	mu          sync.Mutex
	setCalls    int
	exportCalls int
	lastInject  string
}

func (r *authToolRecorder) bind(ctl *fakeEngineCtl, engineStateJSON string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ctl.toolFn = func(tool string, args map[string]any) (string, bool) {
		switch tool {
		case EngineToolSetStorageState:
			r.mu.Lock()
			r.setCalls++
			r.lastInject, _ = args["filename"].(string)
			r.mu.Unlock()
			return "restored", false
		case EngineToolStorageState:
			if engineStateJSON != "" {
				fn, _ := args["filename"].(string)
				_ = os.WriteFile(fn, []byte(engineStateJSON), 0o600)
			}
			r.mu.Lock()
			r.exportCalls++
			r.mu.Unlock()
			return "saved", false
		}
		return "ok: " + tool, false
	}
}

func (r *authToolRecorder) stats() (setCalls, exportCalls int, lastInject string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.setCalls, r.exportCalls, r.lastInject
}

func TestAuthInjectBeforeReadyAndMergeOnClose(t *testing.T) {
	m := newTestManager(t)
	ctl := withEngineCtl(t)
	rec := &authToolRecorder{}
	rec.bind(ctl, `{"cookies":[{"name":"engine_c","value":"v","domain":".e.com","path":"/","expires":-1,"httpOnly":false,"secure":false,"sameSite":"Lax"}],"origins":[]}`)
	withAuthResolver(t, func(_ context.Context, issueID string) (string, error) { return "ws1", nil })
	shared := authSeedShared(t, "ws1", StorageState{Cookies: []StorageCookie{
		{Name: "shared_c", Value: "s", Domain: ".s.com", Path: "/", Expires: -1, SameSite: "Lax"},
	}})

	// Ensure 返回即就绪——注入（set_storage_state）必然已完成（置 ready 前同步执行）。
	sv, err := m.Ensure(context.Background(), "issue-iss1", nil)
	if err != nil || sv.Status != StatusReady {
		t.Fatalf("Ensure = %v, %v; want ready", sv.Status, err)
	}
	setCalls, _, lastInject := rec.stats()
	if setCalls != 1 {
		t.Fatalf("set_storage_state 调用数 = %d, want 1", setCalls)
	}
	wantStaging := filepath.Join(mustDownloadsDir(t, "issue-iss1"), stagingInjectFile)
	if lastInject != wantStaging {
		t.Fatalf("staging 路径 = %q, want %q", lastInject, wantStaging)
	}
	if _, err := os.Stat(wantStaging); !os.IsNotExist(err) {
		t.Fatalf("注入后 staging 未清理: %v", err)
	}

	// Close 收尾导出：假引擎写 staging（engine_c）→ 与共享文件合并回写（shared_c 保留）。
	if err := m.Close("issue-iss1"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	setCalls, exportCalls, _ := rec.stats()
	if exportCalls != 1 {
		t.Fatalf("storage_state 导出调用数 = %d, want 1", exportCalls)
	}
	merged := authReadShared(t, shared)
	if len(merged.Cookies) != 2 {
		t.Fatalf("合并后 cookie 数 = %d, want 2: %+v", len(merged.Cookies), merged.Cookies)
	}
	if merged.Cookies[0].Name != "shared_c" || merged.Cookies[1].Name != "engine_c" {
		t.Fatalf("合并结果错误: %+v", merged.Cookies)
	}
}

func TestAuthInjectSkips(t *testing.T) {
	m := newTestManager(t)
	ctl := withEngineCtl(t)
	rec := &authToolRecorder{}
	rec.bind(ctl, "")
	withAuthResolver(t, func(_ context.Context, issueID string) (string, error) { return "ws1", nil })

	// 共享文件缺失：跳过注入，会话照常就绪。
	if sv, err := m.Ensure(context.Background(), "issue-iss1", nil); err != nil || sv.Status != StatusReady {
		t.Fatalf("缺失文件 Ensure = %v, %v; want ready", sv.Status, err)
	}
	if setCalls, _, _ := rec.stats(); setCalls != 0 {
		t.Fatalf("缺失文件不应注入: setCalls = %d", setCalls)
	}

	// 首次播种后 spawn：注入一次。
	authSeedShared(t, "ws1", StorageState{Cookies: []StorageCookie{
		{Name: "c", Value: "v", Domain: ".s.com", Path: "/", Expires: -1, SameSite: "Lax"},
	}})
	if err := m.Close("issue-iss1"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if sv, err := m.Ensure(context.Background(), "issue-iss1", nil); err != nil || sv.Status != StatusReady {
		t.Fatalf("播种后 Ensure = %v, %v; want ready", sv.Status, err)
	}
	if setCalls, _, _ := rec.stats(); setCalls != 1 {
		t.Fatalf("播种后应注入: setCalls = %d", setCalls)
	}

	// hash 跳重：同内容共享文件下释放后重开，不二次注入（假引擎导出不产 staging，
	// Close 导出自然失败、共享文件内容保持不变）。
	if err := m.Close("issue-iss1"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if sv, err := m.Ensure(context.Background(), "issue-iss1", nil); err != nil || sv.Status != StatusReady {
		t.Fatalf("同内容重开 Ensure = %v, %v; want ready", sv.Status, err)
	}
	if setCalls, _, _ := rec.stats(); setCalls != 1 {
		t.Fatalf("同内容重开不应二次注入: setCalls = %d", setCalls)
	}

	// 共享文件内容变化（他山会话登录过）：重开须重新注入。
	authSeedShared(t, "ws1", StorageState{Cookies: []StorageCookie{
		{Name: "c2", Value: "v2", Domain: ".s.com", Path: "/", Expires: -1, SameSite: "Lax"},
	}})
	if err := m.Close("issue-iss1"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if sv, err := m.Ensure(context.Background(), "issue-iss1", nil); err != nil || sv.Status != StatusReady {
		t.Fatalf("内容变化后重开 Ensure = %v, %v; want ready", sv.Status, err)
	}
	if setCalls, _, _ := rec.stats(); setCalls != 2 {
		t.Fatalf("共享文件变化后应重新注入: setCalls = %d", setCalls)
	}
}

func TestAuthInjectFailureDegrades(t *testing.T) {
	m := newTestManager(t)
	ctl := withEngineCtl(t)
	ctl.mu.Lock()
	ctl.toolFn = func(tool string, args map[string]any) (string, bool) {
		if tool == EngineToolSetStorageState {
			return "engine exploded", true
		}
		return "ok: " + tool, false
	}
	ctl.mu.Unlock()
	withAuthResolver(t, func(_ context.Context, issueID string) (string, error) { return "ws1", nil })
	authSeedShared(t, "ws1", StorageState{Cookies: []StorageCookie{
		{Name: "c", Value: "v", Domain: ".s.com", Path: "/", Expires: -1, SameSite: "Lax"},
	}})

	// 注入失败：会话照常就绪（降级 profile 自管），不阻断 Ensure。
	sv, err := m.Ensure(context.Background(), "issue-iss1", nil)
	if err != nil || sv.Status != StatusReady {
		t.Fatalf("注入失败 Ensure = %v, %v; want ready", sv.Status, err)
	}
}

func TestAuthNoNamespaceNoSync(t *testing.T) {
	m := newTestManager(t)
	ctl := withEngineCtl(t)
	rec := &authToolRecorder{}
	rec.bind(ctl, "")
	authSeedShared(t, "ws1", StorageState{Cookies: []StorageCookie{
		{Name: "c", Value: "v", Domain: ".s.com", Path: "/", Expires: -1, SameSite: "Lax"},
	}})

	// resolver 未注入（nil）：issue- 前缀无共享，零注入零导出。
	if sv, err := m.Ensure(context.Background(), "issue-iss1", nil); err != nil || sv.Status != StatusReady {
		t.Fatalf("nil resolver Ensure = %v, %v; want ready", sv.Status, err)
	}
	// workspace- 前缀直通（无需 resolver）：注入发生。
	if sv, err := m.Ensure(context.Background(), "workspace-ws1", nil); err != nil || sv.Status != StatusReady {
		t.Fatalf("workspace 前缀 Ensure = %v, %v; want ready", sv.Status, err)
	}
	setCalls, _, lastInject := rec.stats()
	if setCalls != 1 {
		t.Fatalf("workspace 前缀应直通注入: setCalls = %d", setCalls)
	}
	if !strings.Contains(lastInject, "workspace-ws1") {
		t.Fatalf("staging 未按 profile 隔离: %q", lastInject)
	}
	// default 自定义名无命名空间：无注入。
	if err := m.Close("workspace-ws1"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if sv, err := m.Ensure(context.Background(), "default", nil); err != nil || sv.Status != StatusReady {
		t.Fatalf("default Ensure = %v, %v; want ready", sv.Status, err)
	}
	if setCalls, _, _ := rec.stats(); setCalls != 1 {
		t.Fatalf("default 不应注入: setCalls = %d", setCalls)
	}
}

// mustDownloadsDir 测试取 downloads 目录（error 即 fatal）。
func mustDownloadsDir(t *testing.T, profile string) string {
	t.Helper()
	dir, err := DownloadsDir(profile)
	if err != nil {
		t.Fatalf("DownloadsDir: %v", err)
	}
	return dir
}
