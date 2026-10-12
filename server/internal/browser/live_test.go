package browser

// live.go 单测：DevToolsActivePort 解析、target 匹配、帧箱跳帧/多订户、拨号自愈预算
// 与协议基线（帧号字段 sessionId、逐帧 ack）——httptest 模拟 DevTools HTTP/ws 面，
// 不依赖真实浏览器（真实链路冒烟归 spike 族 real_cdp_test.go）。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// testJPEG 任意字节载荷（生产链路只做 base64 解码，不做图像解析）。
var testJPEG = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}

func TestReadDevToolsPort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, devToolsActivePortFile)

	if _, _, err := readDevToolsPort(dir); err == nil {
		t.Fatal("文件缺失应报错（浏览器未活表象）")
	}
	if err := os.WriteFile(path, []byte("9222\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readDevToolsPort(dir); err == nil {
		t.Fatal("单行内容应报错")
	}
	if err := os.WriteFile(path, []byte("abc\n/devtools/browser/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readDevToolsPort(dir); err == nil {
		t.Fatal("端口损坏应报错")
	}
	if err := os.WriteFile(path, []byte("9222\n/devtools/browser/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	port, wsPath, err := readDevToolsPort(dir)
	if err != nil || port != 9222 || wsPath != "/devtools/browser/x" {
		t.Fatalf("正常解析 = %d, %q, %v", port, wsPath, err)
	}
}

func TestMatchPageTarget(t *testing.T) {
	targets := []cdpTargetInfo{
		{ID: "t0", Type: "page", URL: "about:blank", Title: "空白页"},
		{ID: "t1", Type: "page", URL: "https://x.test/", Title: "例页"},
		{ID: "t2", Type: "iframe", URL: "https://x.test/frame", Title: "内嵌"},
	}
	page := PageView{Index: 1, Title: "例页", URL: "https://x.test/"}

	tg, err := matchPageTarget(page, targets)
	if err != nil || tg.ID != "t1" {
		t.Fatalf("精确匹配 = %+v, %v; want t1", tg, err)
	}

	ambiguous := append([]cdpTargetInfo{{ID: "a", Type: "page", URL: page.URL}}, targets...)
	tg, err = matchPageTarget(page, ambiguous)
	if err != nil || tg.ID != "a" {
		t.Fatalf("歧义应取首个 = %+v, %v", tg, err)
	}

	_, err = matchPageTarget(PageView{URL: "https://gone.test/"}, targets)
	if err == nil {
		t.Fatal("未命中应显式报错")
	}
}

func TestFrameMailbox(t *testing.T) {
	mb := newFrameMailbox()
	if _, _, ok := mb.latest(); ok {
		t.Fatal("空箱不应有帧")
	}

	// keep-latest：连续投递三帧，latest 恒为最新（慢订户跳帧）。
	for i := 1; i <= 3; i++ {
		mb.deliver(LiveFrame{Metadata: frameMeta{DeviceWidth: i}})
	}
	f, gen, ok := mb.latest()
	if !ok || f.Metadata.DeviceWidth != 3 || gen != 3 {
		t.Fatalf("latest = %+v gen=%d ok=%v; want 3/3/true", f, gen, ok)
	}

	// 多订户同帧全唤醒；ctx 取消可中断等待。
	const subscribers = 3
	_, curGen, _ := mb.latest()
	errs := make(chan error, subscribers)
	for i := 0; i < subscribers; i++ {
		go func() {
			wctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			errs <- mb.waitChange(wctx, curGen)
		}()
	}
	time.Sleep(50 * time.Millisecond) // 等订户进入等待
	mb.deliver(LiveFrame{})
	for i := 0; i < subscribers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("订户未唤醒: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, curGen, _ = mb.latest()
	if err := mb.waitChange(ctx, curGen); err == nil {
		t.Fatal("ctx 取消应中断等待")
	}
}

// cdpTestServer 模拟 DevTools HTTP 面（/json/version、/json/list）与 ws JSON-RPC 面
// （按 method 罐头应答 + acks/calls 记账 + 事件注入通道）。
type cdpTestServer struct {
	srv    *httptest.Server
	port   int
	mu     sync.Mutex
	calls  []string
	acks   []int64
	events chan cdpWireMsg
}

func newCDPTestServer(t *testing.T, targets []cdpTargetInfo) *cdpTestServer {
	t.Helper()
	s := &cdpTestServer{events: make(chan cdpWireMsg, 16)}

	var wsMu sync.Mutex
	var conns []*websocket.Conn

	mux := http.NewServeMux()
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"webSocketDebuggerUrl": "ws://" + r.Host + "/devtools/browser/test",
		})
	})
	mux.HandleFunc("/json/list", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(targets)
	})
	mux.HandleFunc("/devtools/browser/test", func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		wsMu.Lock()
		conns = append(conns, conn)
		wsMu.Unlock()
		go func() { // 事件注入泵（写侧与应答共用 wsMu 单写者）
			for ev := range s.events {
				wsMu.Lock()
				err := conn.WriteJSON(ev)
				wsMu.Unlock()
				if err != nil {
					return
				}
			}
		}()
		for {
			var msg cdpWireMsg
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			if msg.ID == 0 {
				continue
			}
			s.mu.Lock()
			s.calls = append(s.calls, msg.Method)
			s.mu.Unlock()
			result := s.canned(t, msg.Method, msg.Params)
			wsMu.Lock()
			_ = conn.WriteJSON(map[string]any{"id": msg.ID, "result": result})
			wsMu.Unlock()
		}
	})
	s.srv = httptest.NewServer(mux)
	s.port, _ = strconv.Atoi(strings.TrimPrefix(s.srv.URL, "http://127.0.0.1:"))
	t.Cleanup(func() {
		wsMu.Lock()
		for _, c := range conns {
			_ = c.Close()
		}
		wsMu.Unlock()
		s.srv.Close()
		close(s.events)
	})
	return s
}

