package bot

import (
	"errors"
	"strconv"
	"strings"

	"ocean-harness/server/internal/acpsession"
)

// pendingGate 挂起审批的 IM 数字快路径（T2.4 回合内文本交互形态）。审批挂起期用户在
// IM 回复纯数字即应答；应答消息不能入会话队列——每会话串行队列的 worker 正阻塞在
// pending 挂起的上一条消息的回合里，排队即死锁，必须在 HandleInbound 入队前拦截。
// 编排器以此为可选依赖（nil = 关闭，headless 单引擎装配与既有测试零改动）。
type pendingGate interface {
	// TryRespondPending 判定一条入站消息是否为审批数字应答。handled=true 时 text 即
	// 该消息的终帧文案（调用方直接 Flush(text, true)，不入队、不占并发槽）。
	TryRespondPending(cfg BotRuntimeConfig, msg InboundMessage) (text string, handled bool)
}

// TryRespondPending 审批快路径判定（HandleInbound 的入队前步骤）：纯数字消息且路由
// 命中 ACP 绑定会话时，按 Get 快照的全局顺序编号应答 permission 挂起。handled=false
// 落回主路径出回合——非数字、非 ACP 会话、路由读库失败（fail closed 交回合路径报错）
// 与「无任何挂起」（数字是普通正文）四种情形。
// 编号不变式：Get 快照序与 pendingOpened 帧到达序同源于视图注册序（单写者追加），
// collectTurn 状态行的展示编号与本快路径的应答编号恒一致（同一 permissionChoices 展开）。
func (r *driverRoute) TryRespondPending(cfg BotRuntimeConfig, msg InboundMessage) (string, bool) {
	n, ok := parseDigitReply(msg.Text)
	if !ok {
		return "", false
	}
	target, err := r.resolve(cfg.BotID, cfg.WorkspaceID, msg.ConversationKey)
	if err != nil || !target.ACP {
		return "", false
	}
	snap := r.sessions.Get(target.IssueID)
	if snap.Status != acpsession.StatusReady {
		// 会话未就绪/已终结：视图残留挂起不可应答（agent 进程死亡后视图不清挂起），回落
		// 主路径交 Ensure 重建语义受理，不吞数字消息。
		return "", false
	}
	perms, hasElicit := splitPendings(snap.Pendings)
	if len(perms) == 0 {
		if hasElicit {
			return elicitGateText(snap.Pendings), true // 只有表单挂起：数字非应答形态，按可表示性两态指引
		}
		return "", false
	}
	choices := permissionChoices(perms)
	if n < 1 || n > len(choices) {
		return "序号超出范围，可应答的审批：\n" + formatPermissionPendingStatus(choices), true
	}
	choice := choices[n-1]
	if err := r.sessions.RespondPermission(target.IssueID, choice.pendingID, choice.optionID, acpsession.PendingSourceBot); err != nil {
		if errors.Is(err, acpsession.ErrPendingAlreadyHandled) {
			return err.Error(), true // 后到方：文案自带「该审批已由桌面端/IM 应答（…）」判别
		}
		return "❌ 审批应答失败：" + firstLine(err.Error()), true
	}
	text := "✅ 已应答审批：" + choiceDisplay(choice)
	return text, true
}

// parseDigitReply 纯数字消息解析：TrimSpace 后非空且全为 ASCII 数字（全角数字不识别——
// IM 输入法数字恒为半角）。溢出按超范围处理（返回 0 = 恒不在合法序号内）。
func parseDigitReply(text string) (int, bool) {
	s := strings.TrimSpace(text)
	if s == "" {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, true
	}
	return n, true
}

// permissionChoice 扁平应答目录的单项：一个审批挂起的单个选项（跨挂起全局顺序编号，
// 用户回复 i+1 即应答第 i 项）。
type permissionChoice struct {
	pendingID uint64
	optionID  string
	label     string // acpsession.PermissionOptionLabel 产物（中文 kind 标签）
	tool      string // 触发审批的工具标题（呈现辅助，可空）
}

