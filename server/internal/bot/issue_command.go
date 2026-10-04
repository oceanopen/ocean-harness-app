package bot

import (
	"context"
	"strconv"
	"strings"
	"unicode"

	"gorm.io/gorm"

	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
)

// 本文件是 #issue IM 指令（T2.3 bot↔issue 显式绑定）的解析与匹配纯函数集：parseIssueCommand
// 只认形态不碰 DB；resolveIssueTarget 只查库不做语义决策；文案函数只做组装。三段各自可测，
// 编排侧（orchestrator.applyIssueCommand）按结果分支。

// issueCommandToken 指令前缀：仅消息起始生效（前缀后必须紧跟空白或行尾），正文中部出现不解析。
// 绑定目标以首个空白分隔字段为准（整段当关键词会把任务正文一并吃进匹配域）。
const issueCommandToken = "#issue"

// issueUnbindToken 解绑指令：rest 恰等于此值才解绑（`#issue 解绑 xxx` 归关键词路径，
// 收窄与标题撞词的歧义）。
const issueUnbindToken = "解绑"

// issueCandidateLimit 多命中候选列表截断条数（IM 文本列表保持克制，超出以总数注明）。
const issueCandidateLimit = 5

// issueIDPrefixMinLen uuid 短前缀通道的最短长度（去连字符后；8 位 hex 已具区分度）。
const issueIDPrefixMinLen = 8

// issueCommand 解析产物（parseIssueCommand 返回 nil = 非指令，正文原样进回合管线）：
// Target 空 = 用法帮助；Unbind = 解绑；Target 非空 = 绑定/切换（Body 空 = 纯切换，
// 非空 = 绑定 + 首回合一步，Body 即首回合正文）。
type issueCommand struct {
	Unbind bool
	Target string
	Body   string
}

// parseIssueCommand 解析消息起始的 #issue 指令。固定规则：TrimSpace 后须以 token 起始，
// 且 token 后紧跟空白或行尾（"#issues"、"#issue-42" 等粘连形态按普通文本放行——用法由
// 文案教学，不猜测粘连意图）；rest 恰等于解绑词则解绑；否则首个空白切分 target 与 body。
func parseIssueCommand(text string) *issueCommand {
	trimmed := strings.TrimSpace(text)
	rest, ok := strings.CutPrefix(trimmed, issueCommandToken)
	if !ok {
		return nil
	}
	if rest != "" && !unicode.IsSpace(rune(rest[0])) {
		return nil // 粘连形态（#issues / #issue-42）：非指令
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return &issueCommand{} // 裸指令：用法帮助
	}
	if rest == issueUnbindToken {
		return &issueCommand{Unbind: true}
	}
	body := ""
	if i := strings.IndexFunc(rest, unicode.IsSpace); i >= 0 {
		body = strings.TrimSpace(rest[i:])
		rest = rest[:i]
	}
	return &issueCommand{Target: rest, Body: body}
}

// resolveIssueTarget 在 workspace 域内匹配 issue：关键词呈 uuid 短前缀形态（去连字符后
// 8+ 位 hex）先走 id 前缀通道（恒精确、无撞词；前缀形态见 issueIDLikePrefix），零命中
// 回落标题子串通道（与 service 层 getList keyword 同款 LIKE 语义；%/_ 不转义——误放大
// 退化为多候选文案，无害）。不按 state 过滤（恒可绑定，与模式无关）。
func resolveIssueTarget(db *gorm.DB, workspaceID int, keyword string) ([]*model.ProjectIssue, error) {
	q := query.Use(db)
	if isIssueIDPrefix(keyword) {
		hits, err := q.ProjectIssue.WithContext(context.Background()).Where(
			q.ProjectIssue.WorkspaceID.Eq(workspaceID),
			q.ProjectIssue.ID.Like(issueIDLikePrefix(keyword)+"%"),
		).Order(q.ProjectIssue.SortOrder).Find()
		if err != nil || len(hits) > 0 {
			return hits, err
		}
	}
	return q.ProjectIssue.WithContext(context.Background()).Where(
		q.ProjectIssue.WorkspaceID.Eq(workspaceID),
		q.ProjectIssue.Name.Like("%"+keyword+"%"),
	).Order(q.ProjectIssue.SortOrder).Find()
}

