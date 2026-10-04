package bot

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

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
		acpMode:  func(workspaceID int) (bool, error) { return workspaceID == 7, nil },
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
	r := driverRoute{acpMode: func(int) (bool, error) { return false, errors.New("db boom") }}
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
		acpMode:  func(int) (bool, error) { return true, nil },
	}
	_, err := r.RunTurn(ctx, TurnRequest{WorkspaceID: 7})
	if err == nil || !isNotAccepted(err) || acpErrStub.calls != 1 {
		t.Fatalf("ACP 引擎失败应包受理拒绝标记: %v", err)
	}

	r2 := driverRoute{
		headless: headlessErrStub,
		acp:      acpErrStub,
		acpMode:  func(int) (bool, error) { return false, nil },
	}
	_, err = r2.RunTurn(ctx, TurnRequest{WorkspaceID: 1})
	if err == nil || isNotAccepted(err) || headlessErrStub.calls != 1 {
		t.Fatalf("headless 引擎失败不得包受理拒绝标记（自愈语义不变）: %v", err)
	}

	r3 := driverRoute{acpMode: func(int) (bool, error) { return false, errors.New("db boom") }}
	_, err = r3.RunTurn(ctx, TurnRequest{WorkspaceID: 1})
	if err == nil || !isNotAccepted(err) || !strings.Contains(err.Error(), "读取工作空间启动配置失败") {
		t.Fatalf("路由解析失败应包受理拒绝标记且保留原文案: %v", err)
	}
}

// TestDriverRouteWorkspaceModeResolution 真 DB 解析验收：逐行断言 launch_settings 各形态
// 的 mode 判定（acp / 终端档 / 空配置 / 损坏 JSON = 未配置语义 / 行缺失）。
func TestDriverRouteWorkspaceModeResolution(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开临时 sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Workspace{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
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
		want     bool
	}{
		{"acp 档", `{"mode":"acp"}`, true},
		{"终端档", `{"mode":"terminal-manual"}`, false},
		{"空配置", "", false},
		{"损坏 JSON（未配置语义）", "{bad json", false},
	}
	ids := make([]int, 0, len(cases))
	for _, tc := range cases {
		ids = append(ids, mk(tc.settings))
	}

	r := newDriverRoute(db, nil) // mode 解析不触达会话域，nil 安全
	route := r.(driverRoute)
	for i, tc := range cases {
		got, err := route.acpMode(ids[i])
		if err != nil || got != tc.want {
			t.Fatalf("%s: got=%v err=%v", tc.name, got, err)
		}
	}
	if got, err := route.acpMode(999999); err != nil || got {
		t.Fatalf("行缺失应按未启用处理: got=%v err=%v", got, err)
	}
}
