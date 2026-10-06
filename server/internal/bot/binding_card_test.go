package bot

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"gorm.io/gorm"

	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
)

// newBindingTestDB 绑定卡链路夹具库：四表全触达（workspace/issue 候选查询、bot 行
// workspace_id 落库、会话行 bound_issue_id 落库）。
func newBindingTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return newBotTestDB(t, &model.Workspace{}, &model.ProjectIssue{}, &model.ImBot{}, &model.ImBotConversation{})
}

// mkWorkspace 建行辅助，返回行 id。
func mkWorkspace(t *testing.T, db *gorm.DB, name string) int {
	t.Helper()
	ws := &model.Workspace{Name: name, Dir: "/tmp/ocean-test"}
	if err := db.Create(ws).Error; err != nil {
		t.Fatalf("建 workspace: %v", err)
	}
	return ws.ID
}

// mkBindIssue 建 issue 行（状态与父级显式控制——候选口径断言依据；SortOrder 排序断言依据）。
func mkBindIssue(t *testing.T, db *gorm.DB, workspaceID int, id, name string, sortOrder float64, state enums.StateCode, parentID string) {
	t.Helper()
	issue := &model.ProjectIssue{
		ID: id, ProjectID: 1, WorkspaceID: workspaceID, Name: name,
		StateCode: state, Priority: enums.PRIORITY_NONE,
		SortOrder: sortOrder, TypeID: 1, ParentID: parentID,
	}
	if err := db.Create(issue).Error; err != nil {
		t.Fatalf("建 issue %s: %v", id, err)
	}
}

// mkBot 建启用 bot 行（botID 恒 1，workspace_id 显式传入）。
func mkBot(t *testing.T, db *gorm.DB, workspaceID int) {
	t.Helper()
	bot := &model.ImBot{
		Name: "b", Channel: enums.CHANNEL_WECOM, Credential: "{}",
		WorkspaceID: workspaceID, AccessPolicy: `{"mode":"open"}`, Enabled: enums.YES_NO_YES,
	}
	if err := db.Create(bot).Error; err != nil {
		t.Fatalf("建 bot: %v", err)
	}
}

// fakeApplier 热重载消费面替身：同步留痕（异步在 Supervisor 实现内，此处只验调用事实）。
type fakeApplier struct {
	mu    sync.Mutex
	calls []int
}

func (f *fakeApplier) ApplyBotAsync(botID int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, botID)
}

