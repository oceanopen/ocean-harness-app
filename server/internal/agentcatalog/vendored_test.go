package agentcatalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeStaging 造最小 npx-adapter 资源源：<srcRoot>/<id>/<version>/ 下含一个入口脚本、
// 安装包（package.json bin 字符串形态）与 .bin 相对 symlink，覆盖 copyTree 的三类节点。
func writeStaging(t *testing.T, version string) (srcRoot, dstRoot string) {
	t.Helper()
	srcRoot = t.TempDir()
	dstRoot = t.TempDir()
	root := filepath.Join(srcRoot, "agent-x", version)
	mustWriteFile(t, filepath.Join(root, "entry.js"), "console.log('ok')\n", 0o644)
	mustWriteFile(t, filepath.Join(root, "node_modules", "@scope", "pkg", "dist", "cli.js"), "#!/usr/bin/env node\n", 0o755)
	mustWriteFile(t, filepath.Join(root, "node_modules", "@scope", "pkg", "package.json"),
		`{"name":"@scope/pkg","version":"`+version+`","bin":"dist/cli.js"}`, 0o644)
	binDir := filepath.Join(root, "node_modules", ".bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir .bin: %v", err)
	}
	// 相对 symlink：从 .bin/cli 指向 ../@scope/pkg/dist/cli.js（npm 实际形态）。
	if err := os.Symlink(filepath.Join("..", "@scope", "pkg", "dist", "cli.js"), filepath.Join(binDir, "cli")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	return srcRoot, dstRoot
}

// vendoringEntry 造 npx-adapter 条目（Args 尾部非 flag 参数 = 安装 spec）。
func vendoringEntry(version string) Entry {
	return Entry{
		ID: "agent-x", Code: "agent-x", Version: version,
		Strategy: StrategyNpxAdapter,
		Command:  "npx", Args: []string{"-y", "@scope/pkg@" + version},
		Env:     map[string]string{"FOO": "bar"},
		Enabled: true,
	}
}

func mustWriteFile(t *testing.T, path, content string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), perm); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func markerAt(installDir string) string {
	return filepath.Join(installDir, vendoredMarker)
}

