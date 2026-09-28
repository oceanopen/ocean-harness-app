package marketplace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// manifest_test.go：marketplace 包纯函数的表驱动测试（清单解析 / source 分类 / CLI 探测 /
// 复合 id）。不调外部 CLI，全部用临时目录 fixture——解析边界是前端投影正确性的源头。

func TestPluginRef(t *testing.T) {
	if got := PluginRef("ocean-code", "ocean-claude-plugins"); got != "ocean-code@ocean-claude-plugins" {
		t.Fatalf("PluginRef() = %q, want %q", got, "ocean-code@ocean-claude-plugins")
	}
}

func TestClaudeMarketplaceEntrySourceType(t *testing.T) {
	cases := []struct {
		name       string
		entry      ClaudeMarketplaceEntry
		wantType   string
		wantDetail string
	}{
		{"github 源归一 repo", ClaudeMarketplaceEntry{Source: "github", Repo: "owner/repo"}, "github", "owner/repo"},
		{"git 源归一 url", ClaudeMarketplaceEntry{Source: "git", URL: "git@host:repo.git"}, "git", "git@host:repo.git"},
		{"directory 源归一 local", ClaudeMarketplaceEntry{Source: "directory", Path: "/tmp/plugins"}, "local", "/tmp/plugins"},
		{"file 源归一 local", ClaudeMarketplaceEntry{Source: "file", Path: "/tmp/mk.json"}, "local", "/tmp/mk.json"},
		{"未知源原样透传", ClaudeMarketplaceEntry{Source: "exotic"}, "exotic", ""},
	}
	for _, c := range cases {
		gotType, gotDetail := c.entry.SourceType()
		if gotType != c.wantType || gotDetail != c.wantDetail {
			t.Errorf("%s: SourceType() = (%q, %q), want (%q, %q)", c.name, gotType, gotDetail, c.wantType, c.wantDetail)
		}
	}
}