func (f *fakeApplier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// bindClickMsg 点击消息构造（TaskID 与 DeliveryID 同锚——防串卡对照通过）。
func bindClickMsg(taskID string, idx int) InboundMessage {
	return InboundMessage{
		MessageID: "m-1", ConversationKey: "single:u1", SenderID: "u1",
		Interaction: &Interaction{DeliveryID: taskID, ActionIndex: idx, TaskID: taskID},
	}
}

// bindWsList 当前 workspace 列表（卡构造同源数据）。
func bindWsList(t *testing.T, db *gorm.DB) []*model.Workspace {
	t.Helper()
	wss, err := bindingWorkspaces(db)
	if err != nil {
		t.Fatalf("查 workspace 列表: %v", err)
	}
	return wss
}

// TestBindingTaskIDRoundtrip 投递锚往返三段式 + 域互斥 + 非法形态全 miss + 卡契约合法。
func TestBindingTaskIDRoundtrip(t *testing.T) {
	wsID := wsbindCardPrefix + bindingFingerprint([]string{"1", "22", "333"})
	fp, ok := parseWsbindTaskID(wsID)
	if !ok || fp != bindingFingerprint([]string{"1", "22", "333"}) {
		t.Fatalf("wsbind 往返不符: fp=%q ok=%v", fp, ok)
	}
	itID := taskbindCardPrefix + bindingFingerprint([]string{"a", "b"})
	if fp, ok := parseTaskbindTaskID(itID); !ok || fp != bindingFingerprint([]string{"a", "b"}) {
		t.Fatalf("taskbind 往返不符: fp=%q ok=%v", fp, ok)
	}
	// 域互斥 + 非法形态：对侧域锚、无前缀、尾段空/过短/过长/非 hex 全部 miss（两域各验各的）。
	for _, bad := range []string{itID, "acp-x@y-perm-1", "wsbind-", "wsbind-abc", "wsbind-123456789", "01234567"} {
		if _, ok := parseWsbindTaskID(bad); ok {
			t.Fatalf("parseWsbindTaskID(%q) 应 miss", bad)
		}
	}
	for _, bad := range []string{wsID, "acp-x@y-perm-1", "taskbind-", "taskbind-abc", "taskbind-123456789", "taskbind-zzzzzzzz", "01234567"} {
		if _, ok := parseTaskbindTaskID(bad); ok {
			t.Fatalf("parseTaskbindTaskID(%q) 应 miss", bad)
		}
	}
	// 卡契约：前缀 + hex 字符集天然过 ValidateCardTaskID。
	if err := ValidateCardTaskID(wsID); err != nil {
		t.Fatalf("wsbind 锚应过卡契约: %v", err)
	}
	if err := ValidateCardTaskID(itID); err != nil {
		t.Fatalf("taskbind 锚应过卡契约: %v", err)
	}
}

// TestBindingFingerprintStability 指纹稳定性：同列表恒同值；内容因子（增删）与次序因子
// （换位）任一变化即不符——候选查询确定序（SortOrder + ID 次序键）的依据。
func TestBindingFingerprintStability(t *testing.T) {
	ids := []string{"1", "22", "333"}
	a := bindingFingerprint(ids)
	if b := bindingFingerprint(ids); a != b || len(a) != bindingFingerprintLen {
		t.Fatalf("同列表应恒同指纹: %q vs %q", a, b)
	}
	if bindingFingerprint([]string{"1", "22"}) == a || bindingFingerprint([]string{"1", "22", "333", "4"}) == a {
		t.Fatal("候选集增删应致指纹不符")
	}
	if bindingFingerprint([]string{"333", "22", "1"}) == a {
		t.Fatal("同集换位应致指纹不符")
	}
}

// TestBindingCardSpecConstruction 卡构造 SSOT：单选点击即答形态、TaskID 指纹锚、置灰
// 重建同构（企微整卡替换）、空列表不出卡。
func TestBindingCardSpecConstruction(t *testing.T) {
	wss := []*model.Workspace{{ID: 3, Name: "前端仓"}, {ID: 7, Name: "后端仓"}}
	spec, ok := workspaceCardSpec(wss)
	if !ok {
		t.Fatal("非空列表应出卡")
	}
	if spec.Multiple || spec.Submit {
		t.Fatal("绑定卡为单选点击即答形态")
	}
	if spec.Title == "" || spec.Description == "" || len(spec.Options) != 2 ||
		spec.Options[0].ID != "3" || spec.Options[0].Text != "前端仓" || spec.Options[1].ID != "7" {
		t.Fatalf("workspace 卡字段不符: %+v", spec)
	}
	if spec.TaskID != wsbindCardPrefix+bindingFingerprint([]string{"3", "7"}) {
		t.Fatalf("TaskID 应为候选指纹锚: %q", spec.TaskID)
	}
	// 置灰重建共用 SSOT：同构造仅 Disabled（首发与点击侧重建两处同源，字段一致由此保证）。
	gray, _ := workspaceCardSpec(wss)
	gray.Disabled = true
	if gray.TaskID != spec.TaskID || !gray.Disabled || len(gray.Options) != len(spec.Options) {
		t.Fatal("置灰卡应与原卡同构仅 Disabled")
	}
	// 空列表不出卡（触发侧回纯文本）。
	if _, ok := workspaceCardSpec(nil); ok {
		t.Fatal("空 workspace 列表不应出卡")
	}
	if _, ok := issueCardSpec(nil); ok {
		t.Fatal("空候选列表不应出卡")
	}
	// 任务卡同构。
	issues := []*model.ProjectIssue{{ID: "i-1", Name: "登录重构"}, {ID: "i-2", Name: "接口联调"}}
	ispec, ok := issueCardSpec(issues)
	if !ok || len(ispec.Options) != 2 || ispec.Options[1].Text != "接口联调" ||
		ispec.TaskID != taskbindCardPrefix+bindingFingerprint([]string{"i-1", "i-2"}) {
		t.Fatalf("任务卡字段不符: %+v", ispec)
	}
}

// TestBindingIssueCandidates 候选口径验收（拍板口径）：非终态且顶级、外域排除、NULL 与
// 空串顶级等价、SortOrder 升序、并列按 ID 次序键、上限截断 20。
func TestBindingIssueCandidates(t *testing.T) {
	db := newBindingTestDB(t)
	const ws = 1
	mkBindIssue(t, db, ws, "i-null", "NULL 顶级任务", 0, enums.STATE_CODE_BACKLOG, "")
	mkBindIssue(t, db, ws, "i-backlog", "待办池任务", 1, enums.STATE_CODE_BACKLOG, "")
	mkBindIssue(t, db, ws, "i-todo", "待办任务", 2, enums.STATE_CODE_TODO, "")
	mkBindIssue(t, db, ws, "i-doing", "进行中任务", 3, enums.STATE_CODE_IN_PROGRESS, "")
	mkBindIssue(t, db, ws, "i-done", "已完成任务", 4, enums.STATE_CODE_DONE, "")           // 终态排除
	mkBindIssue(t, db, ws, "i-cancel", "已取消任务", 5, enums.STATE_CODE_CANCELLED, "")    // 终态排除
	mkBindIssue(t, db, ws, "i-child", "子任务", 0.5, enums.STATE_CODE_TODO, "i-backlog") // 子任务排除（顶级口径）
	mkBindIssue(t, db, 2, "i-other", "外域任务", 0, enums.STATE_CODE_BACKLOG, "")         // 外域排除
	// NULL 顶级（外部写入的历史行形态）。
	if err := db.Exec("UPDATE t_project_issues SET parent_id = NULL WHERE id = ?", "i-null").Error; err != nil {
		t.Fatalf("置 NULL 顶级: %v", err)
	}

	got, err := bindingIssueCandidates(db, ws)
	if err != nil {
		t.Fatalf("候选查询: %v", err)
	}
	want := []string{"i-null", "i-backlog", "i-todo", "i-doing"} // SortOrder 升序
	if len(got) != len(want) {
		t.Fatalf("候选数量不符: got %v want %v", issueIDKeys(got), want)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("候选序不符: got %v want %v", issueIDKeys(got), want)
		}
	}

	t.Run("并列按 ID 次序键", func(t *testing.T) {
		db := newBindingTestDB(t)
		mkBindIssue(t, db, 1, "b-id", "乙", 5, enums.STATE_CODE_BACKLOG, "")
		mkBindIssue(t, db, 1, "a-id", "甲", 5, enums.STATE_CODE_BACKLOG, "")
		got, err := bindingIssueCandidates(db, 1)
		if err != nil || len(got) != 2 {
			t.Fatalf("候选查询: %v %+v", err, got)
		}
		if got[0].ID != "a-id" {
			t.Fatalf("并列 SortOrder 应按 ID 升序定序（指纹稳定性依据）: %v", issueIDKeys(got))
		}
	})

	t.Run("上限截断", func(t *testing.T) {
		db := newBindingTestDB(t)
		for i := 0; i < bindingOptionLimit+2; i++ {
			mkBindIssue(t, db, 1, fmt.Sprintf("cap-%02d", i), fmt.Sprintf("任务%02d", i),
				float64(i), enums.STATE_CODE_BACKLOG, "")
		}
		got, err := bindingIssueCandidates(db, 1)
		if err != nil || len(got) != bindingOptionLimit {
			t.Fatalf("应截断至上限: n=%d err=%v", len(got), err)
		}
	})
}

