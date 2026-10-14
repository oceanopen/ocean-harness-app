package service

/*
 * inputCDP kind→CDP Input 域映射单测（T4.5）：四合法路的 method 与 params 字段、非法
 * 路的中文报错。
 */

import (
	"reflect"
	"strings"
	"testing"

	"ocean-harness/server/internal/dal/types"
)

func TestInputCDP(t *testing.T) {
	for _, tc := range []struct {
		name       string
		req        types.BrowserInputRequest
		wantMethod string
		wantParams map[string]any
		wantErr    string
	}{
		{
			name:       "mouse 全字段",
			req:        types.BrowserInputRequest{Kind: "mouse", Action: "mousePressed", X: 10.5, Y: 20, Button: "left", ClickCount: 2, Modifiers: 2},
			wantMethod: "Input.dispatchMouseEvent",
			wantParams: map[string]any{"type": "mousePressed", "x": 10.5, "y": 20.0, "button": "left", "clickCount": 2, "modifiers": 2},
		},
		{
			name:       "mouse 最小字段（零值缺省不传）",
			req:        types.BrowserInputRequest{Kind: "mouse", Action: "mouseMoved"},
			wantMethod: "Input.dispatchMouseEvent",
			wantParams: map[string]any{"type": "mouseMoved", "x": 0.0, "y": 0.0},
		},
		{
			name:       "wheel 固定 mouseWheel",
			req:        types.BrowserInputRequest{Kind: "wheel", X: 5, Y: 6, DeltaX: 0, DeltaY: -120},
			wantMethod: "Input.dispatchMouseEvent",
			wantParams: map[string]any{"type": "mouseWheel", "x": 5.0, "y": 6.0, "deltaX": 0.0, "deltaY": -120.0},
		},
		{
			name:       "key 全字段（vk 映射 windowsVirtualKeyCode）",
			req:        types.BrowserInputRequest{Kind: "key", Action: "keyDown", Key: "a", Code: "KeyA", VK: 65, Text: "a", Modifiers: 1},
			wantMethod: "Input.dispatchKeyEvent",
			wantParams: map[string]any{"type": "keyDown", "key": "a", "code": "KeyA", "windowsVirtualKeyCode": 65, "text": "a", "modifiers": 1},
		},
		{
			name:       "key 控制键 rawKeyDown",
			req:        types.BrowserInputRequest{Kind: "key", Action: "rawKeyDown", Key: "Control", VK: 17},
			wantMethod: "Input.dispatchKeyEvent",
			wantParams: map[string]any{"type": "rawKeyDown", "key": "Control", "windowsVirtualKeyCode": 17},
		},
		{
			name:       "text 走 insertText",
			req:        types.BrowserInputRequest{Kind: "text", Text: "中文输入"},
			wantMethod: "Input.insertText",
			wantParams: map[string]any{"text": "中文输入"},
		},
		{name: "未知 kind", req: types.BrowserInputRequest{Kind: "move"}, wantErr: "kind 非法"},
		{name: "mouse 缺 action", req: types.BrowserInputRequest{Kind: "mouse"}, wantErr: "action 非法"},
		{name: "mouse action 越白名单", req: types.BrowserInputRequest{Kind: "mouse", Action: "mouseWheel"}, wantErr: "action 非法"},
		{name: "key action 越白名单", req: types.BrowserInputRequest{Kind: "key", Action: "press"}, wantErr: "action 非法"},
		{name: "text 缺 text", req: types.BrowserInputRequest{Kind: "text"}, wantErr: "text 不能为空"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			method, params, err := inputCDP(&tc.req)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want 含 %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("inputCDP: %v", err)
			}
			if method != tc.wantMethod {
				t.Fatalf("method = %s, want %s", method, tc.wantMethod)
			}
			if !reflect.DeepEqual(params, tc.wantParams) {
				t.Fatalf("params = %v, want %v", params, tc.wantParams)
			}
		})
	}
}