func TestEnsureVendoredHit(t *testing.T) {
	srcRoot, dstRoot := writeStaging(t, "1.0.0")
	entry := vendoringEntry("1.0.0")

	installDir, err := EnsureVendored(entry, srcRoot, dstRoot)
	if err != nil {
		t.Fatalf("EnsureVendored: %v", err)
	}
	// marker 内容 = 版本@平台 triple（npm 平台命名，与 VendoredClaudeBin 定位同口径）。
	wantMarker := "1.0.0@" + claudePlatformTriple()
	if data, err := os.ReadFile(markerAt(installDir)); err != nil || strings.TrimSpace(string(data)) != wantMarker {
		t.Fatalf("marker 内容 = %q, %v, want %q", data, err, wantMarker)
	}
	// 命中即复用：二次调用不重制（installDir 内新落文件原样保留）。
	sentinel := filepath.Join(installDir, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	again, err := EnsureVendored(entry, srcRoot, dstRoot)
	if err != nil {
		t.Fatalf("EnsureVendored 二次: %v", err)
	}
	if again != installDir {
		t.Fatalf("二次安装目录漂移: %s != %s", again, installDir)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("命中复用被重制，sentinel 丢失: %v", err)
	}
}

func TestEnsureVendoredResourceMissing(t *testing.T) {
	_, dstRoot := writeStaging(t, "1.0.0")
	emptySrc := t.TempDir()
	_, err := EnsureVendored(vendoringEntry("1.0.0"), emptySrc, dstRoot)
	if err == nil || !strings.Contains(err.Error(), "资源缺失") {
		t.Fatalf("err = %v, want 资源缺失类错误", err)
	}
}

func TestEnsureVendoredHalfDoneReinstall(t *testing.T) {
	srcRoot, dstRoot := writeStaging(t, "1.0.0")
	entry := vendoringEntry("1.0.0")
	installDir, err := EnsureVendored(entry, srcRoot, dstRoot)
	if err != nil {
		t.Fatalf("EnsureVendored: %v", err)
	}
	// 模拟漂移：marker 篡改 + 夹带垃圾文件。
	if err := os.WriteFile(markerAt(installDir), []byte("0.0.0"), 0o644); err != nil {
		t.Fatalf("篡改 marker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "junk.txt"), []byte("junk"), 0o644); err != nil {
		t.Fatalf("write junk: %v", err)
	}
	reinstalled, err := EnsureVendored(entry, srcRoot, dstRoot)
	if err != nil {
		t.Fatalf("EnsureVendored 重制: %v", err)
	}
	if reinstalled != installDir {
		t.Fatalf("重制目录漂移: %s != %s", reinstalled, installDir)
	}
	wantMarker := "1.0.0@" + claudePlatformTriple()
	if data, err := os.ReadFile(markerAt(installDir)); err != nil || strings.TrimSpace(string(data)) != wantMarker {
		t.Fatalf("marker 未恢复: %q, %v, want %q", data, err, wantMarker)
	}
	if _, err := os.Stat(filepath.Join(installDir, "junk.txt")); !os.IsNotExist(err) {
		t.Fatalf("整树替换未清除垃圾文件: %v", err)
	}
	if _, err := os.Stat(filepath.Join(installDir, "node_modules", "@scope", "pkg", "dist", "cli.js")); err != nil {
		t.Fatalf("重制后入口缺失: %v", err)
	}
}

// 旧格式 / 异平台 marker 一律不命中，整树重制自愈——跨架构错配的受管目录
// （如历史版本按「仅版本号」marker 复用的 darwin-arm64-only 资源被 amd64 sidecar 命中）
// 在升级后首次请求即被重制，无法永久存续。
func TestEnsureVendoredStaleMarkerReinstalls(t *testing.T) {
	srcRoot, dstRoot := writeStaging(t, "1.0.0")
	entry := vendoringEntry("1.0.0")
	installDir, err := EnsureVendored(entry, srcRoot, dstRoot)
	if err != nil {
		t.Fatalf("EnsureVendored: %v", err)
	}
	// 旧格式（仅版本号，marker 带 triple 前的历史产物）与异平台 triple 两种不匹配形态。
	for name, stale := range map[string]string{
		"旧格式仅版本号": "1.0.0",
		"异平台triple": "1.0.0@sunos-sparc",
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(markerAt(installDir), []byte(stale), 0o644); err != nil {
				t.Fatalf("写入过期 marker: %v", err)
			}
			if err := os.WriteFile(filepath.Join(installDir, "junk.txt"), []byte("junk"), 0o644); err != nil {
				t.Fatalf("write junk: %v", err)
			}
			reinstalled, err := EnsureVendored(entry, srcRoot, dstRoot)
			if err != nil {
				t.Fatalf("EnsureVendored 重制: %v", err)
			}
			if reinstalled != installDir {
				t.Fatalf("重制目录漂移: %s != %s", reinstalled, installDir)
			}
			wantMarker := "1.0.0@" + claudePlatformTriple()
			if data, err := os.ReadFile(markerAt(installDir)); err != nil || strings.TrimSpace(string(data)) != wantMarker {
				t.Fatalf("marker 未恢复: %q, %v, want %q", data, err, wantMarker)
			}
			if _, err := os.Stat(filepath.Join(installDir, "junk.txt")); !os.IsNotExist(err) {
				t.Fatalf("错配目录未被整树重制: %v", err)
			}
		})
	}
}

func TestEnsureVendoredVersionSwitchCleansOld(t *testing.T) {
	srcRoot, dstRoot := writeStaging(t, "1.0.0")
	// 追加 v2 资源源（同 id 不同版本目录）。
	mustWriteFile(t, filepath.Join(srcRoot, "agent-x", "2.0.0", "node_modules", "@scope", "pkg", "dist", "cli.js"), "#!/usr/bin/env node\n", 0o755)
	mustWriteFile(t, filepath.Join(srcRoot, "agent-x", "2.0.0", "node_modules", "@scope", "pkg", "package.json"),
		`{"name":"@scope/pkg","version":"2.0.0","bin":"dist/cli.js"}`, 0o644)

	if _, err := EnsureVendored(vendoringEntry("1.0.0"), srcRoot, dstRoot); err != nil {
		t.Fatalf("EnsureVendored v1: %v", err)
	}
	installDir, err := EnsureVendored(vendoringEntry("2.0.0"), srcRoot, dstRoot)
	if err != nil {
		t.Fatalf("EnsureVendored v2: %v", err)
	}
	if filepath.Base(installDir) != "2.0.0" {
		t.Fatalf("安装目录 = %s, want 版本目录 2.0.0", installDir)
	}
	if _, err := os.Stat(filepath.Join(dstRoot, "agent-x", "1.0.0")); !os.IsNotExist(err) {
		t.Fatalf("旧版本目录未清理: %v", err)
	}
}

