package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"

	"ocean-harness/server/internal/dal/types"
)

/*
 * globalSSHConfigFixture 模拟全局 ~/.ssh/config：文件头裸配置（隐式 Host *）+ 精确/通配符
 * 多 pattern 段 + 无关段，覆盖 T1.2 的匹配面。
 */
const globalSSHConfigFixture = `AddKeysToAgent yes

Host github.com *.weoa.com
  IdentityFile ~/.ssh/id_ed25519

Host *.weoa.com
  ProxyJump bastion.weoa.com

Host other.com
  User bob
`

func TestTaskWorkspaceSSHHost(t *testing.T) {
	cases := []struct{ url, want string }{
		{"git@github.com:oceanopen/ocean-harness-app.git", "github.com"}, // 实际场景唯一格式
		{"git@git.weoa.com:group/repo.git", "git.weoa.com"},
		{"git@github.com:oceanopen/repo", "github.com"}, // 无 .git 后缀
		{"https://github.com/oceanopen/repo.git", ""},   // https 不参与
		{"ssh://git@github.com:2222/org/repo.git", ""},  // 带协议前缀本期不解析
		{"", ""},
		{"github.com", ""},    // 无 user@ 与 :
		{"git@host", ""},      // 无 :
		{"git@:path.git", ""}, // host 为空
	}
	for _, c := range cases {
		if got := taskWorkspaceSSHHost(c.url); got != c.want {
			t.Errorf("taskWorkspaceSSHHost(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

func TestTaskWorkspaceSSHHostsFromManifest(t *testing.T) {
	manifest := []types.TaskWorkspaceRepoRef{
		{RemoteURL: "git@github.com:org/a.git"},
		{RemoteURL: "git@github.com:org/b.git"},     // 同 host 去重
		{RemoteURL: "https://github.com/org/c.git"}, // 非 SSH 忽略
		{RemoteURL: "git@git.weoa.com:group/d.git"},
	}
	hosts := taskWorkspaceSSHHostsFromManifest(manifest)
	if len(hosts) != 2 || hosts[0] != "github.com" || hosts[1] != "git.weoa.com" {
		t.Fatalf("hosts = %v, want [github.com git.weoa.com]", hosts)
	}
}

func TestTaskWorkspaceBuildSSHConfig(t *testing.T) {
	// 匹配 github.com：隐式 Host *（文件头裸配置）与 github 段保留，weoa 专属段与无关段剔除。
	content, matched, err := taskWorkspaceBuildSSHConfig([]byte(globalSSHConfigFixture), []string{"github.com"})
	if err != nil {
		t.Fatalf("build 出错: %v", err)
	}
	if !matched {
		t.Fatal("github.com 应有匹配段")
	}
	for _, want := range []string{"AddKeysToAgent yes", "Host github.com *.weoa.com", "IdentityFile ~/.ssh/id_ed25519"} {
		if !strings.Contains(content, want) {
			t.Errorf("生成内容缺少 %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "ProxyJump") || strings.Contains(content, "other.com") {
		t.Errorf("生成内容应剔除无匹配段:\n%s", content)
	}

	// 通配符匹配 weoa host：两个 weoa 相关段保留，github 段也因多 pattern 命中保留。
	content, matched, err = taskWorkspaceBuildSSHConfig([]byte(globalSSHConfigFixture), []string{"git.weoa.com"})
	if err != nil || !matched {
		t.Fatalf("通配符匹配失败: matched=%v err=%v", matched, err)
	}
	for _, want := range []string{"ProxyJump bastion.weoa.com", "Host *.weoa.com"} {
		if !strings.Contains(content, want) {
			t.Errorf("生成内容缺少 %q:\n%s", want, content)
		}
	}

	/*
	 * 文件头裸配置（隐式 Host *）匹配一切 host：example.org 也命中该段（设计内行为，
	 * 保留用户全局默认），但其余具体段不出现。
	 */
	content, matched, err = taskWorkspaceBuildSSHConfig([]byte(globalSSHConfigFixture), []string{"example.org"})
	if err != nil {
		t.Fatalf("build 出错: %v", err)
	}
	if !matched || !strings.Contains(content, "AddKeysToAgent yes") || strings.Contains(content, "IdentityFile") {
		t.Fatalf("隐式 Host * 段应保留且具体段剔除: matched=%v\n%s", matched, content)
	}

	// 无任何匹配（fixture 不含文件头裸配置）：matched=false 且内容为空。
	content, matched, err = taskWorkspaceBuildSSHConfig([]byte("Host only.com\n  User bob\n"), []string{"example.org"})
	if err != nil {
		t.Fatalf("build 出错: %v", err)
	}
	if matched || content != "" {
		t.Fatalf("无匹配时 matched=%v content=%q, want false/空", matched, content)
	}
}

func TestTaskWorkspaceRunSSHConfig(t *testing.T) {
	if taskWorkspaceStepRunners[types.IW_STEP_KEY_SSH_CONFIG] == nil {
		t.Fatal("sshConfig runner 未注册（init 自注册失效）")
	}

	// 全局 config 路径替换为临时文件（包级函数变量，测试可替换）。
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "global_ssh_config")
	if err := os.WriteFile(globalPath, []byte(globalSSHConfigFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	origPath := taskWorkspaceSSHGlobalConfigPath
	taskWorkspaceSSHGlobalConfigPath = func() string { return globalPath }
	t.Cleanup(func() { taskWorkspaceSSHGlobalConfigPath = origPath })

	newStep := func(remoteURL string) (*types.TaskWorkspaceState, *types.TaskWorkspaceStep) {
		baseDir := t.TempDir()
		taskID := "01111111-1111-7111-1111-111111111111"
		// createDirs（步骤 1）保证 .ssh 目录先在
		if err := os.MkdirAll(filepath.Join(baseDir, taskID, ".ssh"), 0o755); err != nil {
			t.Fatal(err)
		}
		return &types.TaskWorkspaceState{
			Version: taskWorkspaceStateVersion, TaskID: taskID, BaseDir: baseDir,
			Manifest: []types.TaskWorkspaceRepoRef{{RemoteURL: remoteURL}},
		}, &types.TaskWorkspaceStep{Key: types.IW_STEP_KEY_SSH_CONFIG, Status: types.IW_STATUS_RUNNING}
	}

	// 路径 1：匹配 → 生成文件（0o600），步骤不置 SKIPPED（编排随后置 SUCCESS）。
	state, step := newStep("git@github.com:oceanopen/ocean-harness-app.git")
	if err := taskWorkspaceRunSSHConfig(state, step, zap.NewNop()); err != nil {
		t.Fatalf("runner 出错: %v", err)
	}
	if step.Status == types.IW_STATUS_SKIPPED {
		t.Fatalf("应生成而非跳过: %+v", step)
	}
	data, err := os.ReadFile(filepath.Join(state.BaseDir, state.TaskID, ".ssh", "config"))
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	if !strings.Contains(string(data), "IdentityFile ~/.ssh/id_ed25519") {
		t.Errorf("生成文件内容异常:\n%s", data)
	}
	if info, err := os.Stat(filepath.Join(state.BaseDir, state.TaskID, ".ssh", "config")); err == nil && info.Mode().Perm() != 0o600 {
		t.Errorf("文件权限 = %v, want 0600", info.Mode().Perm())
	}

	// 路径 2：无匹配（HTTPS URL）→ SKIPPED + 说明。
	step = &types.TaskWorkspaceStep{Key: types.IW_STEP_KEY_SSH_CONFIG, Status: types.IW_STATUS_RUNNING}
	if err := taskWorkspaceRunSSHConfig(newStateForURL(t, "https://github.com/oceanopen/repo.git"), step, zap.NewNop()); err != nil {
		t.Fatalf("runner 出错: %v", err)
	}
	if step.Status != types.IW_STATUS_SKIPPED || step.Message == "" {
		t.Fatalf("无匹配应 SKIPPED 且带说明: %+v", step)
	}

	// 路径 3：全局 config 不存在 → SKIPPED + 说明。
	taskWorkspaceSSHGlobalConfigPath = func() string { return filepath.Join(dir, "not_exist") }
	step = &types.TaskWorkspaceStep{Key: types.IW_STEP_KEY_SSH_CONFIG, Status: types.IW_STATUS_RUNNING}
	if err := taskWorkspaceRunSSHConfig(newStateForURL(t, "git@github.com:org/repo.git"), step, zap.NewNop()); err != nil {
		t.Fatalf("runner 出错: %v", err)
	}
	if step.Status != types.IW_STATUS_SKIPPED || step.Message == "" {
		t.Fatalf("全局缺失应 SKIPPED 且带说明: %+v", step)
	}
}

// newStateForURL 构造仅含单个仓库 URL 的最小 state（TestTaskWorkspaceRunSSHConfig 用）。
func newStateForURL(t *testing.T, remoteURL string) *types.TaskWorkspaceState {
	t.Helper()
	return &types.TaskWorkspaceState{
		Version:  taskWorkspaceStateVersion,
		TaskID:   "01111111-1111-7111-1111-111111111111",
		BaseDir:  t.TempDir(),
		Manifest: []types.TaskWorkspaceRepoRef{{RemoteURL: remoteURL}},
	}
}