// permissionChoices 审批挂起列表 → 扁平应答目录（编号 SSOT：快路径应答与 IM 状态行
// 展示共用本展开）。
func permissionChoices(pendings []acpsession.PendingView) []permissionChoice {
	var choices []permissionChoice
	for i := range pendings {
		for _, opt := range pendings[i].Options {
			choices = append(choices, permissionChoice{
				pendingID: pendings[i].PendingID,
				optionID:  string(opt.OptionID),
				label:     acpsession.PermissionOptionLabel(string(opt.Kind), opt.Name),
				tool:      pendings[i].ToolTitle(),
			})
		}
	}
	return choices
}

// choiceDisplay 应答目录单项的呈现文本（「标签（工具）」）——应答确认文案与状态行编号
// 列表共用本格式，二者恒一致。
func choiceDisplay(c permissionChoice) string {
	if c.tool == "" {
		return c.label
	}
	return c.label + "（" + c.tool + "）"
}

// formatPermissionPendingStatus 审批挂起的 IM 呈现（回合内状态行与超范围用法提示共用）。
func formatPermissionPendingStatus(choices []permissionChoice) string {
	var b strings.Builder
	b.WriteString("⏳ Agent 请求审批，回复数字应答：")
	for i := range choices {
		b.WriteString("\n")
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(". ")
		b.WriteString(choiceDisplay(choices[i]))
	}
	return b.String()
}

// 表单挂起的 IM 指引文案（T3.3）：已出表单卡 → 提示卡上提交（collectTurn 状态行专用——
// 按挂起粒度判定，命中即卡在场，文案可确定）；gate 数字回吞可表示 → 不确定态（gate 无回合
// 上下文，「审批卡曾占位后残留纯表单挂起」在快照上与正常出卡态同形，无法判定卡是否在场，
// 文案两头兜底）；不可表示 → 指回桌面（两处共用）。
const (
	elicitPendingCardText    = "⏳ Agent 请求输入（表单）：请在上方卡片中选择并提交。"
	elicitPendingGateText    = "⏳ Agent 请求输入（表单）：如上方有对应的表单卡片，请在卡片中选择并提交；否则请在桌面端处理。"
	elicitPendingDesktopText = "⏳ Agent 请求输入（表单）：该表单 IM 无法呈现，请在桌面端处理。"
)

// elicitGateText 纯表单挂起期的数字回吞提示两态：任一挂起 schema 可表示 → 不确定态文案
// （可表示仅说明「若卡位空闲则已出卡」，本回合卡位是否被审批卡占用过、首表单是否已答，
// 快照上不可见——审批已答后的纯表单态与正常出卡态同形，确定态文案会在前者指引一张不存在
// 的卡）；全部不可表示 → 指回桌面。
func elicitGateText(pendings []acpsession.PendingView) string {
	for i := range pendings {
		if pendings[i].Kind != "elicitation" || pendings[i].Request == nil {
			continue
		}
		if _, ok := classifyElicitation(pendings[i].Request.RequestedSchema); ok {
			return elicitPendingGateText
		}
	}
	return elicitPendingDesktopText
}

// splitPendings 快照挂起按 kind 二分（保持注册序）：permission 列表 + 是否存在
// elicitation。
func splitPendings(pendings []acpsession.PendingView) (perms []acpsession.PendingView, hasElicitation bool) {
	for i := range pendings {
		if pendings[i].Kind == "permission" {
			perms = append(perms, pendings[i])
		} else {
			hasElicitation = true
		}
	}
	return perms, hasElicitation
}

// dropClosedPending 从回合内开放挂起目录摘除已关闭项（返回展示名词「审批/表单」；未见过
// 的关闭帧返回 ok=false，不打扰状态行）。目录维护与视图摘除同源同序——关闭后剩余挂起的
// 编号与快路径 Get 快照的编号保持一致。
func dropClosedPending(open *[]acpsession.PendingView, pendingID uint64) (string, bool) {
	for i := range *open {
		if (*open)[i].PendingID == pendingID {
			noun := "表单"
			if (*open)[i].Kind == "permission" {
				noun = "审批"
			}
			*open = append((*open)[:i], (*open)[i+1:]...)
			return noun, true
		}
	}
	return "", false
}