func TestEnsureVendoredPreservesSymlinkAndPerm(t *testing.T) {
	srcRoot, dstRoot := writeStaging(t, "1.0.0")
	installDir, err := EnsureVendored(vendoringEntry("1.0.0"), srcRoot, dstRoot)
	if err != nil {
		t.Fatalf("EnsureVendored: %v", err)
	}
	link := filepath.Join(installDir, "node_modules", ".bin", "cli")
	wantTarget := filepath.Join("..", "@scope", "pkg", "dist", "cli.js")
	if got, err := os.Readlink(link); err != nil || got != wantTarget {
		t.Fatalf("symlink 未原样重建: got %q, %v, want %q", got, err, wantTarget)
	}
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("symlink 不可解析: %v", err)
	}
	if !strings.HasSuffix(resolved, filepath.Join("@scope", "pkg", "dist", "cli.js")) {
		t.Fatalf("symlink 解析落点异常: %s", resolved)
	}
	info, err := os.Stat(filepath.Join(installDir, "node_modules", "@scope", "pkg", "dist", "cli.js"))
	if err != nil {
		t.Fatalf("stat 入口: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("执行位丢失: mode = %v, want 0755", info.Mode().Perm())
	}
}

func TestEnsureVendoredRejectsNonVendoredStrategy(t *testing.T) {
	srcRoot, dstRoot := writeStaging(t, "1.0.0")
	entry := vendoringEntry("1.0.0")
	entry.Strategy = StrategyNativeACP
	if _, err := EnsureVendored(entry, srcRoot, dstRoot); err == nil {
		t.Fatal("native-acp 条目应报无 vendoring 形态")
	}
}

// writeInstalledPkg 造最小 vendored 安装目录：<installDir>/node_modules/@scope/pkg/。
// binJSON 为 package.json 的 bin 字段原文（覆盖两种形态与异常形态）。
func writeInstalledPkg(t *testing.T, installDir, binJSON string) {
	t.Helper()
	mustWriteFile(t, filepath.Join(installDir, "node_modules", "@scope", "pkg", "dist", "cli.js"), "#!/usr/bin/env node\n", 0o755)
	mustWriteFile(t, filepath.Join(installDir, "node_modules", "@scope", "pkg", "package.json"),
		`{"name":"@scope/pkg","bin":`+binJSON+`}`, 0o644)
}

