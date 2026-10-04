package bot

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gorm.io/gorm"

	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
)

// stubDriver 引擎替身：记录被调次数，可注入受理失败。
type stubDriver struct {
	calls int
	err   error
}

func (s *stubDriver) RunTurn(_ context.Context, _ TurnRequest) (<-chan TurnEvent, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	events := make(chan TurnEvent, 1)
	events <- TurnEvent{Type: TurnDone, Result: "ok"}
	close(events)
	return events, nil
}

func TestDriverRouteBranching(t *testing.T) {
	headless, acpDrv := &stubDriver{}, &stubDriver{}
	r := driverRoute{
		headless: headless,
		acp:      acpDrv,
		resolve: func(botID, workspaceID int, conversationKey string) (routeTarget, error) {
			return routeTarget{ACP: workspaceID == 7}, nil
		},
	}
	ctx := context.Background()
	if _, err := r.RunTurn(ctx, TurnRequest{WorkspaceID: 7}); err != nil {
		t.Fatalf("acp 路由受理失败: %v", err)
	}
	if _, err := r.RunTurn(ctx, TurnRequest{WorkspaceID: 0}); err != nil { // 0 = 未选择工作空间 → headless
		t.Fatalf("headless 路由受理失败: %v", err)
	}
	if acpDrv.calls != 1 || headless.calls != 1 {
		t.Fatalf("路由分发不符: acp=%d headless=%d", acpDrv.calls, headless.calls)
	}
}

func TestDriverRouteResolverErrorFailClosed(t *testing.T) {
	r := driverRoute{resolve: func(int, int, string) (routeTarget, error) { return routeTarget{}, errors.New("db boom") }}
	_, err := r.RunTurn(context.Background(), TurnRequest{WorkspaceID: 1})
	if err == nil || !strings.Contains(err.Error(), "读取工作空间启动配置失败") || !strings.Contains(err.Error(), "db boom") {
		t.Fatalf("解析失败应 fail closed 同步报错: %v", err)
	}
}

// TestDriverRouteAcpErrorsMarkedNotAccepted 锚点保护契约：ACP 引擎同步失败与路由解析
// 失败包 turnNotAcceptedError（编排器据此跳过清锚）；headless 引擎失败不包（自愈语义
// 保持不变）。
func TestDriverRouteAcpErrorsMarkedNotAccepted(t *testing.T) {
	ctx := context.Background()
	isNotAccepted := func(err error) bool {
		var rejected turnNotAcceptedError
		return errors.As(err, &rejected)
	}

	acpErrStub, headlessErrStub := &stubDriver{err: errors.New("acp boom")}, &stubDriver{err: errors.New("headless boom")}
	r := driverRoute{
		headless: headlessErrStub,
		acp:      acpErrStub,
		resolve:  func(int, int, string) (routeTarget, error) { return routeTarget{ACP: true}, nil },
	}
	_, err := r.RunTurn(ctx, TurnRequest{WorkspaceID: 7})
	if err == nil || !isNotAccepted(err) || acpErrStub.calls != 1 {
		t.Fatalf("ACP 引擎失败应包受理拒绝标记: %v", err)
	}

	r2 := driverRoute{
		headless: headlessErrStub,
		acp:      acpErrStub,
		resolve:  func(int, int, string) (routeTarget, error) { return routeTarget{}, nil },
	}
	_, err = r2.RunTurn(ctx, TurnRequest{WorkspaceID: 1})
	if err == nil || isNotAccepted(err) || headlessErrStub.calls != 1 {
		t.Fatalf("headless 引擎失败不得包受理拒绝标记（自愈语义不变）: %v", err)
	}

	r3 := driverRoute{resolve: func(int, int, string) (routeTarget, error) { return routeTarget{}, errors.New("db boom") }}
	_, err = r3.RunTurn(ctx, TurnRequest{WorkspaceID: 1})
	if err == nil || !isNotAccepted(err) || !strings.Contains(err.Error(), "读取工作空间启动配置失败") {
		t.Fatalf("路由解析失败应包受理拒绝标记且保留原文案: %v", err)
	}
}

