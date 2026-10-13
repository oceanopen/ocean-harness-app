package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"ocean-harness/server/internal/acpsession"
	"ocean-harness/server/internal/apis"
	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/global"
)

// 订阅前置判定（D3 细化口径）：idle/terminated 拒绝，starting/ready/failed 可订阅
// （failed 是可观察状态，快照含失败原因供前端呈现）。
func TestAcpEventsReject(t *testing.T) {
	cases := []struct {
		name       string
		status     acpsession.SessionStatus
		wantStatus int
	}{
		{"不存在 404", acpsession.StatusIdle, http.StatusNotFound},
		{"已结束 409", acpsession.StatusTerminated, http.StatusConflict},
		{"启动中可订阅", acpsession.StatusStarting, 0},
		{"就绪可订阅", acpsession.StatusReady, 0},
		{"启动失败可订阅", acpsession.StatusFailed, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotStatus, err := acpEventsReject(tc.status)
			if tc.wantStatus == 0 {
				if err != nil {
					t.Fatalf("期望可订阅，得到 %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("期望拒绝，得到 nil")
			}
			if gotStatus != tc.wantStatus {
				t.Fatalf("状态码 = %d，期望 %d", gotStatus, tc.wantStatus)
			}
		})
	}
}

// Events handler 接线：预检先于 Subscribe，不存在会话（Get 返回 idle 空快照）404 +
// code!=0 真实状态码落线（预检被删/挪到 Subscribe 之后则此处退化）。Manager 装配用
// 临时 sqlite（AutoMigrate 锚点表，范式同 acpsession/launch_settings_test 的 newTestDB）。
func TestAcpEventsHandlerReject(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开临时 sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.IssueAcpSession{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	mgr, err := acpsession.NewManager(db, acpsession.Dirs{}, 0, nil)
	if err != nil {
		t.Fatalf("装配 Manager: %v", err)
	}
	global.AcpSessions = mgr
	t.Cleanup(func() { global.AcpSessions = nil })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/acpSession/events?issueId=t3-no-such-issue", nil)
	AcpSession{}.Events(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d，期望 %d", w.Code, http.StatusNotFound)
	}
	var body apis.Response
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应体: %v", err)
	}
	if body.Code != apis.RESPONSE_CODE_ERROR || body.Msg == "" {
		t.Fatalf("响应体 = code:%d msg:%q，期望 code=%d 且带原因", body.Code, body.Msg, apis.RESPONSE_CODE_ERROR)
	}
}