func TestVendoredSpawnConfigBinForms(t *testing.T) {
	entry := vendoringEntry("1.0.0")
	cases := []struct {
		name    string
		binJSON string
	}{
		{name: "字符串形态", binJSON: `"dist/cli.js"`},
		{name: "map 单键", binJSON: `{"whatever":"dist/cli.js"}`},
		{name: "map 多键同名键", binJSON: `{"pkg":"dist/cli.js","other":"dist/other.js"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			installDir := t.TempDir()
			writeInstalledPkg(t, installDir, c.binJSON)
			cfg, err := entry.VendoredSpawnConfig(installDir, "/tmp/workdir")
			if err != nil {
				t.Fatalf("VendoredSpawnConfig: %v", err)
			}
			wantEntry := filepath.Join(installDir, "node_modules", "@scope", "pkg", "dist", "cli.js")
			// Command[0] = resolveNodeBin() 结果（LookPath 命中即绝对路径，不再裸 "node"）。
			if len(cfg.Command) != 2 || !filepath.IsAbs(cfg.Command[0]) || cfg.Command[1] != wantEntry {
				t.Fatalf("Command = %v, want [node绝对路径 %s]", cfg.Command, wantEntry)
			}
			if cfg.Cwd != "/tmp/workdir" {
				t.Fatalf("Cwd = %q, want 透传", cfg.Cwd)
			}
			if cfg.Env["FOO"] != "bar" {
				t.Fatalf("Env 未携带条目声明: %v", cfg.Env)
			}
		})
	}
}

func TestVendoredSpawnConfigErrors(t *testing.T) {
	t.Run("args 无 spec", func(t *testing.T) {
		entry := vendoringEntry("1.0.0")
		entry.Args = []string{"-y"}
		if _, err := entry.VendoredSpawnConfig(t.TempDir(), ""); err == nil {
			t.Fatal("无 spec 应报错")
		}
	})
	t.Run("bin 指向不存在入口", func(t *testing.T) {
		entry := vendoringEntry("1.0.0")
		installDir := t.TempDir()
		writeInstalledPkg(t, installDir, `"dist/missing.js"`)
		if _, err := entry.VendoredSpawnConfig(installDir, ""); err == nil || !strings.Contains(err.Error(), "不存在") {
			t.Fatalf("err = %v, want 入口不存在", err)
		}
	})
	t.Run("无 bin 字段", func(t *testing.T) {
		entry := vendoringEntry("1.0.0")
		installDir := t.TempDir()
		writeInstalledPkg(t, installDir, `null`)
		if _, err := entry.VendoredSpawnConfig(installDir, ""); err == nil {
			t.Fatal("无 bin 应报错")
		}
	})
	t.Run("native-acp 防误用", func(t *testing.T) {
		entry := vendoringEntry("1.0.0")
		entry.Strategy = StrategyNativeACP
		if _, err := entry.VendoredSpawnConfig(t.TempDir(), ""); err == nil {
			t.Fatal("native-acp 条目应报无 vendored 入口")
		}
	})
}

func TestAdapterPkgName(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"-y", "@scope/pkg@1.2.3"}, "@scope/pkg"},
		{[]string{"@scope/pkg"}, "@scope/pkg"},
		{[]string{"pkg@1.2.3"}, "pkg"},
		{[]string{"-y", "--omit=dev", "pkg"}, "pkg"},
	}
	for _, c := range cases {
		got, err := adapterPkgName(c.args)
		if err != nil || got != c.want {
			t.Fatalf("adapterPkgName(%v) = %q, %v, want %q", c.args, got, err, c.want)
		}
	}
	if _, err := adapterPkgName([]string{"-y"}); err == nil {
		t.Fatal("全 flag args 应报错")
	}
}

func TestNodeBinFromDirs(t *testing.T) {
	mkDir := func(t *testing.T) string {
		return t.TempDir()
	}
	// 命中：首个含可执行 node 的目录胜出。
	hit := mkDir(t)
	mustWriteFile(t, filepath.Join(hit, "node"), "#!/bin/sh\n", 0o755)
	bin, err := nodeBinFromDirs([]string{mkDir(t), hit})
	if err != nil || bin != filepath.Join(hit, "node") {
		t.Fatalf("nodeBinFromDirs = %q, %v, want %s", bin, err, filepath.Join(hit, "node"))
	}
	// 跳过同名目录与不可执行文件，落到后续目录。
	dirNode := mkDir(t)
	if err := os.MkdirAll(filepath.Join(dirNode, "node"), 0o755); err != nil {
		t.Fatalf("mkdir 同名目录: %v", err)
	}
	notExec := mkDir(t)
	mustWriteFile(t, filepath.Join(notExec, "node"), "not executable", 0o644)
	ok := mkDir(t)
	mustWriteFile(t, filepath.Join(ok, "node"), "#!/bin/sh\n", 0o755)
	bin, err = nodeBinFromDirs([]string{dirNode, notExec, ok})
	if err != nil || bin != filepath.Join(ok, "node") {
		t.Fatalf("nodeBinFromDirs = %q, %v, want %s", bin, err, filepath.Join(ok, "node"))
	}
	// 空列表与全未命中报错。
	if _, err := nodeBinFromDirs(nil); err == nil {
		t.Fatal("空目录列表应报错")
	}
	if _, err := nodeBinFromDirs([]string{dirNode, notExec}); err == nil {
		t.Fatal("无可执行命中应报错")
	}
}
