// 带态 HTTP（T1.4，D3 登录态零持久化）：从活引擎实时拉取当前登录态 cookies，按声明域
// 逐条落锚进 net/http/cookiejar（标准域匹配）后 Go http.Client 直发——纯内存、不落盘、
// 不手拼 Cookie header。防跨站泄漏的保证点 = 按声明域落锚（host-only 锚定真实归属
// host——cookiejar 对 Domain 空的 cookie 会锚定传入 URL host，全量对目标落锚会把其它
// 站 host-only 凭据重锚到目标站，见 injectCookies）+ 发送期 jar 域匹配（引擎列表输出
// 不含 secure 标志，用户拍板一律注入、无 scheme 门槛）；解析格式漂移一律显式报错且
// 错误文案脱敏（凭据内容不随错误外传），不静默发无凭据请求。

package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	// authedBodyLimit 带态响应体截断上限（超出截断并置 Truncated）。
	authedBodyLimit = 256 << 10

	// engineResultSection 引擎默认模式输出的 Result 段头（`### ${title}` 逐段序列化，
	// 0.0.83 源码核实；段与段之间无空行，下一段以 "### " 起始）。
	engineResultSection = "### Result"

	// engineNoCookiesText cookie_list 空集文案（0.0.83 源码核实）——未登录态合法，
	// 请求照发，登录态缺失由服务端响应呈现。
	engineNoCookiesText = "No cookies found"
)

// authedHTTPTimeout 带态请求的 HTTP 直发段预算（var 化供测试覆写；引擎调用段预算已由
// Forward per-call 超时约束）。
var authedHTTPTimeout = 30 * time.Second

// cookieLineRe browser_cookie_list 输出行（0.0.83 源码核实：
// `${name}=${value} (domain: ${domain}, path: ${path})` 逐行 join）。name 不含空白与
// '='（cookie token），value 按首个 '=' 切分（可含 '='）；RFC 6265 值不含空白，
// " (domain: " 分隔天然无歧义。域名前导点 = domain cookie（匹配子域），无点 = host-only。
var cookieLineRe = regexp.MustCompile(`^([^\s=]+)=(.*) \(domain: (.*), path: (.*)\)$`)

// AuthedResponse 带态请求结果（纯数据；响应体超 authedBodyLimit 截断并置 Truncated）。
type AuthedResponse struct {
	StatusCode int         `json:"statusCode"`
	Header     http.Header `json:"header"`
	Body       string      `json:"body"`
	Truncated  bool        `json:"truncated"`
}

// AuthedRequest 带态请求统一入口：ensure 引擎 → browser_cookie_list 实时拉登录态（纯
// 内存，勿走 browser_storage_state 落文件语义）→ 解析后按声明域落锚进 cookiejar（见
// injectCookies）→
// browser_evaluate 探测浏览器真实 UA → http.Client 直发（默认跟随重定向，浏览器同款
// 语义）。显式 headers 全透传，显式 User-Agent 优先于探测值（token 类凭据由调用方从
// localStorage 提取后传入，本函数不碰 localStorage）。cookies 与 UA 为两次独立调用
// 的尽力快照，中间被并发调用穿插属预期。
func (m *Manager) AuthedRequest(ctx context.Context, profile, method, rawURL string, headers map[string]string, body string) (*AuthedResponse, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("目标 URL 非法: %q", rawURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("目标 URL 协议不受支持（仅 http/https）: %q", u.Scheme)
	}
	cookies, err := m.fetchAuthCookies(ctx, profile)
	if err != nil {
		return nil, err
	}
	ua, err := m.fetchBrowserUA(ctx, profile)
	if err != nil {
		return nil, err
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("cookie jar 创建失败: %w", err)
	}
	// 落锚后「A 站凭据不随 B 站请求发出」由发送期 jar 域匹配保证（用户拍板：无
	// scheme 门槛，一律注入）。
	injectCookies(jar, cookies)

	req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造带态请求失败: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", ua)
	}
	client := &http.Client{Timeout: authedHTTPTimeout, Jar: jar}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("带态请求发送失败: %w", err)
	}
	defer resp.Body.Close()
	data, truncated, err := readBodyTruncated(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取带态响应失败: %w", err)
	}
	return &AuthedResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Body:       string(data),
		Truncated:  truncated,
	}, nil
}

// fetchAuthCookies 经 Forward 拉引擎 cookie 全集（isError 显式报错——引擎错误一律
// isError 文本）并解析为可注入的 cookie 列表。
func (m *Manager) fetchAuthCookies(ctx context.Context, profile string) ([]engineCookie, error) {
	res, err := m.Forward(ctx, profile, EngineToolCookieList, nil, "")
	if err != nil {
		return nil, fmt.Errorf("拉取浏览器 cookies: %w", err)
	}
	if res.IsError {
		return nil, fmt.Errorf("拉取浏览器 cookies 失败: %s", resultText(res))
	}
	return parseCookieList(resultText(res))
}

// fetchBrowserUA 探测引擎浏览器的真实 UA（evaluate navigator.userAgent；冷启动无 tab
// 时引擎 ensureTab 自动补 about:blank——0.0.83 源码核实，副作用无害）。
func (m *Manager) fetchBrowserUA(ctx context.Context, profile string) (string, error) {
	res, err := m.Forward(ctx, profile, EngineToolEvaluate,
		map[string]any{"function": "() => navigator.userAgent"}, "")
	if err != nil {
		return "", fmt.Errorf("探测浏览器 UA: %w", err)
	}
	if res.IsError {
		return "", fmt.Errorf("探测浏览器 UA 失败: %s", resultText(res))
	}
	return parseEvalString(resultText(res))
}

