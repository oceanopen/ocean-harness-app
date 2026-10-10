package browser

import (
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
		wantTail []string // 断言的关键片段（有序）
	}{
		{"有头缺省", false, nil},
		{"无头追加", true, []string{"--headless"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := EngineSpawnArgs("/p/dir", "/p/down", tc.headless)
			joined := strings.Join(args, "\x00")
			// 固定 flags 全在（拼写即 T1.1 采信结论）。
			for _, fragment := range []string{
				"--browser\x00chrome",
				"--user-data-dir\x00/p/dir",
				"--caps\x00" + engineCaps,
				"--output-dir\x00/p/down",
				"--idle-timeout\x00900000",
				"--no-webmcp",
				"--file-paths\x00absolute",
			} {
				if !strings.Contains(joined, fragment) {
					t.Errorf("args 缺片段 %q: %v", fragment, args)
				}
			}
			hasHeadless := strings.Contains(joined, "--headless")
			if hasHeadless != tc.headless {
				t.Errorf("--headless 出现 = %v, want %v", hasHeadless, tc.headless)
			}
		})
	}
}
