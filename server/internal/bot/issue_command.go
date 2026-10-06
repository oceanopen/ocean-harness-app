package bot

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// 本文件是 #任务 IM 指令（T2.3 显式绑定 → T2.2 统一卡片化）的解析纯函数集与解绑文案：
// parseIssueCommand 只认形态不碰 DB；候选匹配查询与出卡收口在 binding_card.go 域内——
// 统一「搜索 → 卡片 → 提交绑定」模型下 #任务 仅产出搜索关键词，绑定动作唯一入口是
// taskbind 卡点击（旧「唯一命中直绑 + Body 首回合一步」链路与 ID 前缀通道已退役）。指令名
// 对齐用户面词汇（任务），用户不再需要理解 issue 术语。解绑同模型卡片化（#任务解绑 出
// 确认卡，点击提交才落库）——无子指令形态。

// issueCommandToken 指令前缀：仅消息起始生效（前缀后必须紧跟空白或行尾），正文中部出现不解析。
const issueCommandToken = "#任务"

// issueUnbindCommandToken 解绑指令：从 #任务 常量派生（单点改名跟动）。恰等判定（同
// #帮助）——带后缀/粘连形态不命中；#任务解绑 对 #任务 是粘连形态（首字符非空白），
// 两指令互不抢消息。
const issueUnbindCommandToken = issueCommandToken + "解绑"

// issueCommand 解析产物（parseIssueCommand 返回 nil = 非指令，正文原样进回合管线）：
// Keyword 空 = 全列表卡，非空 = 名称模糊搜索（唯一命中同样出卡）。
type issueCommand struct {
	Keyword string
}

// parseIssueCommand 解析消息起始的 #任务 指令。固定规则：TrimSpace 后须以 token 起始，
// 且 token 后紧跟空白或行尾（粘连形态按普通文本放行——用法由文案教学，不猜测粘连意图；
// 空白按首 rune 解码，全角空格 U+3000 也是合法分隔）；rest 整段（规整截断后）为搜索
// 关键词，无子指令。
func parseIssueCommand(text string) *issueCommand {
	trimmed := strings.TrimSpace(text)
	rest, ok := strings.CutPrefix(trimmed, issueCommandToken)
	if !ok {
		return nil
	}
	if rest != "" {
		if r, _ := utf8.DecodeRuneInString(rest); !unicode.IsSpace(r) {
			return nil // 粘连形态：非指令（#任务解绑 在此形态由独立恰等判定接管）
		}
	}
	return &issueCommand{Keyword: normalizeBindingKeyword(rest)}
}

// parseIssueUnbindCommand 判定消息是否为解绑指令：TrimSpace 后恰等于 token（同 #帮助
// 惯例，无参数语义）。触发侧出确认卡，解绑动作唯一入口是 taskunbind 卡点击。
func parseIssueUnbindCommand(text string) bool {
	return strings.TrimSpace(text) == issueUnbindCommandToken
}

// issueNotBoundText 解绑触发侧未绑定教学（无卡可出，纯文本终帧收口）。
func issueNotBoundText() string {
	return "当前会话未绑定任务，无需解绑。"
}

// issueUnbindFailedText 解绑落库失败（fail visible，触发侧与点击侧共用）。
func issueUnbindFailedText() string {
	return "任务解绑失败，请稍后重试。"
}