// TestBindingWorkspacesLimit workspace 卡候选同享上限（与任务卡同受企微 vote 选项约束，
// 超出截断防止触发链发卡被适配器拒收）。
func TestBindingWorkspacesLimit(t *testing.T) {
	db := newBindingTestDB(t)
	for i := 0; i < bindingOptionLimit+2; i++ {
		mkWorkspace(t, db, fmt.Sprintf("仓%02d", i))
	}
	if got := bindWsList(t, db); len(got) != bindingOptionLimit {
		t.Fatalf("workspace 候选应截断至上限: n=%d", len(got))
	}
}

// TestWsbindClick 命中路径：落库 bot 行 workspace_id + 置灰完整重建 + 热重载触发 + 终帧文案。
func TestWsbindClick(t *testing.T) {
	db := newBindingTestDB(t)
	wsA, wsB := mkWorkspace(t, db, "前端仓"), mkWorkspace(t, db, "后端仓")
	mkBot(t, db, wsA)
	applier := &fakeApplier{}
	route := &driverRoute{db: db, applier: applier}
	cfg := BotRuntimeConfig{BotID: 1, WorkspaceID: wsA}

	spec, ok := workspaceCardSpec(bindWsList(t, db))
	if !ok {
		t.Fatal("应能出卡")
	}
	text, update, handled := route.TryHandleInteraction(cfg, bindClickMsg(spec.TaskID, 1))
	if !handled {
		t.Fatal("wsbind 点击应命中")
	}
	if text != "✅ 已关联工作空间「后端仓」" {
		t.Fatalf("终帧文案不符: %q", text)
	}
	if update == nil || !update.Disabled || update.TaskID != spec.TaskID || len(update.Options) != 2 {
		t.Fatalf("置灰 spec 应为完整重建: %+v", update)
	}
	if applier.count() != 1 {
		t.Fatalf("应恰一次热重载触发，got %d", applier.count())
	}
	botRow, err := query.Use(db).ImBot.WithContext(context.Background()).Where(query.Use(db).ImBot.ID.Eq(1)).First()
	if err != nil {
		t.Fatalf("读回 bot 行: %v", err)
	}
	if botRow.WorkspaceID != wsB {
		t.Fatalf("bot 行 workspace_id 应已落库: ws=%d", botRow.WorkspaceID)
	}
}

