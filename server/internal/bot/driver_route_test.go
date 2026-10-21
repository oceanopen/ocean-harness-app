package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"gorm.io/gorm"

	"ocean-harness/server/internal/acpsession"
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
		resolve: func(botID, workspaceID string, conversationKey string) (routeTarget, error) {
			return routeTarget{SessionKey: "sk-1", ACP: workspaceID == "ws-acp"}, nil
		},
	}
	ctx := context.Background()
	if _, err := r.RunTurn(ctx, TurnRequest{WorkspaceID: "ws-acp"}); err != nil {
		t.Fatalf("acp 路由受理失败: %v", err)
	}
	if _, err := r.RunTurn(ctx, TurnRequest{WorkspaceID: ""}); err != nil { // 空 = 未选择工作空间 → headless
		t.Fatalf("headless 路由受理失败: %v", err)
	}
	if acpDrv.calls != 1 || headless.calls != 1 {
		t.Fatalf("路由分发不符: acp=%d headless=%d", acpDrv.calls, headless.calls)
	}
}

func TestDriverRouteResolverErrorFailClosed(t *testing.T) {
	r := driverRoute{resolve: func(string, string, string) (routeTarget, error) { return routeTarget{}, errors.New("db boom") }}
	_, err := r.RunTurn(context.Background(), TurnRequest{WorkspaceID: "ws-1"})
	if err == nil || !strings.Contains(err.Error(), "读取工作空间启动配置失败") || !strings.Contains(err.Error(), "db boom") {
		t.Fatalf("解析失败应 fail closed 同步报错: %v", err)
	}
}

/*
 * TestDriverRouteAcpErrorsMarkedNotAccepted 锚点保护契约：ACP 引擎同步失败与路由解析失败包
 * turnNotAcceptedError（编排器据此跳过清锚）；headless 引擎失败不包（自愈语义保持不变）。
 */
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
		resolve:  func(string, string, string) (routeTarget, error) { return routeTarget{SessionKey: "sk-1", ACP: true}, nil },
	}
	_, err := r.RunTurn(ctx, TurnRequest{WorkspaceID: "ws-acp"})
	if err == nil || !isNotAccepted(err) || acpErrStub.calls != 1 {
		t.Fatalf("ACP 引擎失败应包受理拒绝标记: %v", err)
	}

	r2 := driverRoute{
		headless: headlessErrStub,
		acp:      acpErrStub,
		resolve:  func(string, string, string) (routeTarget, error) { return routeTarget{}, nil },
	}
	_, err = r2.RunTurn(ctx, TurnRequest{WorkspaceID: "ws-1"})
	if err == nil || isNotAccepted(err) || headlessErrStub.calls != 1 {
		t.Fatalf("headless 引擎失败不得包受理拒绝标记（自愈语义不变）: %v", err)
	}

	r3 := driverRoute{resolve: func(string, string, string) (routeTarget, error) { return routeTarget{}, errors.New("db boom") }}
	_, err = r3.RunTurn(ctx, TurnRequest{WorkspaceID: "ws-1"})
	if err == nil || !isNotAccepted(err) || !strings.Contains(err.Error(), "读取工作空间启动配置失败") {
		t.Fatalf("路由解析失败应包受理拒绝标记且保留原文案: %v", err)
	}
}

/*
 * TestDriverRouteWorkspaceModeResolution 真 DB 解析验收：逐行断言 launch_settings 各形态的
 * mode 判定（acp / 终端档 / 空配置 / 损坏 JSON = 未配置语义 / 行缺失）。ACP 档未关联任务时
 * 以会话行为锚（conv-<会话行id>，工作空间根目录独立会话），路由层不阻断。
 */
func TestDriverRouteWorkspaceModeResolution(t *testing.T) {
	db := newBotTestDB(t, &model.Workspace{}, &model.ImBotConversation{})
	q := query.Use(db)
	seq := 0
	mk := func(settings string) string {
		seq++
		ws := &model.Workspace{ID: fmt.Sprintf("ws-%d", seq), Name: "ws", Dir: "/tmp/ocean-test", LaunchSettings: settings}
		if err := q.Workspace.WithContext(context.Background()).Create(ws); err != nil {
			t.Fatalf("建 workspace: %v", err)
		}
		return ws.ID
	}
	const acpSettings = `{"mode":"acp"}`
	cases := []struct {
		name     string
		settings string
		acp      bool
	}{
		{"acp 档（未关联）", acpSettings, true},
		{"终端档", `{"mode":"terminal-manual"}`, false},
		{"空配置", "", false},
		{"损坏 JSON（未配置语义）", "{bad json", false},
	}
	ids := make([]string, 0, len(cases))
	for _, tc := range cases {
		ids = append(ids, mk(tc.settings))
	}

	route := newDriverRoute(db, nil) // 解析仅触达会话行建行，nil sessions 安全
	const botID, convKey = "", "single:u1"
	for i, tc := range cases {
		got, err := route.resolve(botID, ids[i], convKey)
		want := routeTarget{}
		if tc.acp {
			want = routeTarget{SessionKey: acpsession.ConversationSessionKey(convRowID(t, db, botID, convKey)), ACP: true}
		}
		if err != nil || got != want {
			t.Fatalf("%s: got=%v want=%v err=%v", tc.name, got, want, err)
		}
	}
	if got, err := route.resolve(botID, "ws-missing", convKey); err != nil || got != (routeTarget{}) {
		t.Fatalf("行缺失应按未启用处理: got=%v err=%v", got, err)
	}
}

