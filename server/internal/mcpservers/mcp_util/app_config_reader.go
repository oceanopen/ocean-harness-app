package mcputil

import (
	"errors"
	"fmt"
	"strings"

	"ocean-harness/server/internal/appdb"
	"ocean-harness/server/internal/global"
)

// ReadGithubPAT 只读 GitHub Personal Access Token（设置 → 个人中心录入），供 github MCP
// 工具（T4.1）构造 API 客户端。未配置返回带指引的中文错误（引导用户去设置页录入）。
// 读取本体在 internal/appdb（Go 各域共享的 app.db 只读访问层；路径自 global.Config 注入，
// 本包不被 global 依赖、无环）。
func ReadGithubPAT() (string, error) {
	dbPath := ""
	if global.Config != nil {
		dbPath = global.Config.AppDbPath
	}
	pat, err := appdb.ReadAppConfigValue(dbPath, appdb.KeyGithubPAT)
	if err != nil {
		return "", fmt.Errorf("读取 GitHub PAT 失败: %w", err)
	}
	if strings.TrimSpace(pat) == "" {
		return "", errors.New("未配置 GitHub Personal Access Token（请在 设置 → 个人中心 → GitHub 录入）")
	}
	return pat, nil
}
