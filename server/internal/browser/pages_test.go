package browser

import "testing"

// tabsSample 引擎 browser_tabs（action=list）输出样例（playwright-mcp 官方文档逐字
// 口径，含无标题页/current 标记/crashed 标记三种形态）。
const tabsSample = `  - 0: [](about:blank)
  - 1: (current) [React • TodoMVC](https://demo.playwright.dev/todomvc)
  - 2: [crashed] [Example Domain](https://example.com)
`

func TestParseTabsOutput(t *testing.T) {
	pages := parseTabsOutput(tabsSample)
	if len(pages) != 3 {
		t.Fatalf("解析出 %d 页, want 3: %+v", len(pages), pages)
	}
	want := []PageView{
		{Index: 0, Title: "", URL: "about:blank"},
		{Index: 1, Title: "React • TodoMVC", URL: "https://demo.playwright.dev/todomvc", Current: true},
		{Index: 2, Title: "Example Domain", URL: "https://example.com"},
	}
	// Current 标记只应落在 index 1。
	for i, p := range pages {
		w := want[i]
		if p.Index != w.Index || p.Title != w.Title || p.URL != w.URL || p.Current != w.Current {
			t.Fatalf("pages[%d] = %+v, want %+v", i, p, w)
		}
	}
}

func TestParseTabsOutputNoMatchKeepsNil(t *testing.T) {
	if pages := parseTabsOutput("no tabs here"); pages != nil {
		t.Fatalf("无匹配行应返回 nil（保持原投影）, got %+v", pages)
	}
	if pages := parseTabsOutput(""); pages != nil {
		t.Fatalf("空文本应返回 nil, got %+v", pages)
	}
}