// engineCookie 引擎 cookie 行解析结果（纯数据）。declaredDomain 保留引擎声明的原始
// 形态：前导点 = domain cookie，无点 = host-only——落锚分组与 host-only 归属 host
// 的唯一依据（不能提前丢：cookiejar 的 Domain 空值语义是「锚定传入 URL 的 host」）。
type engineCookie struct {
	name           string
	value          string
	declaredDomain string
	path           string
}

// parseCookieList 解析引擎 browser_cookie_list 文本 → 可注入的 cookie 列表。
// "No cookies found" / 空 Result 段 = 空集（nil，无 error）；任何不可解析行 = 引擎输出
// 格式漂移，显式报错（不静默发无凭据请求），错误文案脱敏不回显凭据值。默认模式输出
// 的 Code 段等其余段不解析。
func parseCookieList(text string) ([]engineCookie, error) {
	lines, found := resultSectionLines(text)
	if !found {
		return nil, fmt.Errorf("cookie 输出缺少 %s 段（引擎输出格式可能已变化，内容已隐去）", engineResultSection)
	}
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == engineNoCookiesText) {
		return nil, nil
	}
	cookies := make([]engineCookie, 0, len(lines))
	for _, line := range lines {
		m := cookieLineRe.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("cookie 行解析失败（引擎输出格式可能已变化，值已隐去）: %s", redactCookieLine(line))
		}
		cookies = append(cookies, engineCookie{name: m[1], value: m[2], declaredDomain: m[3], path: m[4]})
	}
	return cookies, nil
}

// redactCookieLine 解析错误回显脱敏：只保留首个 '=' 前的 name，值以字节数示意——
// 错误文案会随上层日志与 MCP 出参扩散，凭据内容不落错误（D3）。格式漂移恰发生在
// 已登录时刻，坏行里的值就是真实凭据。
func redactCookieLine(line string) string {
	if i := strings.IndexByte(line, '='); i >= 0 {
		return fmt.Sprintf("%s=<%d 字节已隐去>", strings.TrimSpace(line[:i]), len(line)-i-1)
	}
	return "<整行已隐去>"
}

// injectCookies 按声明域逐条落锚：cookiejar 对 Domain 空的 cookie 锚定到传入 URL 的
// host（stdlib domainAndType 语义）——若全量对目标 URL 一次落锚，profile 内其它站点
// 的 host-only 凭据会被重锚到目标站并随请求发出（跨站泄漏）。故 host-only 以其真实
// 归属 host 合成 URL 落锚（Domain 留空），domain cookie 以去点域名合成落锚（Domain
// 保留前导点供 jar 判型）；发送期隔离完全交给 jar 域匹配（浏览器同款语义，跨域重定向
// 后按目标域补发凭据由此自然成立）。
func injectCookies(jar http.CookieJar, cookies []engineCookie) {
	u := &url.URL{Scheme: "https"}
	for _, c := range cookies {
		domain, host := "", c.declaredDomain
		if strings.HasPrefix(host, ".") {
			domain, host = host, host[1:]
		}
		u.Host = host
		jar.SetCookies(u, []*http.Cookie{{Name: c.name, Value: c.value, Domain: domain, Path: c.path}})
	}
}

// parseEvalString 解析 browser_evaluate 的字符串结果（出参为 JSON.stringify(result)
// 的 Result 段——0.0.83 源码核实）：拼回多行后 JSON 反序列化得原值。错误文案脱敏——
// evaluate 结果可能是 localStorage token 等敏感值，不随错误回显。
func parseEvalString(text string) (string, error) {
	lines, found := resultSectionLines(text)
	if !found {
		return "", fmt.Errorf("evaluate 输出缺少 %s 段（引擎输出格式可能已变化，内容已隐去）", engineResultSection)
	}
	var s string
	if err := json.Unmarshal([]byte(strings.Join(lines, "\n")), &s); err != nil {
		return "", fmt.Errorf("evaluate 结果解析失败（引擎输出格式可能已变化，内容已隐去）")
	}
	return s, nil
}

// resultSectionLines 提取引擎默认模式输出的 Result 段行（下一段 "### " 或文本末尾
// 止，跳过空行）。found = 段头是否存在。
func resultSectionLines(text string) (lines []string, found bool) {
	for line := range strings.SplitSeq(text, "\n") {
		if line == engineResultSection {
			found = true
			continue
		}
		if found && strings.HasPrefix(line, "### ") {
			break
		}
		if found && strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines, found
}

// bodyReader 空 body 走 nil（GET 等无体请求不带 Content-Length）。
func bodyReader(body string) io.Reader {
	if body == "" {
		return nil
	}
	return strings.NewReader(body)
}

// readBodyTruncated 读取响应体至 authedBodyLimit+1 判截断（不整读超大响应）。
func readBodyTruncated(r io.Reader) (string, bool, error) {
	data, err := io.ReadAll(io.LimitReader(r, authedBodyLimit+1))
	if err != nil {
		return "", false, err
	}
	if len(data) > authedBodyLimit {
		return string(data[:authedBodyLimit]), true, nil
	}
	return string(data), false, nil
}
