package browser

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// 带态 HTTP（authhttp，T1.4）单测：假引擎 + httptest.Server 断言文档验收四类——
// cookie 注入（含跨域不泄漏）/ UA / 截断 / 显式 header，另覆盖引擎 isError 传导、
// 空集照发与解析边界（含错误文案脱敏）。

// engineResultSample 引擎默认模式输出样例（0.0.83 核实形态：### Result 段 + 尾随
// ### Code 段，段间无空行）。
func engineResultSample(resultLines ...string) string {
	return engineResultSection + "\n" + strings.Join(resultLines, "\n") + "\n### Code\n```js\nawait page.context().cookies();\n```"
}

// authedToolFn 固定假引擎出参：cookie_list / evaluate 按传入文本应答，其余工具 "ok"。
func authedToolFn(cookieList, evaluate string) func(string, map[string]any) (string, bool) {
	return func(tool string, _ map[string]any) (string, bool) {
		switch tool {
		case EngineToolCookieList:
			return cookieList, false
		case EngineToolEvaluate:
			return evaluate, false
		}
		return "ok", false
	}
}

// probedUA evaluate 探测 UA 的样例出参。
var probedUA = engineResultSample(`"probed-ua"`)

// capturedRequest 服务端捕获的请求快照（handler 在服务 goroutine 写入，经互斥保护）。
type capturedRequest struct {
	mu     sync.Mutex
	hits   int
	Method string
	Cookie string
	UA     string
	Header http.Header
	Body   string
}

func (c *capturedRequest) record(r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hits++
	c.Method = r.Method
	c.Cookie = r.Header.Get("Cookie")
	c.UA = r.Header.Get("User-Agent")
	c.Header = r.Header.Clone()
	c.Body = string(b)
}

func (c *capturedRequest) snapshot() (hits int, method, cookie, ua, trace, body string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.Method, c.Cookie, c.UA, c.Header.Get("X-Trace"), c.Body
}

