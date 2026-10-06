package bot

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// 本文件是绑定指令解析域（T2.2 统一「搜索 → 卡片 → 提交绑定」模型）：#工作空间 与 #帮助
// 指令的形态判定、两类绑定指令共用的关键词规整。指令只产出搜索关键词——绑定动作唯一
// 入口是卡点击（binding_card.go）；形态判定与 #任务 同款惯例：只认形态不碰 DB，正文中部
// 出现不解析。指令名对齐用户面词汇（任务/工作空间），帮助文案从 token 常量派生。

// workspaceCommandToken 指令前缀：仅消息起始生效（前缀后必须紧跟空白或行尾）。
const workspaceCommandToken = "#工作空间"

// helpCommandToken 帮助指令前缀：TrimSpace 后恰等于本值才解析（无参数语义，粘连形态按
// 普通文本放行）。
const helpCommandToken = "#帮助"

// bindingKeywordMaxBytes 搜索关键词字节上限（utf-8）：TaskID 携带关键词的 hex 编码，40 字节
// （≈13 个汉字）hex 后 80 字符，加前缀与指纹共 ~98 字节，留足 128 字节投递锚上限余量。
// 超限截断只放宽模糊命中面，无损语义。
const bindingKeywordMaxBytes = 40

// workspaceCommand 解析产物（parseWorkspaceCommand 返回 nil = 非指令）：Keyword 空 =
// 全列表卡，非空 = 名称模糊搜索。
type workspaceCommand struct {
	Keyword string
}

// parseWorkspaceCommand 解析消息起始的 #工作空间 指令。TrimSpace 后须以 token 起始，且
// token 后紧跟空白或行尾（粘连形态按普通文本放行）；rest 整段为搜索关键词（无子指令
// ——workspace 数量少，卡片即完整选择面）。空白判定按首 rune 解码：多字节首字节经
// rune(rest[0]) 折算成 Latin-1 会误判（全角空格 U+3000 曾因此漏判为粘连形态）。
func parseWorkspaceCommand(text string) *workspaceCommand {
	trimmed := strings.TrimSpace(text)
	rest, ok := strings.CutPrefix(trimmed, workspaceCommandToken)
	if !ok {
		return nil
	}
	if rest != "" {
		if r, _ := utf8.DecodeRuneInString(rest); !unicode.IsSpace(r) {
			return nil // 粘连形态：非指令
		}
	}
	return &workspaceCommand{Keyword: normalizeBindingKeyword(rest)}
}

// parseHelpCommand 判定消息是否为帮助指令：TrimSpace 后恰等于 token。带后缀形态
// （「#帮助一下」）不解析——帮助无参数语义，且 #帮助 token 本身不构成其它消息的合理前缀，
// 无粘连歧义可撞。
func parseHelpCommand(text string) bool {
	return strings.TrimSpace(text) == helpCommandToken
}

// helpText 指令列表文案（#帮助 的终帧回复）。行式纯文本列表（企微回复流无 markdown，
// 与卡同帧文本同款 `\n- ` 形态）；条目从 token 常量派生，指令改名只动常量此处自动跟。
func helpText() string {
	return "可用指令：\n" +
		"- " + workspaceCommandToken + " <关键词>：搜索并选择机器人归属的工作空间（无关键词看全部）\n" +
		"- " + issueCommandToken + " <关键词>：搜索并选择本会话绑定的任务（无关键词看全部）\n" +
		"- " + issueUnbindCommandToken + "：解除本会话绑定的任务（点击卡片确认）\n" +
		"- " + helpCommandToken + "：查看本指令列表"
}

// normalizeBindingKeyword 关键词规整：TrimSpace + 字节截断（回退到 rune 边界，不切半个字符）。
func normalizeBindingKeyword(keyword string) string {
	keyword = strings.TrimSpace(keyword)
	if len(keyword) <= bindingKeywordMaxBytes {
		return keyword
	}
	cut := bindingKeywordMaxBytes
	for cut > 0 && !utf8.RuneStart(keyword[cut]) {
		cut--
	}
	return keyword[:cut]
}
