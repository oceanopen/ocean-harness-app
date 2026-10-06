// 本文件是审批卡域（T3.2）：permission 挂起 → 单选审批卡的出站构造与入站点击消费。
// TaskID 无状态编码（issueID + 会话代锚 + pendingID 全量编进投递锚）：点击时经
// acpsession.Get 快照重建完整 spec 应答与置灰。会话代锚 = AcpSessionID 前 8 位——
// pendingID 是 per-AgentClient 发号（会话重建后从 1 重记，旧卡锚可能撞上同号新挂起），
// 代锚让旧代卡的点击对不上新代快照（不符即失效文案，不触达应答）；跨 sidecar 重启
// （AcpSessionID 必换新）与迟到点击（挂起已关闭，出快照）同样自然降级为文案告知，
// 无内存注册表可失配。
package bot

import (
	"errors"
	"strconv"
	"strings"

	"ocean-harness/server/internal/acpsession"
)

// TaskID 编码格式：acp-<issueID>@<会话代锚>-perm-<pendingID>（perm 段为 T3.3 elicitation
// 卡留位）。issueID 为 UUID（不含 '@'），Index 切分无歧义；'-perm-' 以 LastIndex 切分，
// issueID 含该子串时仍以最右段为 pendingID。
const (
	permissionCardPrefix     = "acp-"
	permissionCardSessionSep = "@"
	permissionCardMarker     = "-perm-"
)

// permissionCardSessionToken 会话代锚：AcpSessionID 前 8 位（UUID 首段 hex，字符集天然
// 合法；会话重建必换新 ID，代际判定以锚比对为准）。
func permissionCardSessionToken(acpSessionID string) string {
	r := []rune(acpSessionID)
	if len(r) > 8 {
		r = r[:8]
	}
	return string(r)
}

// permissionCardTaskID 编码审批卡投递锚。
func permissionCardTaskID(issueID, acpSessionID string, pendingID uint64) string {
	return permissionCardPrefix + issueID + permissionCardSessionSep +
		permissionCardSessionToken(acpSessionID) + permissionCardMarker + strconv.FormatUint(pendingID, 10)
}

// parsePermissionCardTaskID 解码本域投递锚（issueID / 会话代锚 / pendingID）；非本域形态
// （未知卡片/格式异常）返回 ok=false，调用方 miss 交拦截步兜底文案。
func parsePermissionCardTaskID(taskID string) (issueID, sessionToken string, pendingID uint64, ok bool) {
	if !strings.HasPrefix(taskID, permissionCardPrefix) {
		return "", "", 0, false
	}
	rest := taskID[len(permissionCardPrefix):]
	i := strings.LastIndex(rest, permissionCardMarker)
	if i <= 0 {
		return "", "", 0, false
	}
	left := rest[:i]
	pid, err := strconv.ParseUint(rest[i+len(permissionCardMarker):], 10, 64)
	if err != nil || pid == 0 {
		return "", "", 0, false
	}
	at := strings.Index(left, permissionCardSessionSep)
	if at <= 0 || at == len(left)-1 {
		return "", "", 0, false
	}
	return left[:at], left[at+1:], pid, true
}

// permissionCardSpec 审批挂起 → 卡 spec（首发与置灰重建的 SSOT：企微 UpdateTemplateCard
// 整卡替换，两处共用同一构造函数保证原卡与置灰卡字段一致，见 CardReplyStream.UpdateCard
// 契约）。选项呈现全集不过滤（T2.4 拍板②：bot 与桌面等价客户端，含 allow_always），标签走
// PermissionOptionLabel 中文 SSOT（与数字快路径状态行同源）。
func permissionCardSpec(issueID, acpSessionID string, pending acpsession.PendingView) CardSpec {
	desc := pending.ToolTitle()
	if desc == "" {
		desc = "Agent 请求授权执行操作"
	}
	options := make([]CardOption, 0, len(pending.Options))
	for _, opt := range pending.Options {
		options = append(options, CardOption{
			ID:   string(opt.OptionID),
			Text: acpsession.PermissionOptionLabel(string(opt.Kind), opt.Name),
		})
	}
	return CardSpec{
		Title:       "Agent 请求审批",
		Description: desc,
		Options:     options,
		TaskID:      permissionCardTaskID(issueID, acpSessionID, pending.PendingID),
	}
}

