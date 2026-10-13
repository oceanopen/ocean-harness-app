package controller

import (
	"net/http"
	"testing"

	"ocean-harness/server/internal/browser"
)

// 订阅前置判定（D3 细化口径）：不可订阅状态逐一落 404/409，可订阅状态返回 nil。
func TestBrowserStreamReject(t *testing.T) {
	cases := []struct {
		name       string
		status     browser.SessionStatus
		wantStatus int
	}{
		{"未知 profile 零值行空串 404", "", http.StatusNotFound},
		{"已关闭 409", browser.StatusIdle, http.StatusConflict},
		{"引擎未运行 409", browser.StatusFailed, http.StatusConflict},
		{"启动中可订阅", browser.StatusStarting, 0},
		{"就绪可订阅", browser.StatusReady, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotStatus, err := browserStreamReject(tc.status)
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
