// Package githubapi 封装 GitHub REST API v3 的最小客户端（T4.1 GitHub MCP 工具的外部
// 服务层）：纯 net/http + 静态 PAT（Authorization: Bearer），不引入 oauth2 / go-github
// 依赖——PAT 是静态令牌，oauth2 全套流程用不上；仅三个端点，go-github 过重。
//
// 无 DB 依赖（owner/repo 由调用方经 gitutil.ParseRemoteURL 从 remote_url 解析），可独立
// 单测。本期仅支持 github.com（API base 固定 api.github.com）；后续 gitee / gitlab 等
// 平台按「各 host 单独适配」原则另立包，本包不参数化 base URL。
package githubapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// apiBase 是 github.com 的 REST API v3 根（GitHub Enterprise 的 /api/v3 形态待后续支持）。
const apiBase = "https://api.github.com"

// Client 是 GitHub REST 客户端。零值不可用，经 NewClient 构造；无内部可变状态、
// 并发安全（http.Client 语义）。
type Client struct {
	httpClient *http.Client
	pat        string
}

// NewClient 以 Personal Access Token 构造客户端（PAT 为空时请求会收到 GitHub 401，
// 由调用方在更早处拦截给「未配置」引导）。
func NewClient(pat string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		pat:        pat,
	}
}

// githubError 是 GitHub 错误响应体（截取 message 字段足够定位）。
type githubError struct {
	Message string `json:"message"`
}

// doRequest 执行一次 API 请求：JSON 序列化入参 → 带认证头 → 非透明处理错误码
// （401 → PAT 无效；404 → 目标不存在；422 → 参数/分支问题，message 最有信息量）
// → JSON 反序列化出参。路径形如 "/repos/{owner}/{repo}/pulls"。
func (c *Client) doRequest(method, path string, in, out interface{}) error {
	var body io.Reader
	if in != nil {
		buf, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("序列化请求失败: %w", err)
		}
		body = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, apiBase+path, body)
	if err != nil {
		return fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.pat)
	req.Header.Set("Accept", "application/vnd.github+json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("请求 GitHub API 失败（网络）: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB 上限防异常响应
	if err != nil {
		return fmt.Errorf("读取 GitHub 响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var ge githubError
		_ = json.Unmarshal(raw, &ge) // 错误体解析失败不掩盖 HTTP 状态码信息
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return fmt.Errorf("GitHub 认证失败（PAT 无效或过期，请在设置 → 个人中心更新）")
		case http.StatusNotFound:
			return fmt.Errorf("GitHub 资源不存在: %s %s: %s", method, path, ge.Message)
		default:
			return fmt.Errorf("GitHub API 失败: HTTP %d: %s", resp.StatusCode, ge.Message)
		}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("解析 GitHub 响应失败: %w", err)
	}
	return nil
}
