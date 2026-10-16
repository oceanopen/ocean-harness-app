package browser

/*
 * observer.go 单测：观察者初始化建行、target 创建/销毁事件驱动投影更新、visibility
 * 探测回写。CDP 面复用 cdpTestServer 桩件（Runtime.evaluate 走 setEvaluate 注入）。
 */

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// waitPages 轮询等待投影页数达标并返回最新行。
func waitPages(t *testing.T, m *Manager, profile string, want int) []PageView {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		pages := m.view.row(profile).Pages
		if len(pages) == want {
			return pages
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("5s 内投影页数未达 %d（当前 %+v）", want, m.view.row(profile).Pages)
	return nil
}

func TestPageObserverEvents(t *testing.T) {
	ts := newCDPTestServer(t, []cdpTargetInfo{{ID: "t1", Type: "page", URL: "https://x.test/", Title: "例页"}})
	m := newTestManager(t)
	dir, err := ProfileDir("default")
	if err != nil {
		t.Fatal(err)
	}
	writePortFile(t, dir, ts.port)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	o, err := newPageObserver(ctx, m, "default")
	if err != nil {
		t.Fatalf("newPageObserver: %v", err)
	}
	t.Cleanup(func() { _ = o.conn.close() })
	go o.run()

	// 初始化：/json/list 建行，无可见性信号回退最近活动页（唯一页即当前）。
	pages := waitPages(t, m, "default", 1)
	if pages[0].TargetID != "t1" || pages[0].URL != "https://x.test/" || !pages[0].Current {
		t.Fatalf("初始投影 = %+v", pages)
	}

	// targetCreated：新 tab 进投影（创建事件回退判据下即当前页）。
	created, _ := json.Marshal(map[string]any{
		"targetInfo": map[string]any{"targetId": "t2", "type": "page", "url": "about:blank", "title": ""},
	})
	ts.pushEvent(t, cdpWireMsg{Method: "Target.targetCreated", Params: created})
	pages = waitPages(t, m, "default", 2)
	if pages[1].TargetID != "t2" || pages[0].Current || !pages[1].Current {
		t.Fatalf("创建后投影 = %+v", pages)
	}

	// targetDestroyed：行移除、Index 重排。
	destroyed, _ := json.Marshal(map[string]any{"targetId": "t1"})
	ts.pushEvent(t, cdpWireMsg{Method: "Target.targetDestroyed", Params: destroyed})
	pages = waitPages(t, m, "default", 1)
	if pages[0].TargetID != "t2" || pages[0].Index != 0 {
		t.Fatalf("销毁后投影 = %+v", pages)
	}

	// visibility 上报 → 重探测回写（取值以 evaluate 为准）。
	ts.setEvaluate(`{"v":"visible","u":"https://z.test/","t":"可见页"}`)
	binding, _ := json.Marshal(map[string]any{"name": observerBindingName})
	ts.pushEvent(t, cdpWireMsg{Method: "Runtime.bindingCalled", SessionID: "test-sid", Params: binding})
	deadline := time.Now().Add(5 * time.Second)
	for {
		pages = m.view.row("default").Pages
		if len(pages) == 1 && pages[0].URL == "https://z.test/" && pages[0].Title == "可见页" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("visibility 回写未达: %+v", pages)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