/*
 * mkRouteFixture 建路由目标解析的三表夹具，返回 (wsID, issueID, db)。
 * launchSettings 可为空串（未配置语义）。
 */
func mkRouteFixture(t *testing.T, wsSettings, issueSettings string) (string, string, *gorm.DB) {
	t.Helper()
	db := newBotTestDB(t, &model.Workspace{}, &model.WorkspaceIssue{}, &model.ImBotConversation{})
	q := query.Use(db)
	ws := &model.Workspace{ID: "ws-route", Name: "ws", Dir: "/tmp/ocean-test", LaunchSettings: wsSettings}
	if err := q.Workspace.WithContext(context.Background()).Create(ws); err != nil {
		t.Fatalf("建 workspace: %v", err)
	}
	issue := &model.WorkspaceIssue{
		ID: "0198dddd-0000-7111-8111-3b6ac9e1f201", WorkspaceID: ws.ID, Name: "关联目标",
		StateCode: enums.STATE_CODE_BACKLOG, LaunchSettings: issueSettings,
	}
	if err := q.WorkspaceIssue.WithContext(context.Background()).Create(issue); err != nil {
		t.Fatalf("建 issue: %v", err)
	}
	return ws.ID, issue.ID, db
}

// setConvBinding 写会话绑定行（issueID 空串 = 绑定锚为空）。
func setConvBinding(t *testing.T, db *gorm.DB, botID string, convKey, issueID string) {
	t.Helper()
	q := query.Use(db)
	conv := &model.ImBotConversation{BotID: botID, ConversationKey: convKey, BoundIssueID: issueID}
	if err := q.ImBotConversation.WithContext(context.Background()).Create(conv); err != nil {
		t.Fatalf("建会话行: %v", err)
	}
}

// convRowID 读会话行 id（conv- 会话锚的载体，GetOrCreate 建行后取用）。
func convRowID(t *testing.T, db *gorm.DB, botID, convKey string) string {
	t.Helper()
	q := query.Use(db)
	row, err := q.ImBotConversation.WithContext(context.Background()).Where(
		q.ImBotConversation.BotID.Eq(botID),
		q.ImBotConversation.ConversationKey.Eq(convKey),
	).First()
	if err != nil {
		t.Fatalf("读会话行: %v", err)
	}
	return row.ID
}

/*
 * TestDriverRouteTargetResolution 目标解析验收（双锚语义矩阵）：有效关联回填 issue 锚（与
 * 桌面共享会话）；未关联/空锚/挂空关联/跨 workspace 关联全部回填 conv- 会话锚（会话行载体，
 * 工作空间根目录）；issue 级 mode 显式覆盖 workspace 级（双向："none" 否决、"acp" 启用）；
 * 读库失败 fail closed。
 */
