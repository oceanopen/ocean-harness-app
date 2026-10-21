package bot

import "strings"

/*
 * 本文件是 IM 指令解析域：slash 系（/help /stop /clear /继续）与 #帮助 的形态判定与帮助
 * 文案。指令只读形态不碰 DB，正文中部出现不解析；帮助文案从 token 常量派生，指令改名只动
 * 常量此处自动跟。
 */

/*
 * slash 系指令（/help /stop /clear）：形态同 #帮助——TrimSpace 后恰等于 token 才解析，
 * 粘连形态按普通文本放行。# 系为既有兼容面，/ 系对齐 IM 指令惯例。
 */
const (
	slashHelpToken  = "/help"
	slashStopToken  = "/stop"
	slashClearToken = "/clear"
)

/*
 * parseSlashCommand 已知 slash 指令判定（返回命中 token，空串 = 非指令）。
 * /help 与 #帮助 同义（帮助无状态变更，走 runTurn 指令步）；其余两条由编排器拦截步消费。
 */
func parseSlashCommand(text string) string {
	switch strings.TrimSpace(text) {
	case slashHelpToken:
		return slashHelpToken
	case slashStopToken:
		return slashStopToken
	case slashClearToken:
		return slashClearToken
	}
	return ""
}

/*
 * parseUnknownSlashCommand 未识别的 slash 指令形态（顶格 + 纯 ASCII 字母 token，如 /helpp）。
 * 参数形态（含空白）与中文/混合 token 不命中——避免误伤含 “/” 的英文正文与路径。
 */
func parseUnknownSlashCommand(text string) bool {
	t := strings.TrimSpace(text)
	if len(t) < 2 || t[0] != '/' {
		return false
	}
	for i := 1; i < len(t); i++ {
		c := t[i]
		if c < 'a' || c > 'z' {
			if c < 'A' || c > 'Z' {
				return false
			}
		}
	}
	return parseSlashCommand(t) == ""
}

/*
 * helpCommandToken 帮助指令前缀：TrimSpace 后恰等于本值才解析（无参数语义，粘连形态按
 * 普通文本放行）。
 */
const helpCommandToken = "#帮助"

/*
 * parseHelpCommand 判定消息是否为帮助指令（#帮助 与 /help 同义）：TrimSpace 后恰等于
 * token。带后缀形态（「#帮助一下」）不解析——帮助无参数语义，且 token 本身不构成其它
 * 消息的合理前缀，无粘连歧义可撞。
 */
func parseHelpCommand(text string) bool {
	t := strings.TrimSpace(text)
	return t == helpCommandToken || t == slashHelpToken
}

/*
 * helpText 指令列表文案（#帮助 /help 的终帧回复）。行式纯文本列表（企微回复流无 markdown，
 * 与卡同帧文本同款 `\n- ` 形态）；条目从 token 常量派生，指令改名只动常量此处自动跟。
 */
func helpText() string {
	return "可用指令：\n" +
		"- " + slashStopToken + "：停止当前正在处理的回复（会话上下文保留）\n" +
		"- " + slashClearToken + "：停止当前回复并清空上下文（后续对话开启新会话）\n" +
		"- " + helpCommandToken + " " + slashHelpToken + "：查看本指令列表"
}