// TestDriverRouteWorkspaceModeResolution 真 DB 解析验收：逐行断言 launch_settings 各形态
// 的 mode 判定（acp / 终端档 / 空配置 / 损坏 JSON = 未配置语义 / 行缺失）。acp 档在无会话
// 行时仍路由 ACP（未绑定交引擎回引导文案，路由层不阻断）。
func TestDriverRouteWorkspaceModeResolution(t *testing.T) {
	db := newBotTestDB(t, &model.Workspace{}, &model.ImBotConversation{})
	q := query.Use(db)
	mk := func(settings string) int {
		ws := &model.Workspace{Name: "ws", Dir: "/tmp/ocean-test", LaunchSettings: settings}
		if err := q.Workspace.WithContext(context.Background()).Create(ws); err != nil {
			t.Fatalf("建 workspace: %v", err)
		}
		return ws.ID
	}
	cases := []struct {
		name     string
		settings string
		want     routeTarget
	}{
		{"acp 档（未绑定）", `{"mode":"acp"}`, routeTarget{ACP: true}},
		{"终端档", `{"mode":"terminal-manual"}`, routeTarget{}},
		{"空配置", "", routeTarget{}},
		{"损坏 JSON（未配置语义）", "{bad json", routeTarget{}},
	}
	ids := make([]int, 0, len(cases))
	for _, tc := range cases {
		ids = append(ids, mk(tc.settings))
	}

	route := newDriverRoute(db, nil) // 解析不触达会话域，nil sessions 安全
	for i, tc := range cases {
		got, err := route.resolve(0, ids[i], "single:u1")
		if err != nil || got != tc.want {
			t.Fatalf("%s: got=%v want=%v err=%v", tc.name, got, tc.want, err)
		}
	}
	if got, err := route.resolve(0, 999999, "single:u1"); err != nil || got != (routeTarget{}) {
		t.Fatalf("行缺失应按未启用处理: got=%v err=%v", got, err)
	}
}

// mkRouteBindingFixture 建路由绑定解析的三表夹具，返回 (wsID, issueID, 存活 helper)。
// launchSettings 可为空串（未配置语义）。
func mkRouteBindingFixture(t *testing.T, wsSettings, issueSettings string) (int, string, *gorm.DB) {
	t.Helper()
	db := newBotTestDB(t, &model.Workspace{}, &model.ImBotConversation{}, &model.ProjectIssue{})
	q := query.Use(db)
	ws := &model.Workspace{Name: "ws", Dir: "/tmp/ocean-test", LaunchSettings: wsSettings}
	if err := q.Workspace.WithContext(context.Background()).Create(ws); err != nil {
		t.Fatalf("建 workspace: %v", err)
	}
	issue := &model.ProjectIssue{
		ID: "0198dddd-0000-7111-8111-3b6ac9e1f201", ProjectID: 1, WorkspaceID: ws.ID, Name: "绑定目标",
		StateCode: enums.STATE_CODE_BACKLOG, Priority: enums.PRIORITY_NONE, LaunchSettings: issueSettings,
	}
	if err := q.ProjectIssue.WithContext(context.Background()).Create(issue); err != nil {
		t.Fatalf("建 issue: %v", err)
	}
	return ws.ID, issue.ID, db
}

// setConvBinding 写会话绑定行（issueID 空串 = 绑定锚为空）。
func setConvBinding(t *testing.T, db *gorm.DB, botID int, convKey, issueID string) {
	t.Helper()
	q := query.Use(db)
	conv := &model.ImBotConversation{BotID: botID, ConversationKey: convKey, BoundIssueID: issueID}
	if err := q.ImBotConversation.WithContext(context.Background()).Create(conv); err != nil {
		t.Fatalf("建会话行: %v", err)
	}
}