func TestDetectSupportedClis(t *testing.T) {
	empty := t.TempDir()
	if got := DetectSupportedClis(empty); len(got) != 0 {
		t.Fatalf("无清单目录: DetectSupportedClis() = %v, want 空", got)
	}

	withManifest := t.TempDir()
	if err := os.MkdirAll(filepath.Join(withManifest, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(withManifest, ".claude-plugin", "marketplace.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := DetectSupportedClis(withManifest)
	if len(got) != 1 || got[0] != "claude" {
		t.Fatalf("有清单目录: DetectSupportedClis() = %v, want [claude]", got)
	}
}

func TestClassifySource(t *testing.T) {
	cases := []struct {
		name         string
		raw          string
		wantSource   string
		wantExternal bool
	}{
		{"相对路径字符串", `"./plugins/foo"`, "./plugins/foo", false},
		{"空值跳过目录解析", ``, "", false},
		{"对象形态=外部引用", `{"source":"github","repo":"o/x"}`, `{"source":"github","repo":"o/x"}`, true},
		{"非法字符串 JSON 按外部引用兜底", `"unclosed`, "", true},
	}
	for _, c := range cases {
		gotSource, gotExternal := classifySource(json.RawMessage(c.raw))
		if gotSource != c.wantSource || gotExternal != c.wantExternal {
			t.Errorf("%s: classifySource() = (%q, %v), want (%q, %v)", c.name, gotSource, gotExternal, c.wantSource, c.wantExternal)
		}
	}
}

// writeManifestFixture 在 root 下落一份市场清单与样例插件目录树，返回清单 JSON 文本。
func writeManifestFixture(t *testing.T, root, manifestJSON string) {
	t.Helper()
	mkDir := filepath.Join(root, ".claude-plugin")
	if err := os.MkdirAll(mkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mkDir, "marketplace.json"), []byte(manifestJSON), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadManifest(t *testing.T) {
	root := t.TempDir()
	// 完整组件样例插件：2 commands + 1 agent + 1 skill + hooks + mcp，plugin.json 带 version。
	sample := filepath.Join(root, "plugins", "sample")
	for _, dir := range []string{
		filepath.Join(sample, "commands"),
		filepath.Join(sample, "agents"),
		filepath.Join(sample, "skills", "git-commit"),
		filepath.Join(sample, "hooks"),
		filepath.Join(sample, ".claude-plugin"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{
		filepath.Join(sample, "commands", "a.md"),
		filepath.Join(sample, "commands", "b.md"),
		filepath.Join(sample, "commands", "ignore.txt"), // 非 .md 不计
		filepath.Join(sample, "agents", "reviewer.md"),
		filepath.Join(sample, "skills", "git-commit", "SKILL.md"),
		filepath.Join(sample, "hooks", "hooks.json"),
		filepath.Join(sample, ".mcp.json"),
		filepath.Join(sample, ".claude-plugin", "plugin.json"),
	} {
		if err := os.WriteFile(f, []byte(`{"version":"1.2.3"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 无 plugin.json 的插件：版本回落市场条目 version。
	nover := filepath.Join(root, "plugins", "nover")
	if err := os.MkdirAll(nover, 0o755); err != nil {
		t.Fatal(err)
	}

	writeManifestFixture(t, root, `{
		"name": "sample-mk",
		"metadata": {"description": "meta 描述", "version": "2.0.0"},
		"plugins": [
			{"name": "sample", "source": "./plugins/sample", "description": "样例插件"},
			{"name": "ext", "source": {"source": "github", "repo": "o/x"}},
			{"name": "escape", "source": "../outside", "version": "0.1.0"},
			{"name": "nover", "source": "./plugins/nover", "version": "0.9.0"}
		]
	}`)

	mf, err := LoadManifest(root)
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	if mf.Name != "sample-mk" || mf.Description != "meta 描述" || mf.Version != "2.0.0" {
		t.Fatalf("顶层字段 = (%q, %q, %q), want metadata 备选位置生效", mf.Name, mf.Description, mf.Version)
	}
	if len(mf.Plugins) != 4 {
		t.Fatalf("plugins 数 = %d, want 4", len(mf.Plugins))
	}

	samplePlugin := mf.Plugins[0]
	if samplePlugin.Version != "1.2.3" {
		t.Errorf("sample.Version = %q, want plugin.json 的 1.2.3", samplePlugin.Version)
	}
	wantComps := PluginComponents{Commands: 2, Skills: 1, Agents: 1, Hooks: 1, McpServers: 1}
	if samplePlugin.Components != wantComps {
		t.Errorf("sample.Components = %+v, want %+v", samplePlugin.Components, wantComps)
	}
	if samplePlugin.Dir == "" || samplePlugin.SourceExternal {
		t.Errorf("sample 应解析出本地目录且非外部引用: dir=%q external=%v", samplePlugin.Dir, samplePlugin.SourceExternal)
	}

	ext := mf.Plugins[1]
	if !ext.SourceExternal || ext.Dir != "" {
		t.Errorf("ext 应为外部引用且无本地目录: dir=%q external=%v", ext.Dir, ext.SourceExternal)
	}

	escape := mf.Plugins[2]
	if escape.Dir != "" || escape.Components != (PluginComponents{}) {
		t.Errorf("escape 的 .. 路径应拒绝目录解析: dir=%q comps=%+v", escape.Dir, escape.Components)
	}

	noverPlugin := mf.Plugins[3]
	if noverPlugin.Version != "0.9.0" {
		t.Errorf("nover.Version = %q, want 回落条目 version 0.9.0", noverPlugin.Version)
	}
}

func TestLoadManifestErrors(t *testing.T) {
	if _, err := LoadManifest(t.TempDir()); err == nil {
		t.Fatal("清单文件缺失应返回错误")
	}

	root := t.TempDir()
	writeManifestFixture(t, root, `{not-json`)
	if _, err := LoadManifest(root); err == nil {
		t.Fatal("清单 JSON 非法应返回错误")
	}
}
