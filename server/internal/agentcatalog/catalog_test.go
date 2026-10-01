package agentcatalog

import (
	"reflect"
	"strings"
	"testing"

	"ocean-harness/server/internal/acp"
	"ocean-harness/server/internal/dal/enums"
)

// TestLoadEmbedded 内嵌产物常驻断言：catalog 跑偏（pin 包名漂移、条目被清空）在 CI 即红。
func TestLoadEmbedded(t *testing.T) {
	cat, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if cat.SchemaVersion != schemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", cat.SchemaVersion, schemaVersion)
	}
	entry, ok := GetAgentCatalogInfoByCode(string(enums.AGENT_CODE_CLAUDE_ACP))
	if !ok {
		t.Fatal("缺映射条目 claude-acp（一期唯一启用 agent）")
	}
	if entry.Code != string(enums.AGENT_CODE_CLAUDE_ACP) {
		t.Fatalf("entry.Code = %q, want %q", entry.Code, enums.AGENT_CODE_CLAUDE_ACP)
	}
	if !entry.Enabled {
		t.Fatal("claude-acp 未启用")
	}
	if entry.Strategy != StrategyNpxAdapter {
		t.Fatalf("strategy = %q, want %q", entry.Strategy, StrategyNpxAdapter)
	}
	if len(entry.Args) < 2 || !strings.Contains(entry.Args[1], "@agentclientprotocol/claude-agent-acp@") {
		t.Fatalf("pin 形态异常: %v（期望 args[1] 为 @agentclientprotocol/claude-agent-acp@<精确版本>）", entry.Args)
	}
	if len(Enabled()) == 0 {
		t.Fatal("Enabled() 为空")
	}
}

func TestEntrySpawnConfig(t *testing.T) {
	cases := []struct {
		name    string
		entry   Entry
		cwd     string
		want    acp.SpawnConfig
		wantErr string
	}{
		{
			name:  "npx-adapter 合成 argv 与 env",
			entry: Entry{ID: "claude-acp", Strategy: StrategyNpxAdapter, Command: "npx", Args: []string{"-y", "@agentclientprotocol/claude-agent-acp@0.84.0"}, Env: map[string]string{"FOO": "bar"}},
			cwd:   "/tmp/ws",
			want:  acp.SpawnConfig{Command: []string{"npx", "-y", "@agentclientprotocol/claude-agent-acp@0.84.0"}, Env: map[string]string{"FOO": "bar"}, Cwd: "/tmp/ws"},
		},
		{
			name:  "native-acp 直连 CLI",
			entry: Entry{ID: "codex", Strategy: StrategyNativeACP, Command: "codex", Args: []string{"acp"}},
			cwd:   "/tmp/ws",
			want:  acp.SpawnConfig{Command: []string{"codex", "acp"}, Cwd: "/tmp/ws"},
		},
		{
			name:    "in-process-go 预留枚举报错",
			entry:   Entry{ID: "x", Strategy: StrategyInProcessGo, Command: "go"},
			wantErr: "预留枚举",
		},
		{
			name:    "未知 strategy 报错",
			entry:   Entry{ID: "x", Strategy: "bogus", Command: "go"},
			wantErr: "非法",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.entry.SpawnConfig(tc.cwd)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want 含 %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("SpawnConfig(): %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("SpawnConfig() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseValidation(t *testing.T) {
	valid := `{"schemaVersion":1,"source":{"url":"u","registryVersion":"1","fetchedAt":"t"},"agents":[` +
		`{"id":"claude-acp","code":"claude-acp","strategy":"npx-adapter","command":"npx","args":["-y","pkg@1.0.0"],"enabled":true}]}`

	cases := []struct {
		name    string
		data    string
		wantErr string
	}{
		{name: "合法单条目", data: valid},
		{name: "schemaVersion 不符", data: strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":2`, 1), wantErr: "schemaVersion"},
		{name: "无条目", data: `{"schemaVersion":1,"agents":[]}`, wantErr: "无条目"},
		{name: "空 id", data: strings.Replace(valid, `"id":"claude-acp"`, `"id":""`, 1), wantErr: "空 id"},
		{name: "id 重复", data: strings.Replace(valid, `"enabled":true}]`, `"enabled":true},{"id":"claude-acp","code":"claude-acp","strategy":"native-acp","command":"c"}]`, 1), wantErr: "id 重复"},
		{name: "code 缺失", data: strings.Replace(valid, `"code":"claude-acp",`, ``, 1), wantErr: "code 为空"},
		{name: "code 重复", data: strings.Replace(valid, `"enabled":true}]`, `"enabled":true},{"id":"other","code":"claude-acp","strategy":"native-acp","command":"c"}]`, 1), wantErr: "code 重复"},
		{name: "strategy 非法", data: strings.Replace(valid, `"strategy":"npx-adapter"`, `"strategy":"bogus"`, 1), wantErr: "非法"},
		{name: "enabled 但 command 空", data: strings.Replace(valid, `"command":"npx"`, `"command":""`, 1), wantErr: "command 为空"},
		{name: "JSON 损坏", data: `{`, wantErr: "解析 agent catalog"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cat, err := parse([]byte(tc.data))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("parse(): %v", err)
				}
				if len(cat.Agents) != 1 {
					t.Fatalf("Agents = %d, want 1", len(cat.Agents))
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want 含 %q", err, tc.wantErr)
			}
		})
	}
}
