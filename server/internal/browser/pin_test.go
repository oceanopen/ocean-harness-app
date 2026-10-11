package browser

import (
	"os"
	"strings"
	"testing"

	"ocean-harness/server/internal/agentcatalog"
)

func TestEngineEntry(t *testing.T) {
	entry, err := EngineEntry()
	if err != nil {
		t.Fatalf("EngineEntry: %v", err)
	}
	if entry.ID != "playwright-mcp" || entry.Strategy != agentcatalog.StrategyNpxAdapter {
		t.Fatalf("entry 身份字段 = %+v", entry)
	}
	if got, want := strings.Join(entry.Args, " "), "-y @playwright/mcp@"+entry.Version; got != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
	// 版本 pin 与 browser-mcp.json 一致。
	version, err := EngineVersion()
	if err != nil || version != entry.Version {
		t.Fatalf("EngineVersion = %q, %v; want %q", version, err, entry.Version)
	}
}

func TestEngineSpawnArgs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		headless bool
	}{
		{"有头缺省", false},
		{"无头追加", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := EngineSpawnArgs("/p/dir", "/p/down", "/p/engine.json", tc.headless)
			joined := strings.Join(args, "\x00")
			// 固定 flags 全在（拼写即 T1.1 采信结论）；--browser 不传 = playwright 内置
			// Chromium build（T3.5/D2 修订，系统 chrome channel 退役）。
			for _, fragment := range []string{
				"--user-data-dir\x00/p/dir",
				"--caps\x00" + engineCaps,
				"--output-dir\x00/p/down",
				"--idle-timeout\x00900000",
				"--no-webmcp",
				"--file-paths\x00absolute",
				"--config\x00/p/engine.json",
			} {
				if !strings.Contains(joined, fragment) {
					t.Errorf("args 缺片段 %q: %v", fragment, args)
				}
			}
			if strings.Contains(joined, "--browser") {
				t.Errorf("args 不应含 --browser（内置 Chromium 不传 channel）: %v", args)
			}
			hasHeadless := strings.Contains(joined, "--headless")
			if hasHeadless != tc.headless {
				t.Errorf("--headless 出现 = %v, want %v", hasHeadless, tc.headless)
			}
		})
	}
}

func TestEnsureEngineConfig(t *testing.T) {
	SetRoot(t.TempDir())
	t.Cleanup(func() { SetRoot("") })

	path, err := EnsureEngineConfig()
	if err != nil {
		t.Fatalf("EnsureEngineConfig: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读回配置: %v", err)
	}
	if string(data) != engineConfigJSON {
		t.Fatalf("配置内容漂移: %q", string(data))
	}
	// 幂等：内容一致不重写（mtime 不变）。
	info1, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	path2, err := EnsureEngineConfig()
	if err != nil || path2 != path {
		t.Fatalf("二次调用 = %q, %v", path2, err)
	}
	info2, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Fatalf("内容一致时不应重写文件")
	}
	// 内容漂移（旧版本残留）时覆写对齐。
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatalf("写入漂移内容: %v", err)
	}
	if _, err := EnsureEngineConfig(); err != nil {
		t.Fatalf("漂移后调用: %v", err)
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != engineConfigJSON {
		t.Fatalf("漂移未对齐: %q, %v", string(data), err)
	}
}
