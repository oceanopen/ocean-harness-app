package browser

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

// tabsLineRe 引擎 browser_tabs（action=list）输出行格式（playwright-mcp 官方文档逐字
// 核实口径）：
//
//   - 0: [](about:blank)
//   - 1: (current) [React • TodoMVC](https://demo.playwright.dev/todomvc)
//
// 行结构：缩进 + "- <index>:" + 可选 "(current) " + Markdown 链接 [title](url)；无标题
// 页面 title 为空。崩溃 tab 的 "[crashed]" 标记出现在链接前，正则以可选组容忍。
// current 以捕获组判定（非子串匹配——防 title 含 "(current)" 误判）。
var tabsLineRe = regexp.MustCompile(`(?m)^\s*-\s*(\d+):\s*(?:\((current)\)\s*)?(?:\[crashed\]\s*)?\[([^\]]*)\]\(([^)]*)\)`)

// parseTabsOutput 解析 tab 列表文本 → []PageView；一行未匹配则返回 nil（格式漂移时
// 保持原投影，不误清空）。
func parseTabsOutput(text string) []PageView {
	matches := tabsLineRe.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	pages := make([]PageView, 0, len(matches))
	for _, m := range matches {
		index, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		pages = append(pages, PageView{
			Index:   index,
			Title:   m[3],
			URL:     m[4],
			Current: m[2] == "current",
		})
	}
	return pages
}

// resultText 提取 CallToolResult 的 TextContent 拼接文本（引擎结果以文本为主载体）。
func resultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// refreshPages Forward 成功后的页面投影刷新：拉 tab 列表 → 解析 → view 行更新（锁外
// 广播 sessions 帧）。一次调用一次刷新，无防抖；截图轮询路径经 pagesRefreshExempt 豁免
// （一次轮询不触发两次引擎调用）。best-effort——刷新失败仅记日志，不影响转发结果。
func (m *Manager) refreshPages(handle *EngineHandle, profile string) {
	ctx, cancel := context.WithTimeout(context.Background(), pagesRefreshTimeout)
	defer cancel()
	res, err := handle.Session().CallTool(ctx, &mcp.CallToolParams{
		Name:      EngineToolTabs,
		Arguments: map[string]any{"action": "list"},
	})
	if err != nil {
		m.log.Debug("页面投影刷新失败", zap.String("profile", profile), zap.Error(err))
		return
	}
	if res.IsError {
		m.log.Debug("页面投影刷新被引擎拒绝", zap.String("profile", profile), zap.String("result", resultText(res)))
		return
	}
	pages := parseTabsOutput(resultText(res))
	if pages == nil {
		return
	}
	sv := m.view.apply(profile, func(row *SessionView) { row.Pages = pages })
	m.hub.broadcast([]Frame{{Type: FrameSessions, Sessions: []SessionView{sv}}})
}
