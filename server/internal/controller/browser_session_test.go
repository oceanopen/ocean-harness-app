package controller

import (
	"testing"

	"ocean-harness/server/internal/browser"
)

// 订阅前置判定：不可订阅状态逐一落中文拒绝原因，可订阅状态返回 nil（拒绝经 streamReject
// 以 SSE reject 事件透出，不再走 HTTP 状态码路径）。
func TestBrowserStreamReject(t *testing.T) {
	cases := []struct {
		name    string
		status  browser.SessionStatus
		wantMsg string
	}{
		{"未知 profile 零值行空串", "", "浏览器会话不存在"},
		{"已关闭", browser.StatusIdle, "浏览器会话已关闭"},
		{"引擎未运行", browser.StatusFailed, "浏览器引擎未运行"},
		{"启动中可订阅", browser.StatusStarting, ""},
		{"就绪可订阅", browser.StatusReady, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := browserStreamReject(tc.status)
			if tc.wantMsg == "" {
				if err != nil {
					t.Fatalf("期望可订阅，得到 %v", err)
				}
				return
			}
			if err == nil || err.Error() != tc.wantMsg {
				t.Fatalf("错误 = %v，期望 %q", err, tc.wantMsg)
			}
		})
	}
}