// canned 按 method 应答（ack 记账帧号——协议基线「帧号字段实名 sessionId」的锚点）。
func (s *cdpTestServer) canned(t *testing.T, method string, params json.RawMessage) map[string]any {
	switch method {
	case "Target.attachToTarget":
		return map[string]any{"sessionId": "test-sid"}
	case "Page.captureScreenshot":
		return map[string]any{"data": base64.StdEncoding.EncodeToString(testJPEG)}
	case "Page.screencastFrameAck":
		var p struct {
			SessionID int64 `json:"sessionId"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			t.Errorf("ack 参数解析: %v", err)
		}
		s.mu.Lock()
		s.acks = append(s.acks, p.SessionID)
		s.mu.Unlock()
		return map[string]any{}
	default: // setDiscoverTargets / Page.enable / startScreencast 等空结果
		return map[string]any{}
	}
}

// pushEvent 注入一条 CDP 事件（sessionId 字段即会话归属）。
func (s *cdpTestServer) pushEvent(t *testing.T, ev cdpWireMsg) {
	t.Helper()
	select {
	case s.events <- ev:
	case <-time.After(5 * time.Second):
		t.Fatal("事件注入超时（ws 写侧阻塞）")
	}
}

func (s *cdpTestServer) acked(t *testing.T, frame int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		found := false
		for _, n := range s.acks {
			if n == frame {
				found = true
			}
		}
		s.mu.Unlock()
		if found {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("帧 %d 未在 5s 内收到 ack（acks=%v）", frame, s.acks)
}

func (s *cdpTestServer) called(method string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.calls {
		if m == method {
			return true
		}
	}
	return false
}

// writePortFile 在 profile 目录落盘 DevToolsActivePort（port 缺省用测试服务器端口）。
func writePortFile(t *testing.T, profileDir string, port int) {
	t.Helper()
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("%d\n/devtools/browser/test-uuid\n", port)
	if err := os.WriteFile(filepath.Join(profileDir, devToolsActivePortFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// zeroLiveDelays 自愈退避覆写为 1ms（收尾恢复——退避表是包级 var）。
func zeroLiveDelays(t *testing.T) {
	t.Helper()
	orig := liveRetryDelays
	liveRetryDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { liveRetryDelays = orig })
}

func TestConnectLiveE2E(t *testing.T) {
	ts := newCDPTestServer(t, []cdpTargetInfo{
		{ID: "t0", Type: "page", URL: "about:blank", Title: "空白页"},
		{ID: "t1", Type: "page", URL: "https://x.test/", Title: "例页"},
		{ID: "t2", Type: "iframe", URL: "https://x.test/frame", Title: "内嵌"},
	})
	withTestRoot(t)
	dir, err := ProfileDir("default")
	if err != nil {
		t.Fatal(err)
	}
	writePortFile(t, dir, ts.port)
	zeroLiveDelays(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	v, err := connectLive(ctx, "default", PageView{Index: 1, Title: "例页", URL: "https://x.test/"}, nil, nil)
	if err != nil {
		t.Fatalf("connectLive: %v", err)
	}
	t.Cleanup(func() { _ = v.conn.close() })
	if v.session != "test-sid" {
		t.Fatalf("session = %q, want test-sid", v.session)
	}
	for _, method := range []string{"Target.setDiscoverTargets", "Target.attachToTarget", "Page.captureScreenshot", "Page.startScreencast"} {
		if !ts.called(method) {
			t.Fatalf("拨号序缺 %s（calls=%v）", method, ts.calls)
		}
	}

	// 初始帧（captureScreenshot 兜底，静态页不白屏）。
	if f, gen, ok := v.frames.latest(); !ok || len(f.Data) == 0 || gen != 1 {
		t.Fatalf("初始帧 = len=%d gen=%d ok=%v", len(f.Data), gen, ok)
	}

	// screencast 帧 → 帧箱投递 + 帧号 sessionId 字段逐帧 ack（协议基线锚点）。
	params, _ := json.Marshal(map[string]any{
		"sessionId": 7,
		"data":      base64.StdEncoding.EncodeToString(testJPEG),
		"metadata":  map[string]any{"deviceWidth": 800, "deviceHeight": 600, "offsetTop": 12},
	})
	ts.pushEvent(t, cdpWireMsg{Method: "Page.screencastFrame", SessionID: "test-sid", Params: params})
	_, gen, ok := v.frames.latest()
	deadline := time.Now().Add(5 * time.Second)
	for ok && gen < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("5s 内未收到投递帧（gen 停在 %d）", gen)
		}
		wctx, wcancel := context.WithTimeout(ctx, time.Second)
		_ = v.frames.waitChange(wctx, gen)
		wcancel()
		_, gen, ok = v.frames.latest()
	}
	if f, _, ok2 := v.frames.latest(); !ok2 || f.Metadata.DeviceWidth != 800 || f.Metadata.OffsetTop != 12 {
		t.Fatalf("投递帧缺失或 metadata 异常: %+v ok=%v", f, ok2)
	}
	ts.acked(t, 7)

	// target 生命周期回显与关闭终态。
	ts.pushEvent(t, cdpWireMsg{Method: "Target.targetInfoChanged", Params: json.RawMessage(
		`{"targetInfo":{"targetId":"t1","url":"https://y.test/","title":"新页"}}`)})
	select {
	case up := <-v.targets:
		if up.URL != "https://y.test/" || up.Title != "新页" || up.Closed {
			t.Fatalf("target 回显 = %+v", up)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("target 变更通知未送达")
	}
	ts.pushEvent(t, cdpWireMsg{Method: "Target.targetDestroyed", Params: json.RawMessage(`{"targetId":"t1"}`)})
	select {
	case up := <-v.targets:
		if !up.Closed {
			t.Fatalf("target 关闭终态 = %+v", up)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("target 关闭通知未送达")
	}
}

// TestConnectLiveSelfHealWake 残档/缺失自愈：首次失败唤醒浏览器（wake 落盘端口文件）
// 后重拨成功，busy 拒绝按「浏览器活着」正交解读不阻断重试。
func TestConnectLiveSelfHealWake(t *testing.T) {
	for _, tc := range []struct {
		name      string
		busyFirst bool
		wantWake  int
	}{
		{"缺失唤醒重拨", false, 1},
		{"busy 拒绝容忍", true, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := newCDPTestServer(t, []cdpTargetInfo{{ID: "t1", Type: "page", URL: "https://x.test/", Title: "例页"}})
			withTestRoot(t)
			dir, err := ProfileDir("default")
			if err != nil {
				t.Fatal(err)
			}
			zeroLiveDelays(t)

			var wakeMu sync.Mutex
			wakeCount := 0
			wake := func(ctx context.Context) error {
				wakeMu.Lock()
				defer wakeMu.Unlock()
				wakeCount++
				if tc.busyFirst && wakeCount == 1 {
					return ErrSessionBusy
				}
				writePortFile(t, dir, ts.port)
				return nil
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			v, err := connectLive(ctx, "default", PageView{URL: "https://x.test/"}, wake, nil)
			if err != nil {
				t.Fatalf("自愈后 connectLive: %v", err)
			}
			t.Cleanup(func() { _ = v.conn.close() })
			wakeMu.Lock()
			got := wakeCount
			wakeMu.Unlock()
			if got != tc.wantWake {
				t.Fatalf("wake 调用 %d 次, want %d", got, tc.wantWake)
			}
		})
	}
}

// TestConnectLiveExhausted 自愈预算耗尽：退避表 3 项 = 3 次唤醒重试后显式失败。
func TestConnectLiveExhausted(t *testing.T) {
	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadPort := dead.Addr().(*net.TCPAddr).Port
	_ = dead.Close() // 只借端口位，随即关闭制造拒连

	withTestRoot(t)
	dir, err := ProfileDir("default")
	if err != nil {
		t.Fatal(err)
	}
	writePortFile(t, dir, deadPort) // 残档端口：拨号恒失败
	zeroLiveDelays(t)

	wakeCount := 0
	wake := func(ctx context.Context) error {
		wakeCount++
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := connectLive(ctx, "default", PageView{URL: "https://x.test/"}, wake, nil); err == nil {
		t.Fatal("预算耗尽应显式失败")
	}
	if wakeCount != len(liveRetryDelays) {
		t.Fatalf("wake 调用 %d 次, want %d", wakeCount, len(liveRetryDelays))
	}
}