// TestDriverRouteBindingResolution 绑定解析验收（T2.3 融合方案定稿的语义矩阵）：
// 有效绑定回填 IssueID；未绑定/空锚/挂空绑定/跨 workspace 绑定全部降级为「ACP+无目标」
// （引擎回引导文案，路由层不阻断不报错）；issue 级 mode 显式覆盖 workspace 级（双向：
// "none" 否决、"acp" 启用）；读库失败 fail closed。
func TestDriverRouteBindingResolution(t *testing.T) {
	const acpSettings = `{"mode":"acp"}`
	const botID = 1
	const convKey = "single:u1"
	// resolve 收敛装配样板：各子测试共享 botID/convKey，仅 db 与 wsID 变化。
	resolve := func(db *gorm.DB, wsID int) (routeTarget, error) {
		return newDriverRoute(db, nil).resolve(botID, wsID, convKey)
	}

	t.Run("有效绑定回填 IssueID", func(t *testing.T) {
		wsID, issueID, db := mkRouteBindingFixture(t, acpSettings, "")
		setConvBinding(t, db, botID, convKey, issueID)
		got, err := resolve(db, wsID)
		if err != nil || got != (routeTarget{IssueID: issueID, ACP: true}) {
			t.Fatalf("got=%+v err=%v", got, err)
		}
	})

	t.Run("空锚与挂空绑定与跨域绑定降级为未绑定", func(t *testing.T) {
		// 空锚。
		wsID, _, db := mkRouteBindingFixture(t, acpSettings, "")
		setConvBinding(t, db, botID, convKey, "")
		got, err := resolve(db, wsID)
		if err != nil || got != (routeTarget{ACP: true}) {
			t.Fatalf("空锚: got=%+v err=%v", got, err)
		}
		// 挂空绑定（issue 已删）。
		wsID2, _, db2 := mkRouteBindingFixture(t, acpSettings, "")
		setConvBinding(t, db2, botID, convKey, "0198dead-0000-7111-8111-3b6ac9e1fdead")
		if got, err = resolve(db2, wsID2); err != nil || got != (routeTarget{ACP: true}) {
			t.Fatalf("挂空绑定: got=%+v err=%v", got, err)
		}
		// 跨 workspace 绑定（issue 归属其他域）。
		wsID3, _, db3 := mkRouteBindingFixture(t, acpSettings, "")
		setConvBinding(t, db3, botID, convKey, "0198dddd-0000-7111-8111-3b6ac9e1f201")
		if err := db3.Model(&model.ProjectIssue{}).Where("1=1").Update("workspace_id", wsID3+999).Error; err != nil {
			t.Fatalf("挪 issue 出域: %v", err)
		}
		if got, err = resolve(db3, wsID3); err != nil || got != (routeTarget{ACP: true}) {
			t.Fatalf("跨域绑定: got=%+v err=%v", got, err)
		}
	})

	t.Run("issue 级 mode 显式覆盖 workspace 级", func(t *testing.T) {
		// 显式 "none" 否决：ACP workspace + issue 明说不要 → headless。
		wsID, _, db := mkRouteBindingFixture(t, acpSettings, `{"mode":"none"}`)
		setConvBinding(t, db, botID, convKey, "0198dddd-0000-7111-8111-3b6ac9e1f201")
		got, err := resolve(db, wsID)
		if err != nil || got != (routeTarget{}) {
			t.Fatalf("issue 显式 none 应否决 ACP: got=%+v err=%v", got, err)
		}
		// 显式 "acp" 启用：终端档 workspace + issue 明说要 ACP → ACP。
		wsID2, _, db2 := mkRouteBindingFixture(t, `{"mode":"terminal-manual"}`, acpSettings)
		setConvBinding(t, db2, botID, convKey, "0198dddd-0000-7111-8111-3b6ac9e1f201")
		if got, err = resolve(db2, wsID2); err != nil || !got.ACP || got.IssueID == "" {
			t.Fatalf("issue 显式 acp 应启用: got=%+v err=%v", got, err)
		}
	})

	t.Run("读库失败 fail closed", func(t *testing.T) {
		wsID, issueID, db := mkRouteBindingFixture(t, acpSettings, "")
		setConvBinding(t, db, botID, convKey, issueID)
		if err := db.Migrator().DropTable("t_im_bot_conversations"); err != nil {
			t.Fatalf("掉会话表: %v", err)
		}
		if _, err := resolve(db, wsID); err == nil {
			t.Fatal("读库失败应 fail closed 报错（不静默降级）")
		}
	})
}