// issueIDLikePrefix ID 通道的 LIKE 前缀：uuid 第 9 个字符恒为连字符，故「无连字符且
// 超过 8 位」的关键词（12/32 位全 hex 等）截前 8 位再前缀匹配——原样拼接会因第 9 位
// 字符错位而恒零命中（假死路）。截断后命中集是精确命中的超集，超集交给多候选文案，
// 语义无害；带连字符形态保持原样（自身已含连字符对位）。
func issueIDLikePrefix(keyword string) string {
	if !strings.Contains(keyword, "-") && len(keyword) > issueIDPrefixMinLen {
		return keyword[:issueIDPrefixMinLen]
	}
	return keyword
}

// isIssueIDPrefix 关键词是否为 uuid 短前缀形态：去掉连字符后长度达标且全为 hex。
func isIssueIDPrefix(keyword string) bool {
	compact := strings.ReplaceAll(keyword, "-", "")
	if len(compact) < issueIDPrefixMinLen {
		return false
	}
	for _, r := range compact {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

// issueShortID issue id 前 8 位（确认/候选文案展示用；不足 8 位原样）。
func issueShortID(issueID string) string {
	return issueID[:min(len(issueID), 8)]
}

// issueUsageText 用法帮助（裸 #issue 或将来可能的误用兜底）。
func issueUsageText() string {
	return "用法：#issue <标题关键词 或 ID前8位> [任务正文] —— 唯一命中即绑定并开跑；" +
		"#issue 解绑 解除绑定。"
}

// issueBoundText 绑定成功确认（issueID 短前缀一并展示，供后续 ID 通道引用）。
func issueBoundText(issue *model.ProjectIssue) string {
	return "✅ 已绑定 issue「" + issue.Name + "」(" + issueShortID(issue.ID) +
		")，直接发消息即可向该 issue 下达任务。"
}

// issueUnboundText 解绑成功确认。
func issueUnboundText() string {
	return "✅ 已解除 issue 绑定。"
}

// issueZeroHitText 零命中（keyword 转义语义见 resolveIssueTarget 注释）。
func issueZeroHitText(keyword string) string {
	return "未在工作空间内找到匹配「" + keyword + "」的 issue。可改用标题关键词或 ID 前 8 位重试。"
}

// issueCandidatesText 多命中候选列表（截前 issueCandidateLimit 条，超出以总数注明）。
func issueCandidatesText(keyword string, hits []*model.ProjectIssue) string {
	var b strings.Builder
	b.WriteString("匹配到多个 issue，请用更完整关键词或 ID 前 8 位指定：")
	for i, issue := range hits {
		if i >= issueCandidateLimit {
			break
		}
		b.WriteString("\n- 「" + issue.Name + "」(" + issueShortID(issue.ID) + ")")
	}
	if rest := len(hits) - issueCandidateLimit; rest > 0 {
		b.WriteString("\n…等共 " + strconv.Itoa(len(hits)) + " 个匹配")
	}
	return b.String()
}

// issueLookupFailedText 匹配查询失败（读库错误，fail visible 不静默）。
func issueLookupFailedText() string {
	return "issue 匹配查询失败，请稍后重试。"
}

// issueUnbindFailedText 解绑落库失败（fail visible，与绑定失败文案区分故障面）。
func issueUnbindFailedText() string {
	return "issue 解绑失败，请稍后重试。"
}

// issueBindFailedText 绑定落库失败（fail visible）。
func issueBindFailedText() string {
	return "issue 绑定失败，请稍后重试。"
}
