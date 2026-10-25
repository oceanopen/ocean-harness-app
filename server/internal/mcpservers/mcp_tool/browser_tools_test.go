package mcptool

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"ocean-harness/server/internal/browser"
	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
	mcpdto "ocean-harness/server/internal/mcpservers/mcp_dto"
	"ocean-harness/server/internal/service"
)

/*
 * browser 工具面单测：profile 归一（会话行反查派生）、引擎入参抽取（剥服务端自有字段）、
 * 归因元数据追加、逃生舱黑名单前置拒绝（后者走 handler 真实入口——黑名单校验先于链式装配，
 * 无需测试环境装配）。
 */

// newResolveProfileSvc 临时 sqlite + 会话表装配的 BrowserSession（resolveProfile DB 依赖）。
func newResolveProfileSvc(t *testing.T) *service.BrowserSession {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开临时 sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Conversation{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	svc := &service.BrowserSession{}
	svc.Context = context.Background()
	svc.Orm = db
	return svc
}

// seedConversation 播种会话行（ID/ownerType/ownerID 最小面）。
func seedConversation(t *testing.T, svc *service.BrowserSession, id string, ownerType enums.ConversationOwnerType, ownerID string) {
	t.Helper()
	now := time.Now()
	row := &model.Conversation{ID: id, OwnerType: ownerType, OwnerID: ownerID, Mode: enums.CONVERSATION_MODE_ACP, CreatedAt: now, UpdatedAt: now}
	if err := svc.Orm.Create(row).Error; err != nil {
		t.Fatalf("播种会话 %s: %v", id, err)
	}
}

func TestResolveProfile(t *testing.T) {
	// 显式 profile 优先与双空缺省不触 DB：svc 可为 nil。
	if got, err := resolveProfile(nil, "work", "", 1); err != nil || got != "work" {
		t.Fatalf(`resolveProfile("work", "", 1) = %q, %v; want 原值`, got, err)
	}
	if got, err := resolveProfile(nil, "", "", 1); err != nil || got != defaultBrowserProfile {
		t.Fatalf(`resolveProfile("", "", 1) = %q, %v; want %q`, got, err, defaultBrowserProfile)
	}

	svc := newResolveProfileSvc(t)
	seedConversation(t, svc, "conv-1", enums.OWNER_TYPE_TASK, "task-1")
	seedConversation(t, svc, "conv-ws", enums.OWNER_TYPE_WORKSPACE, "ws-1")

	// 任务派生约定：profile 留空且会话在手 → issue-<taskId>_session-<conversationId>-NN。
	want1 := browser.TaskProfilePrefix + "task-1" + browser.SessionSegment + "conv-1-01"
	got, err := resolveProfile(svc, "", "conv-1", 1)
	if err != nil || got != want1 {
		t.Fatalf("resolveProfile(conv-1, 1) = %q, %v; want %q", got, err, want1)
	}
	want3 := browser.TaskProfilePrefix + "task-1" + browser.SessionSegment + "conv-1-03"
	got, err = resolveProfile(svc, "", "conv-1", 3)
	if err != nil || got != want3 {
		t.Fatalf("resolveProfile(conv-1, 3) = %q, %v; want %q", got, err, want3)
	}
	// 显式 profile 不被覆写。
	if got, err := resolveProfile(svc, "work", "conv-1", 1); err != nil || got != "work" {
		t.Fatalf("resolveProfile(work, conv-1, 1) = %q, %v; want 原值", got, err)
	}
	// workspace 归属会话 / 不存在的会话：无法解析归属任务。
	for _, conv := range []string{"conv-ws", "conv-missing"} {
		if _, err := resolveProfile(svc, "", conv, 1); err == nil || !strings.Contains(err.Error(), "无法解析归属任务") {
			t.Fatalf("resolveProfile(%s) err = %v, want 无法解析归属任务", conv, err)
		}
	}
}

func TestEngineArgs(t *testing.T) {
	in := mcpdto.BrowserNavigateArgs{
		BrowserProfileArgs: mcpdto.BrowserProfileArgs{Profile: "work", ConversationID: "conv-1"},
		URL:                "https://example.com",
	}
	args := engineArgs(&in)
	if args["url"] != "https://example.com" {
		t.Fatalf("url = %v, want 引擎参数保留", args["url"])
	}
	for _, banned := range []string{"profile", "conversationId"} {
		if _, ok := args[banned]; ok {
			t.Fatalf("args 含服务端自有字段 %s: %v", banned, args)
		}
	}
}

func TestEngineArgsPointerFields(t *testing.T) {
	depth := 3
	in := mcpdto.BrowserSnapshotArgs{
		BrowserProfileArgs: mcpdto.BrowserProfileArgs{Profile: "default"},
		Depth:              &depth,
		Boxes:              nil, // 未传字段不出现在引擎入参（omitempty）
	}
	args := engineArgs(&in)
	if args["depth"] != float64(3) { // JSON 往返数值统一 float64
		t.Fatalf("depth = %v, want 3", args["depth"])
	}
	if _, ok := args["boxes"]; ok {
		t.Fatalf("未传的指针字段不应出现在引擎入参: %v", args)
	}
}

func TestWithForwardMeta(t *testing.T) {
	engine := &mcp.TextContent{Text: "engine-result"}

	res := &mcp.CallToolResult{Content: []mcp.Content{engine}}
	withForwardMeta(res, "work", "")
	last, ok := res.Content[1].(*mcp.TextContent)
	if !ok || res.Content[0] != mcp.Content(engine) || last.Text != "[profile: work]" {
		t.Fatalf("无 conversationId 元数据 = %+v, want 引擎 Content 保持首位、[profile: work] 尾部追加", res.Content)
	}

	res = &mcp.CallToolResult{Content: []mcp.Content{engine}}
	withForwardMeta(res, "work", "conv-9")
	last, ok = res.Content[1].(*mcp.TextContent)
	if !ok || last.Text != "[profile: work, conversationId: conv-9]" {
		t.Fatalf("带 conversationId 元数据 = %v, want [profile: work, conversationId: conv-9] 尾部追加", res.Content[1])
	}
}

func TestToolCallBlacklistRejected(t *testing.T) {
	// 黑名单校验先于链式装配：无 MakeContext 环境也可断言拒绝文案。
	mt := McpBrowserTool{}
	_, _, err := mt.ToolCall(context.Background(), nil, mcpdto.BrowserToolCallArgs{
		BrowserProfileArgs: mcpdto.BrowserProfileArgs{Profile: "default"},
		Name:               "browser_run_code_unsafe",
	})
	if err == nil || !strings.Contains(err.Error(), "禁止直调") {
		t.Fatalf("err = %v, want 黑名单中文拒绝", err)
	}
}
