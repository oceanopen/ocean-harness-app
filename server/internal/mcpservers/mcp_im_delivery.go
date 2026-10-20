/*
 * Package mcpservers 内的 im 投递端点：per-token MCP server 承载 send_file 工具，供
 * headless claude 回合把本机文件即时推给 IM 用户（独立气泡，不占流式回复）。token 由
 * bot 编排器在回合受理时登记（DeliveryRegistry）、回合收尾注销，URL 路径尾段即 token。
 * 与 ocean_harness 单 server 模式不同：工具闭包需捕获 per-request 的 token，故按 token
 * 惰性建 server 并缓存（回合内多次 HTTP 往复共享同一 server 实例）。
 */
package mcpservers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"ocean-harness/server/internal/bot"
)

var (
	imDeliveryMu       sync.Mutex
	imDeliveryRegistry *bot.DeliveryRegistry
)

// SetIMDeliveryRegistry 装配根注入投递注册表（main 在路由装配前调用；nil = 投递链路禁用）。
func SetIMDeliveryRegistry(reg *bot.DeliveryRegistry) {
	imDeliveryMu.Lock()
	defer imDeliveryMu.Unlock()
	imDeliveryRegistry = reg
}

// ImDeliveryPathPrefix im 投递端点的 gin 路由前缀（token 为路径尾段，变更须两端同步；router 挂载同源引用）。
const ImDeliveryPathPrefix = "/mcp/streamableHttp/im/"

// imDeliveryToken 取请求路径尾段的 token（多段/为空 = 非法）。
func imDeliveryToken(r *http.Request) string {
	rest := strings.TrimPrefix(r.URL.Path, ImDeliveryPathPrefix)
	if rest == "" || strings.Contains(rest, "/") {
		return ""
	}
	return rest
}

/*
 * imServerCache token → server 缓存：同一回合的 initialize/tools-list/tools-call 往复
 * 必须落在同一 server 实例（工具闭包绑定 token）；token 逐回合唯一，跨回合不命中。
 */
type imServerCache struct {
	mu      sync.Mutex
	byToken map[string]imServerEntry
}

type imServerEntry struct {
	server   *mcp.Server
	deadline time.Time
}

// imServerTTL server 缓存寿命（覆盖超长回合；注册表 TTL 独立且更短，Lookup 才是权限门）。
const imServerTTL = 24 * time.Hour

// imServerSweepThreshold 缓存条目数超过阈值时全量清扫过期（惰性回收，免定时器）。
const imServerSweepThreshold = 128

var imServers = &imServerCache{byToken: make(map[string]imServerEntry)}

func (c *imServerCache) get(token string) (*mcp.Server, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.byToken[token]
	if !ok {
		return nil, false
	}
	if time.Now().After(e.deadline) {
		delete(c.byToken, token)
		return nil, false
	}
	return e.server, true
}

func (c *imServerCache) put(token string, s *mcp.Server) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.byToken) >= imServerSweepThreshold {
		now := time.Now()
		for k, e := range c.byToken {
			if now.After(e.deadline) {
				delete(c.byToken, k)
			}
		}
	}
	c.byToken[token] = imServerEntry{server: s, deadline: time.Now().Add(imServerTTL)}
}

/*
 * McpImDeliveryStreamableHTTPHandler 构造 im 投递端点 handler（router 调用一次）：
 * 每请求按路径 token 解析 server，未装配注册表/token 非法返回 nil（SDK 以 404 应答）。
 */
func McpImDeliveryStreamableHTTPHandler() http.Handler {
	return mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		imDeliveryMu.Lock()
		reg := imDeliveryRegistry
		imDeliveryMu.Unlock()
		if reg == nil {
			return nil
		}
		token := imDeliveryToken(r)
		if token == "" {
			return nil
		}
		if s, ok := imServers.get(token); ok {
			return s
		}
		s := newIMDeliveryServer(token)
		imServers.put(token, s)
		return s
	}, &mcp.StreamableHTTPOptions{SessionTimeout: mcpSessionTimeout})
}

// newIMDeliveryServer 建单工具 server：send_file 闭包捕获 token，调用时经注册表 Lookup 取投递面。
func newIMDeliveryServer(token string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "im",
		Version: "v1.0.0",
		Title:   "IM 会话文件投递",
	}, nil)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "send_file",
		Description: imSendFileToolDesc,
	}, func(_ context.Context, _ *mcp.CallToolRequest, in imSendFileIn) (*mcp.CallToolResult, imSendFileOut, error) {
		return imSendFileHandler(token, in)
	})
	return s
}

// imSendFileToolDesc 模型可见的工具说明（即提示词：说明何时用、参数怎么填、限额与次数）。
const imSendFileToolDesc = "把一个本机文件作为独立消息发送给当前 IM 会话的用户（不打断正在流式输出的回复）。" +
	"path 为文件在本机的绝对路径（生成到任意位置均可，由你决定）；caption 可选，一句话说明。单文件上限 20MB。回合进行中可多次调用。"

// imSendFileIn send_file 入参（字段描述经 jsonschema tag 反射成 InputSchema）。
type imSendFileIn struct {
	Path    string `json:"path" jsonschema:"文件在本机的绝对路径（必填）"`
	Caption string `json:"caption,omitempty" jsonschema:"可选，一句话说明该文件是什么"`
}

// imSendFileOut send_file 出参。
type imSendFileOut struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

/*
 * imSendFileHandler 投递执行：文件经回合自身回复流推送（渠道素材上传知识全在渠道实现内）；
 * 附言失败不回滚文件（模型可改在正文说明），只如实回告。
 */
func imSendFileHandler(token string, in imSendFileIn) (*mcp.CallToolResult, imSendFileOut, error) {
	if strings.TrimSpace(in.Path) == "" {
		return nil, imSendFileOut{}, errors.New("path 不能为空")
	}
	imDeliveryMu.Lock()
	reg := imDeliveryRegistry
	imDeliveryMu.Unlock()
	if reg == nil {
		return nil, imSendFileOut{}, errors.New("投递链路未启用")
	}
	ars, ok := reg.Lookup(token)
	if !ok {
		return nil, imSendFileOut{}, errors.New("投递会话不存在或已结束（仅在回合处理中可发送）")
	}
	name := filepath.Base(in.Path)
	key := fmt.Sprintf("sendfile-%d", time.Now().UnixNano())
	if err := ars.SendArtifact(key, name, in.Path); err != nil {
		return nil, imSendFileOut{}, fmt.Errorf("文件发送失败: %w", err)
	}
	out := imSendFileOut{OK: true}
	if strings.TrimSpace(in.Caption) != "" {
		if err := ars.SendNotice(in.Caption); err != nil {
			out.Message = "文件已发送，但附言发送失败（可忽略，或改在回复正文中说明）。"
		}
	}
	return &mcp.CallToolResult{}, out, nil
}