// TestWsbindClickMisses wsbind 未命中矩阵：指纹不符（不置灰不落库不热重载）/ 越界防御 /
// bot 行已删（落库 0 行失败文案）/ 读库失败 fail visible / applier 缺省降级 / 防串卡与
// 未知锚 miss。
func TestWsbindClickMisses(t *testing.T) {
	t.Run("指纹不符不落库", func(t *testing.T) {
		db := newBindingTestDB(t)
		wsA := mkWorkspace(t, db, "前端仓")
		mkBot(t, db, wsA)
		applier := &fakeApplier{}
		route := &driverRoute{db: db, applier: applier}
		spec, _ := workspaceCardSpec(bindWsList(t, db))
		mkWorkspace(t, db, "新仓") // 卡发出后列表变化

		text, update, handled := route.TryHandleInteraction(BotRuntimeConfig{BotID: 1}, bindClickMsg(spec.TaskID, 0))
		if !handled || text != bindingListChangedText || update != nil {
			t.Fatalf("指纹不符应回重取卡文案且不置灰: text=%q update=%+v handled=%v", text, update, handled)
		}
		if applier.count() != 0 {
			t.Fatal("指纹不符不应触发热重载")
		}
		botRow, err := query.Use(db).ImBot.WithContext(context.Background()).Where(query.Use(db).ImBot.ID.Eq(1)).First()
		if err != nil {
			t.Fatalf("读回 bot 行: %v", err)
		}
		if botRow.WorkspaceID != wsA {
			t.Fatalf("指纹不符不应落库: ws=%d", botRow.WorkspaceID)
		}
	})

	t.Run("越界防御", func(t *testing.T) {
		db := newBindingTestDB(t)
		mkWorkspace(t, db, "前端仓")
		mkBot(t, db, 0)
		route := &driverRoute{db: db}
		spec, _ := workspaceCardSpec(bindWsList(t, db))
		text, update, handled := route.TryHandleInteraction(BotRuntimeConfig{BotID: 1}, bindClickMsg(spec.TaskID, 99))
		if !handled || text != bindingInvalidOptionText || update != nil {
			t.Fatalf("越界应回无效选项文案: text=%q update=%+v handled=%v", text, update, handled)
		}
	})

	t.Run("bot 行已删", func(t *testing.T) {
		db := newBindingTestDB(t)
		mkWorkspace(t, db, "前端仓")
		route := &driverRoute{db: db} // 无 bot 行：UpdateSimple 0 行
		spec, _ := workspaceCardSpec(bindWsList(t, db))
		text, update, handled := route.TryHandleInteraction(BotRuntimeConfig{BotID: 1}, bindClickMsg(spec.TaskID, 0))
		if !handled || text != "工作空间关联失败，请稍后重试。" || update != nil {
			t.Fatalf("bot 行缺失应回关联失败文案: text=%q handled=%v", text, handled)
		}
	})

	t.Run("读库失败 fail visible", func(t *testing.T) {
		db := newBindingTestDB(t)
		mkBot(t, db, 0)
		if err := db.Migrator().DropTable("t_workspaces"); err != nil {
			t.Fatalf("掉 workspace 表: %v", err)
		}
		route := &driverRoute{db: db}
		text, update, handled := route.TryHandleInteraction(BotRuntimeConfig{BotID: 1}, bindClickMsg(wsbindCardPrefix+"deadbeef", 0))
		if !handled || text != "工作空间列表读取失败，请稍后重试。" || update != nil {
			t.Fatalf("读库失败应 fail visible: text=%q handled=%v", text, handled)
		}
	})

	t.Run("applier 缺省降级", func(t *testing.T) {
		db := newBindingTestDB(t)
		wsA, wsB := mkWorkspace(t, db, "前端仓"), mkWorkspace(t, db, "后端仓")
		mkBot(t, db, wsA)
		route := &driverRoute{db: db} // applier 缺省 nil：跳过热重载仅落库
		spec, _ := workspaceCardSpec(bindWsList(t, db))
		text, update, handled := route.TryHandleInteraction(BotRuntimeConfig{BotID: 1}, bindClickMsg(spec.TaskID, 1))
		if !handled || update == nil || !update.Disabled {
			t.Fatalf("applier 缺省不影响落库与置灰: text=%q handled=%v update=%+v", text, handled, update)
		}
		botRow, err := query.Use(db).ImBot.WithContext(context.Background()).Where(query.Use(db).ImBot.ID.Eq(1)).First()
		if err != nil {
			t.Fatalf("读回 bot 行: %v", err)
		}
		if botRow.WorkspaceID != wsB {
			t.Fatalf("applier 缺省仍应落库: ws=%d", botRow.WorkspaceID)
		}
	})

	t.Run("防串卡与未知锚 miss", func(t *testing.T) {
		db := newBindingTestDB(t)
		mkWorkspace(t, db, "前端仓")
		route := &driverRoute{db: db}
		spec, _ := workspaceCardSpec(bindWsList(t, db))
		msg := bindClickMsg(spec.TaskID, 0)
		msg.Interaction.TaskID = "other" // 渠道回传锚与投递锚不一致
		if _, _, handled := route.TryHandleInteraction(BotRuntimeConfig{BotID: 1}, msg); handled {
			t.Fatal("串卡形态应 miss")
		}
		if _, _, handled := route.TryHandleInteraction(BotRuntimeConfig{BotID: 1}, bindClickMsg("unknown-anchor", 0)); handled {
			t.Fatal("未知投递锚应 miss")
		}
	})
}

