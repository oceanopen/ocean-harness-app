// http.go 放 githost 包共用的 HTTP 调用 helper（net/http 标准库，零外部依赖）。
// 错误折叠风格参考 gitutil.gitRun（合并关键信息生成可读 error），
// 但 token 仅在 header 传递、从不进入 error 文案（url 也不含 token）。

package githost

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// httpClient 包内共享 HTTP client。设 30s 超时避免 hang（项目既有 http.Server 但无 Client 超时先例）。
var httpClient = &http.Client{Timeout: 30 * time.Second}

// doJSON 发送 method 请求到 url（带 headers、可选 body），将响应 JSON 解码进 out（out 为 nil 则忽略响应体）。
// 失败返回中文 error：
//   - 构造/调用/解析失败：fmt.Errorf("...: %w", err)，保留 errors.Is 链；
//   - HTTP >= 400：折叠 method+url+status+body 片段（%s，不含 header/token，body 限 512 字节）。
//
// 透传 ctx 以支持调用方取消（与 service 层 svc.Context 对齐）。
func doJSON(ctx context.Context, method, url string, headers map[string]string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return fmt.Errorf("githost 构造请求失败: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("githost 调用失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		snip, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("githost %s %s: HTTP %d: %s", method, url, resp.StatusCode, strings.TrimSpace(string(snip)))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("githost 解析响应失败: %w", err)
		}
	} else {
		// out==nil（如 merge 成功响应）：drain body 使底层 keep-alive 连接可回连接池复用，
		// 避免每次 merge 新建 TCP+TLS（http.Client 文档：未读完的 body 会导致连接被丢弃）。
		_, _ = io.Copy(io.Discard, resp.Body)
	}
	return nil
}