// newCapturingServer 建响应为固定状态码与体的捕获服务端。
func newCapturingServer(t *testing.T, status int, respBody string) (*httptest.Server, *capturedRequest) {
	t.Helper()
	rec := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func TestAuthedRequestCookieInjectionAndUA(t *testing.T) {
	ctl := withEngineCtl(t)
	m := newTestManager(t)
	srv, rec := newCapturingServer(t, http.StatusCreated, "created")
	// 目标 host-only（127.0.0.1）命中；其它站 host-only 与跨站 domain 均不得出现在
	// B 站请求头。github.com 行为回归锚点：host-only 声明域 ≠ 目标 host——若落锚
	// 退回「全量对目标 URL 一次注入」，此凭据会被 cookiejar 重锚到目标站泄漏
	// （Domain 空 cookie 锚定传入 URL 的 host）。
	ctl.toolFn = authedToolFn(engineResultSample(
		"sessionid=abc123 (domain: 127.0.0.1, path: /)",
		"gh_sess=leakme (domain: github.com, path: /)",
		"csrf=tok=en (domain: .example.com, path: /admin)",
	), engineResultSample(`"Chrome-UA-Probe/1.0"`))

	resp, err := m.AuthedRequest(context.Background(), "default", "POST", srv.URL+"/login",
		map[string]string{"X-Trace": "t1", "Content-Type": "application/json"}, `{"k":1}`)
	if err != nil {
		t.Fatalf("AuthedRequest: %v", err)
	}
	if resp.StatusCode != http.StatusCreated || resp.Body != "created" || resp.Truncated {
		t.Fatalf("resp = %+v, want 201/created/未截断", resp)
	}
	hits, method, cookie, ua, trace, body := rec.snapshot()
	if hits != 1 {
		t.Fatalf("服务端被请求 %d 次, want 1", hits)
	}
	if method != "POST" {
		t.Fatalf("Method = %q, want POST", method)
	}
	if cookie != "sessionid=abc123" {
		t.Fatalf("Cookie 头 = %q, want 仅目标 host-only 命中（跨站凭据被淘汰）", cookie)
	}
	if ua != "Chrome-UA-Probe/1.0" {
		t.Fatalf("UA = %q, want 探测值", ua)
	}
	if trace != "t1" {
		t.Fatalf("X-Trace = %q, want 显式透传", trace)
	}
	if body != `{"k":1}` {
		t.Fatalf("请求体 = %q", body)
	}
}

func TestAuthedRequestExplicitUAOverridesProbe(t *testing.T) {
	ctl := withEngineCtl(t)
	m := newTestManager(t)
	srv, rec := newCapturingServer(t, http.StatusOK, "ok")
	ctl.toolFn = authedToolFn(engineResultSample(engineNoCookiesText), probedUA)

	if _, err := m.AuthedRequest(context.Background(), "default", "GET", srv.URL,
		map[string]string{"User-Agent": "explicit-ua"}, ""); err != nil {
		t.Fatalf("AuthedRequest: %v", err)
	}
	if _, _, _, ua, _, _ := rec.snapshot(); ua != "explicit-ua" {
		t.Fatalf("UA = %q, want 显式值优先于探测值", ua)
	}
}

func TestAuthedRequestNoCookiesProceeds(t *testing.T) {
	ctl := withEngineCtl(t)
	m := newTestManager(t)
	srv, rec := newCapturingServer(t, http.StatusOK, "ok")
	ctl.toolFn = authedToolFn(engineResultSample(engineNoCookiesText), probedUA)

	// 未登录态（空集）合法：请求照发，登录态缺失由服务端响应呈现。
	if _, err := m.AuthedRequest(context.Background(), "default", "GET", srv.URL, nil, ""); err != nil {
		t.Fatalf("AuthedRequest: %v", err)
	}
	hits, _, cookie, _, _, _ := rec.snapshot()
	if hits != 1 || cookie != "" {
		t.Fatalf("hits = %d cookie = %q, want 空集照发", hits, cookie)
	}
}

func TestAuthedRequestEngineErrorStopsRequest(t *testing.T) {
	ctl := withEngineCtl(t)
	m := newTestManager(t)
	srv, rec := newCapturingServer(t, http.StatusOK, "ok")
	ctl.toolFn = func(tool string, _ map[string]any) (string, bool) {
		if tool == EngineToolCookieList {
			return "### Error\nmodal state", true
		}
		return "ok", false
	}

	_, err := m.AuthedRequest(context.Background(), "default", "GET", srv.URL, nil, "")
	if err == nil || !strings.Contains(err.Error(), "modal state") {
		t.Fatalf("err = %v, want 引擎 isError 文案显式传导", err)
	}
	if hits, _, _, _, _, _ := rec.snapshot(); hits != 0 {
		t.Fatalf("服务端被请求 %d 次, want 0（引擎错误即止，不发无凭据请求）", hits)
	}
}

func TestAuthedRequestTruncatesLargeBody(t *testing.T) {
	ctl := withEngineCtl(t)
	m := newTestManager(t)
	srv, _ := newCapturingServer(t, http.StatusOK, strings.Repeat("x", authedBodyLimit+1024))
	ctl.toolFn = authedToolFn(engineResultSample(engineNoCookiesText), probedUA)

	resp, err := m.AuthedRequest(context.Background(), "default", "GET", srv.URL, nil, "")
	if err != nil {
		t.Fatalf("AuthedRequest: %v", err)
	}
	if !resp.Truncated || len(resp.Body) != authedBodyLimit {
		t.Fatalf("Truncated = %v len = %d, want 截断至 %d", resp.Truncated, len(resp.Body), authedBodyLimit)
	}
}

func TestAuthedRequestRejectsBadURL(t *testing.T) {
	withEngineCtl(t)
	m := newTestManager(t)

	for _, rawURL := range []string{"ftp://example.com/x", "not-a-url", "http://"} {
		if _, err := m.AuthedRequest(context.Background(), "default", "GET", rawURL, nil, ""); err == nil {
			t.Fatalf("URL %q 应显式报错", rawURL)
		}
	}
}

func TestParseCookieList(t *testing.T) {
	// domain cookie / host-only（声明域保留原形态）/ 值含 '=' / path 透传。
	cookies, err := parseCookieList(engineResultSample(
		"tok=a=b=c (domain: .example.com, path: /a)",
		"sid=s1 (domain: example.com, path: /)",
	))
	if err != nil {
		t.Fatalf("parseCookieList: %v", err)
	}
	if len(cookies) != 2 {
		t.Fatalf("cookies = %d 条, want 2", len(cookies))
	}
	if cookies[0].name != "tok" || cookies[0].value != "a=b=c" || cookies[0].declaredDomain != ".example.com" || cookies[0].path != "/a" {
		t.Fatalf("cookies[0] = %+v, want domain cookie（值含 '='，声明域保留前导点）", cookies[0])
	}
	if cookies[1].declaredDomain != "example.com" || cookies[1].path != "/" {
		t.Fatalf("cookies[1] = %+v, want host-only（声明域无前导点）", cookies[1])
	}

	// 空集两种形态：No cookies found / 空 Result 段。
	for name, text := range map[string]string{
		"No cookies found": engineResultSample(engineNoCookiesText),
		"空 Result 段":       engineResultSample(),
	} {
		cookies, err = parseCookieList(text)
		if err != nil || cookies != nil {
			t.Fatalf("%s: cookies = %+v err = %v, want 空集", name, cookies, err)
		}
	}

	// 缺 Result 段 = 格式漂移显式报错且不回显全文（其中可能是真实凭据）；### Code 段
	// 内容不参与解析。
	if _, err := parseCookieList("### Code\n```js\nx\n```"); err == nil || !strings.Contains(err.Error(), engineResultSection) || strings.Contains(err.Error(), "```") {
		t.Fatalf("缺 Result 段 err = %v, want 显式报错且不回显全文", err)
	}
	// 坏行显式报错但脱敏：引用 name，不回显值（D3 凭据不外传）。
	if _, err := parseCookieList(engineResultSample("sid=secret-value")); err == nil || !strings.Contains(err.Error(), "sid") || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("坏行 err = %v, want 引用 name 且不回显值", err)
	}
	cookies, err = parseCookieList(engineResultSample("a=b (domain: d, path: /)"))
	if err != nil || len(cookies) != 1 {
		t.Fatalf("Code 段干扰 err = %v cookies = %d, want 仅解析 Result 段", err, len(cookies))
	}
}

func TestParseEvalString(t *testing.T) {
	for raw, want := range map[string]string{
		`"Chrome-UA/1.0"`: "Chrome-UA/1.0",
		`"a\"b"`:          `a"b`,
	} {
		got, err := parseEvalString(engineResultSample(raw))
		if err != nil || got != want {
			t.Fatalf("parseEvalString(%q) = %q, %v, want %q", raw, got, err, want)
		}
	}
	// 缺段 / 非字符串结果均显式报错且不回显内容（evaluate 结果可能是 localStorage
	// token 等敏感值）。
	if _, err := parseEvalString("### Code\nx"); err == nil || strings.Contains(err.Error(), "x") {
		t.Fatalf("缺 Result 段应显式报错且不回显内容, got %v", err)
	}
	if _, err := parseEvalString(engineResultSample(`{"obj":1}`)); err == nil || strings.Contains(err.Error(), "obj") {
		t.Fatalf("非字符串结果应显式报错且不回显内容, got %v", err)
	}
}