// TestTaskbindClick 任务卡命中路径：落库点击来源会话行 + 置灰完整重建 + 终帧文案。
func TestTaskbindClick(t *testing.T) {
	db := newBindingTestDB(t)
	ws := mkWorkspace(t, db, "ws")
	mkBindIssue(t, db, ws, "i-1", "登录重构", 1, enums.STATE_CODE_BACKLOG, "")
	mkBindIssue(t, db, ws, "i-2", "接口联调", 2, enums.STATE_CODE_TODO, "")
	store := &ConversationStore{DB: db}
	if _, err := store.GetOrCreate(1, "single:u1"); err != nil { // 会话行在场（拦截步 HasSeen→GetOrCreate 同款保证）
		t.Fatalf("预置会话行: %v", err)
	}
	route := &driverRoute{db: db}

	issues, err := bindingIssueCandidates(db, ws)
	if err != nil {
		t.Fatalf("候选查询: %v", err)
	}
	spec, ok := issueCardSpec(issues)
	if !ok {
		t.Fatal("应能出卡")
	}
	cfg := BotRuntimeConfig{BotID: 1, WorkspaceID: ws}
	text, update, handled := route.TryHandleInteraction(cfg, bindClickMsg(spec.TaskID, 1))
	if !handled {
		t.Fatal("taskbind 点击应命中")
	}
	if text != "✅ 已绑定任务「接口联调」，直接发消息即可向该任务下达任务。" {
		t.Fatalf("终帧文案不符: %q", text)
	}
	if update == nil || !update.Disabled || update.TaskID != spec.TaskID || len(update.Options) != 2 {
		t.Fatalf("置灰 spec 应为完整重建: %+v", update)
	}
	bound, err := store.BoundIssueID(1, "single:u1")
	if err != nil || bound != "i-2" {
		t.Fatalf("会话行绑定锚应已落库: bound=%q err=%v", bound, err)
	}
}