func TestDriverRouteTargetResolution(t *testing.T) {
	const acpSettings = `{"mode":"acp"}`
	const botID = "bot-1"
	const convKey = "single:u1"
	// resolve 收敛装配样板：各子测试共享 botID/convKey，仅 db 与 wsID 变化。
	resolve := func(db *gorm.DB, wsID string) (routeTarget, error) {
		return newDriverRoute(db, nil).resolve(botID, wsID, convKey)
	}
	convWant := func(db *gorm.DB) routeTarget {
		return routeTarget{SessionKey: acpsession.ConversationSessionKey(convRowID(t, db, botID, convKey)), ACP: true}
	}

	t.Run("有效关联回填 issue 会话锚", func(t *testing.T) {
		wsID, issueID, db := mkRouteFixture(t, acpSettings, "")
		setConvBinding(t, db, botID, convKey, issueID)
		got, err := resolve(db, wsID)
		if err != nil || got != (routeTarget{SessionKey: issueID, ACP: true}) {
			t.Fatalf("got=%+v err=%v", got, err)
		}
	})

	t.Run("未关联与空锚回填 conv 会话锚", func(t *testing.T) {
		// 无绑定行：resolve 兜底建行后以行 id 为锚。
		wsID, _, db := mkRouteFixture(t, acpSettings, "")
		got, err := resolve(db, wsID)
		if err != nil || got != convWant(db) {
			t.Fatalf("未关联: got=%+v want=%+v err=%v", got, convWant(db), err)
		}
		// 空锚绑定行：同 conv 锚形。
		wsID2, _, db2 := mkRouteFixture(t, acpSettings, "")
		setConvBinding(t, db2, botID, convKey, "")
		if got, err = resolve(db2, wsID2); err != nil || got != convWant(db2) {
			t.Fatalf("空锚: got=%+v want=%+v err=%v", got, convWant(db2), err)
		}
	})

	t.Run("挂空关联与跨域关联降级 conv 锚", func(t *testing.T) {
		// 挂空关联（issue 已删）。
		wsID, _, db := mkRouteFixture(t, acpSettings, "")
		setConvBinding(t, db, botID, convKey, "0198dead-0000-7111-8111-3b6ac9e1fdead")
		got, err := resolve(db, wsID)
		if err != nil || got != convWant(db) {
			t.Fatalf("挂空关联: got=%+v want=%+v err=%v", got, convWant(db), err)
		}
		// 跨 workspace 关联（issue 归属其他域，双键校验不命中）。
		wsID2, _, db2 := mkRouteFixture(t, acpSettings, "")
		setConvBinding(t, db2, botID, convKey, "0198dddd-0000-7111-8111-3b6ac9e1f201")
		if err := db2.Model(&model.WorkspaceIssue{}).Where("1=1").Update("workspace_id", "ws-other").Error; err != nil {
			t.Fatalf("挪 issue 出域: %v", err)
		}
		if got, err = resolve(db2, wsID2); err != nil || got != convWant(db2) {
			t.Fatalf("跨域关联: got=%+v want=%+v err=%v", got, convWant(db2), err)
		}
	})

	t.Run("issue 级 mode 显式覆盖 workspace 级", func(t *testing.T) {
		// 显式 "none" 否决：ACP workspace + issue 明说不要 → headless。
		wsID, _, db := mkRouteFixture(t, acpSettings, `{"mode":"none"}`)
		setConvBinding(t, db, botID, convKey, "0198dddd-0000-7111-8111-3b6ac9e1f201")
		got, err := resolve(db, wsID)
		if err != nil || got != (routeTarget{}) {
			t.Fatalf("issue 显式 none 应否决 ACP: got=%+v err=%v", got, err)
		}
		// 显式 "acp" 启用：终端档 workspace + issue 明说要 ACP → ACP。
		wsID2, issueID2, db2 := mkRouteFixture(t, `{"mode":"terminal-manual"}`, acpSettings)
		setConvBinding(t, db2, botID, convKey, issueID2)
		if got, err = resolve(db2, wsID2); err != nil || got != (routeTarget{SessionKey: issueID2, ACP: true}) {
			t.Fatalf("issue 显式 acp 应启用: got=%+v err=%v", got, err)
		}
	})

	t.Run("读库失败 fail closed", func(t *testing.T) {
		wsID, issueID, db := mkRouteFixture(t, acpSettings, "")
		setConvBinding(t, db, botID, convKey, issueID)
		if err := db.Migrator().DropTable("t_im_bot_conversations"); err != nil {
			t.Fatalf("掉会话表: %v", err)
		}
		if _, err := resolve(db, wsID); err == nil {
			t.Fatal("读库失败应 fail closed 报错（不静默降级）")
		}
	})
}

/*
 * TestDriverRouteProbeTarget 探针契约：ProbeTarget 与 resolve 同源——ACP 档（无论关联与否）
 * 探得 acp=true、终端档 acp=false，对齐路由实际判定；读库失败原样透传 err（调用方不拦截，
 * 按探测失败回落保守语义）。
 */
func TestDriverRouteProbeTarget(t *testing.T) {
	const acpSettings = `{"mode":"acp"}`
	const botID = "bot-1"
	const convKey = "single:u1"
	probe := func(db *gorm.DB, wsID string) (bool, error) {
		return newDriverRoute(db, nil).ProbeTarget(BotRuntimeConfig{BotID: botID, WorkspaceID: wsID}, convKey)
	}

	t.Run("ACP 未关联", func(t *testing.T) {
		wsID, _, db := mkRouteFixture(t, acpSettings, "")
		acp, err := probe(db, wsID)
		if err != nil || !acp {
			t.Fatalf("未关联探针应 ACP: acp=%v err=%v", acp, err)
		}
	})

	t.Run("ACP 已关联", func(t *testing.T) {
		wsID, issueID, db := mkRouteFixture(t, acpSettings, "")
		setConvBinding(t, db, botID, convKey, issueID)
		acp, err := probe(db, wsID)
		if err != nil || !acp {
			t.Fatalf("已关联探针应 ACP: acp=%v err=%v", acp, err)
		}
	})

	t.Run("终端档非 ACP", func(t *testing.T) {
		wsID, _, db := mkRouteFixture(t, `{"mode":"terminal-manual"}`, "")
		acp, err := probe(db, wsID)
		if err != nil || acp {
			t.Fatalf("终端档探针应非 ACP: acp=%v err=%v", acp, err)
		}
	})

	t.Run("读库失败透传", func(t *testing.T) {
		wsID, _, db := mkRouteFixture(t, acpSettings, "")
		if err := db.Migrator().DropTable("t_im_bot_conversations"); err != nil {
			t.Fatalf("掉会话表: %v", err)
		}
		if _, err := probe(db, wsID); err == nil {
			t.Fatal("读库失败应透传 err")
		}
	})
}
