package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newBareRemote 构造 clone 测试源：work 仓库（main 分支初始提交 + feature/base 分支追加提交），
// 末尾 checkout main 后 clone --bare 生成 bare（HEAD 指向 main，默认分支语义正确）。
// 返回 (work, bare)。git 子命令局部设置 user.name/email，不依赖全局配置。
func newBareRemote(t *testing.T) (work, bare string) {
	t.Helper()
	work, bare = t.TempDir(), t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(work, "init", "-b", "main")
	run(work, "config", "user.email", "test@we.local")
	run(work, "config", "user.name", "test")
	run(work, "commit", "--allow-empty", "-m", "init")
	run(work, "checkout", "-b", "feature/base")
	run(work, "commit", "--allow-empty", "-m", "base-branch-commit")
	run(work, "checkout", "main")
	run(work, "clone", "--bare", work, bare)
	return work, bare
}

func TestClone(t *testing.T) {
	_, bare := newBareRemote(t)

	// 指定分支 + 浅克隆：HEAD 落在基准分支，.git/shallow 存在。--depth 仅在非本地传输生效，
	// 测试源须走 file:// 协议（本地裸路径 clone 时 git 直接忽略 --depth）。
	dir := filepath.Join(t.TempDir(), "repo")
	if err := Clone("file://"+bare, dir, "feature/base", 1, nil); err != nil {
		t.Fatalf("clone 出错: %v", err)
	}
	if got := gitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "feature/base" {
		t.Fatalf("clone 后分支 = %q, want feature/base", got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "shallow")); err != nil {
		t.Errorf("浅克隆应存在 .git/shallow: %v", err)
	}

	// 默认分支（bare HEAD = main）+ 全量。
	dir2 := filepath.Join(t.TempDir(), "repo2")
	if err := Clone(bare, dir2, "", 0, nil); err != nil {
		t.Fatalf("clone 出错: %v", err)
	}
	if got := gitOutput(dir2, "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Fatalf("默认分支 clone 后 = %q, want main", got)
	}
	if _, err := os.Stat(filepath.Join(dir2, ".git", "shallow")); err == nil {
		t.Error("全量 clone 不应有 .git/shallow")
	}

	// 远程不存在的分支 → 报错（且错误含 stderr 摘要）。
	dir3 := filepath.Join(t.TempDir(), "repo3")
	err := Clone(bare, dir3, "no-such-branch", 1, nil)
	if err == nil || !strings.Contains(err.Error(), "no-such-branch") {
		t.Fatalf("分支不存在应报错且带分支名, got: %v", err)
	}
}

func TestRemoteBranchExists(t *testing.T) {
	_, bare := newBareRemote(t)

	ok, err := RemoteBranchExists(bare, "feature/base", nil)
	if err != nil || !ok {
		t.Fatalf("feature/base 应存在: ok=%v err=%v", ok, err)
	}
	ok, err = RemoteBranchExists(bare, "nope", nil)
	if err != nil || ok {
		t.Fatalf("nope 应不存在: ok=%v err=%v", ok, err)
	}
	// 远程不可达（路径不存在）→ error（与分支不存在区分）。
	if _, err := RemoteBranchExists(filepath.Join(t.TempDir(), "absent.git"), "main", nil); err == nil {
		t.Fatal("远程不可达应返回 error 而非 false")
	}
}

func TestBranchHelpers(t *testing.T) {
	_, bare := newBareRemote(t)
	dir := filepath.Join(t.TempDir(), "repo")
	if err := Clone(bare, dir, "feature/base", 0, nil); err != nil {
		t.Fatal(err)
	}

	if !LocalBranchExists(dir, "feature/base") {
		t.Error("feature/base 应存在")
	}
	if LocalBranchExists(dir, "agent_x") {
		t.Error("agent_x 不应存在")
	}
	if LocalBranchExists(t.TempDir(), "main") {
		t.Error("非 git 目录应一律 false")
	}

	// 创建并切换 agent 分支；重复创建报错；可切回基准分支。
	if err := CreateAndCheckoutBranch(dir, "agent_x"); err != nil {
		t.Fatalf("创建 agent 分支出错: %v", err)
	}
	if got := gitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "agent_x" {
		t.Fatalf("HEAD = %q, want agent_x", got)
	}
	if err := CreateAndCheckoutBranch(dir, "agent_x"); err == nil {
		t.Error("分支已存在再创建应报错")
	}
	if err := CheckoutBranch(dir, "feature/base"); err != nil {
		t.Fatalf("切回基准分支出错: %v", err)
	}
	// 从远程引用建本地分支（本地不存在的 main）。
	if err := CreateAndCheckoutFromRemote(dir, "main"); err != nil {
		t.Fatalf("从 origin/main 建分支出错: %v", err)
	}
	if got := gitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Fatalf("HEAD = %q, want main", got)
	}
}

func TestFetchLatest(t *testing.T) {
	work, bare := newBareRemote(t)
	dir := filepath.Join(t.TempDir(), "repo")
	if err := Clone(bare, dir, "feature/base", 1, nil); err != nil {
		t.Fatal(err)
	}

	// work 侧在 feature/base 追加提交并推送到 bare。
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("checkout", "feature/base")
	run("commit", "--allow-empty", "-m", "second")
	run("push", bare, "feature/base")

	// fetch 后 clone 侧 origin/feature/base 应推进到 work 侧最新提交。
	if err := FetchLatest(dir, nil); err != nil {
		t.Fatalf("fetch 出错: %v", err)
	}
	remoteRef, workHead := gitOutput(dir, "rev-parse", "origin/feature/base"), gitOutput(work, "rev-parse", "HEAD")
	if remoteRef == "" || remoteRef != workHead {
		t.Fatalf("fetch 后 origin/feature/base=%s, work HEAD=%s，应一致", remoteRef, workHead)
	}
}
