package browser

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

/*
 * tabsLineRe 引擎 browser_tabs（action=list）输出行格式（playwright-mcp 官方文档逐字核实口径）：形如 `0: [](about:blank)`、`1: (current) [React • TodoMVC](https://demo.playwright.dev/todomvc)`。
 * 行结构：缩进 + "- <index>:" + 可选 "(current) " + Markdown 链接 [title](url)；无标题页面 title 为空。崩溃 tab 的 "[crashed]" 标记出现在链接前，正则以可选组容忍。
 * current 以捕获组判定（非子串匹配——防 title 含 "(current)" 误判）。
 */
var tabsLineRe = regexp.MustCompile(`(?m)^\s*-\s*(\d+):\s*(?:\((current)\)\s*)?(?:\[crashed\]\s*)?\[([^\]]*)\]\(([^)]*)\)`)

/*
 * ParseTabsOutput 解析 tab 列表文本 → []PageView；一行未匹配则返回 nil（格式漂移时保持
 * 原投影，不误清空）。消费面仅剩两处：Close 收尾捕获与 analyze 的 targetId → 引擎 index
 * 定位（Pages 投影 SSOT 归页面观察者）。
 */
func ParseTabsOutput(text string) []PageView {
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

/*
 * ResultText 提取 CallToolResult 的 TextContent 拼接文本（引擎结果以文本为主载体；
 * REST/工具面等跨包消费的导出口，T2.1 起供 service 层组装分析 prompt 用）。
 */
func ResultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

/*
 * capturePagesCtx Close 收尾捕获：拉引擎 tab 列表并解析返回（捕获行无 TargetID——引擎
 * 文本解析无此信息），供会话重建恢复现场；best-effort——失败返回 nil，不拖累收尾。
 */
func (m *Manager) capturePagesCtx(ctx context.Context, handle *EngineHandle, profile string) []PageView {
	res, err := handle.Session().CallTool(ctx, &mcp.CallToolParams{
		Name:      EngineToolTabs,
		Arguments: map[string]any{"action": "list"},
	})
	if err != nil {
		m.log.Debug("页面捕获失败", zap.String("profile", profile), zap.Error(err))
		return nil
	}
	if res.IsError {
		m.log.Debug("页面捕获被引擎拒绝", zap.String("profile", profile), zap.String("result", ResultText(res)))
		return nil
	}
	return ParseTabsOutput(ResultText(res))
}

/*
 * publishPages 页面投影重算并广播（SSOT 归 pageHistoryStore）；store 无该 profile 记录
 * 时不动投影（避免误清空）。
 */
func (m *Manager) publishPages(profile string) {
	rows := m.pageHist.rows(profile)
	if rows == nil {
		return
	}
	sv := m.view.apply(profile, func(row *SessionView) { row.Pages = rows })
	m.hub.broadcast([]Frame{{Type: FrameSessions, Sessions: []SessionView{sv}}})
}