// TryHandleInteraction 卡片点击消费（interactionHandler 的 driverRoute 第三角色实现，
// pendingGate 同款装配形态）：纯决策不做流 I/O——置灰与终帧由编排器拦截步统一编排。
// 按 TaskID 分流：-perm- 审批卡（点击即答）/ -elicit- 表单卡（提交应答，消费逻辑在
// elicitation_card.go 同域文件）/ wsbind- 与 taskbind- 绑定卡（重查列表指纹比对后落库，
// 消费逻辑在 binding_card.go 同域文件）。perm/elicit 两域决策链共形：非本域锚 miss →
// 渠道回传锚对照不一致 miss → 域内 key 形态分流 → 快照未就绪（跨 sidecar 重启窗口）出
// 失效文案不置灰 → 命中应答（成功与后到方判别均置灰；其他失败不置灰可重试）→ 挂起已出
// 快照（桌面先答/回合结算）经探测应答取 closed-history 判别文案（不置灰——已关闭挂起
// 重建不出完整卡）。绑定卡两支的决策链见 binding_card.go 域内注释。
func (r *driverRoute) TryHandleInteraction(cfg BotRuntimeConfig, msg InboundMessage) (string, *CardSpec, bool) {
	in := msg.Interaction
	if in == nil {
		return "", nil, false
	}
	// 对照锚：渠道回传 task_id 须与本域投递锚一致（防串卡）；空值容忍（渠道未回传字段）。
	if in.TaskID != "" && in.TaskID != in.DeliveryID {
		return "", nil, false
	}
	if issueID, sessionToken, pendingID, ok := parsePermissionCardTaskID(in.DeliveryID); ok {
		return r.handlePermissionInteraction(issueID, sessionToken, pendingID, in)
	}
	if issueID, sessionToken, pendingID, ok := parseElicitCardTaskID(in.DeliveryID); ok {
		return r.handleElicitationInteraction(issueID, sessionToken, pendingID, in)
	}
	if fingerprint, ok := parseWsbindTaskID(in.DeliveryID); ok {
		return r.handleWsbindInteraction(cfg, fingerprint, in)
	}
	if fingerprint, ok := parseTaskbindTaskID(in.DeliveryID); ok {
		return r.handleTaskbindInteraction(cfg, msg, fingerprint, in)
	}
	return "", nil, false
}

// handlePermissionInteraction 审批卡点击消费（-perm- 域，T3.2 原逻辑收敛为域内函数）：
// 非选项 key miss（本卡单选不可达）→ 快照/代锚失效文案 → 命中以选项应答（成功与后到方
// 判别均置灰；其他失败不置灰可重点重试）→ 已关闭经空 optionID 应答取 closed-history
// 判别文案（空选项过不了预检，错误即收敛文案）。
func (r *driverRoute) handlePermissionInteraction(issueID, sessionToken string, pendingID uint64, in *Interaction) (string, *CardSpec, bool) {
	if in.ActionIndex < 0 {
		return "", nil, false // 非选项 key（如多选提交按钮）：本卡单选不可达，防御 miss
	}
	snap := r.sessions.Get(issueID)
	if snap.Status != acpsession.StatusReady {
		return "该审批卡已失效（ACP 会话已重启或未就绪），请重新发起对话后再试。", nil, true
	}
	// 会话代对照：pendingID 是 per-AgentClient 发号（重建后从 1 重记），仅凭 id 命中可能是
	// 旧代卡撞上同号新挂起——代锚不符即旧代卡，失效文案收口、不触达应答（防误答新请求）。
	if snap.AcpSessionID == "" || permissionCardSessionToken(snap.AcpSessionID) != sessionToken {
		return "该审批卡已失效（ACP 会话已重启或未就绪），请重新发起对话后再试。", nil, true
	}
	pending, found := findPermissionPending(snap.Pendings, pendingID)
	if !found {
		// 已关闭（桌面先答/回合结算）：空 optionID 过不了选项预检，RespondPermission 的错误
		// 即收敛判别文案（closed-history：「该审批已由桌面端/IM 应答（…）」/「该审批已随
		// 回合结束自动结算」；未命中历史则「不存在或已关闭」），直出不置灰。
		err := r.sessions.RespondPermission(issueID, pendingID, "", acpsession.PendingSourceBot)
		if err == nil {
			return "该审批已处理。", nil, true // 空选项应答不应成功（预检拒绝），防御收口
		}
		return firstLine(err.Error()), nil, true
	}
	if in.ActionIndex >= len(pending.Options) {
		return "点击的选项无效，请重新点击卡片中的选项。", nil, true
	}
	opt := pending.Options[in.ActionIndex]
	choice := permissionChoice{
		pendingID: pendingID,
		optionID:  string(opt.OptionID),
		label:     acpsession.PermissionOptionLabel(string(opt.Kind), opt.Name),
		tool:      pending.ToolTitle(),
	}
	gray := permissionCardSpec(issueID, snap.AcpSessionID, pending)
	gray.Disabled = true
	err := r.sessions.RespondPermission(issueID, pendingID, choice.optionID, acpsession.PendingSourceBot)
	switch {
	case err == nil:
		return "✅ 已应答审批：" + choiceDisplay(choice), &gray, true
	case errors.Is(err, acpsession.ErrPendingAlreadyHandled):
		// 后到方：文案自带「谁、以何选项处理」判别；快照在手可置灰（挂起仍开放时另一端
		// 刚应答完的窄竞态窗口）。
		return firstLine(err.Error()), &gray, true
	default:
		return "❌ 审批应答失败：" + firstLine(err.Error()), nil, true
	}
}

// findPermissionPending 挂起目录中定位指定 id 的 permission 挂起（pendingID 跨 kind 全局
// 唯一，kind 收窄是防御——本域 TaskID 只为 permission 挂起生成）。
func findPermissionPending(pendings []acpsession.PendingView, pendingID uint64) (acpsession.PendingView, bool) {
	for i := range pendings {
		if pendings[i].PendingID == pendingID && pendings[i].Kind == "permission" {
			return pendings[i], true
		}
	}
	return acpsession.PendingView{}, false
}