// TestTaskbindClickMisses 任务卡未命中矩阵：指纹不符（不置灰不落库）/ 读库失败 fail visible。
func TestTaskbindClickMisses(t *testing.T) {
	t.Run("指纹不符不落库", func(t *testing.T) {
		db := newBindingTestDB(t)
		ws := mkWorkspace(t, db, "ws")
		mkBindIssue(t, db, ws, "i-1", "登录重构", 1, enums.STATE_CODE_BACKLOG, "")
		store := &ConversationStore{DB: db}
		if _, err := store.GetOrCreate(1, "single:u1"); err != nil {
			t.Fatalf("预置会话行: %v", err)
		}
		route := &driverRoute{db: db}
		issues, _ := bindingIssueCandidates(db, ws)
		spec, _ := issueCardSpec(issues)
		mkBindIssue(t, db, ws, "i-new", "新任务", 0, enums.STATE_CODE_BACKLOG, "") // 卡发出后候选变化

		text, update, handled := route.TryHandleInteraction(BotRuntimeConfig{BotID: 1, WorkspaceID: ws}, bindClickMsg(spec.TaskID, 0))
		if !handled || text != bindingListChangedText || update != nil {
			t.Fatalf("指纹不符应回重取卡文案且不置灰: text=%q handled=%v", text, handled)
		}
		if bound, _ := store.BoundIssueID(1, "single:u1"); bound != "" {
			t.Fatalf("指纹不符不应落库: bound=%q", bound)
		}
	})

	t.Run("读库失败 fail visible", func(t *testing.T) {
		db := newBindingTestDB(t)
		if err := db.Migrator().DropTable("t_project_issues"); err != nil {
			t.Fatalf("掉 issue 表: %v", err)
		}
		route := &driverRoute{db: db}
		text, update, handled := route.TryHandleInteraction(BotRuntimeConfig{BotID: 1, WorkspaceID: 1}, bindClickMsg(taskbindCardPrefix+"deadbeef", 0))
		if !handled || text != "任务列表读取失败，请稍后重试。" || update != nil {
			t.Fatalf("读库失败应 fail visible: text=%q handled=%v", text, handled)
		}
	})
}
