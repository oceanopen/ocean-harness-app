// 本文件是绑定卡域（T2.1）：workspace / 任务两类绑定卡的候选查询、出站构造与入站点击
// 消费。TaskID 无状态自包含——<前缀><候选列表指纹 8 位 hex>，不编码 pendingID 或会话
// 代锚（候选列表即全部状态）：点击时重查列表算指纹比对，一致才按 ActionIndex 下标解析
// 目标落库——列表增删或换位均指纹不符，回「重新获取卡片」防索引错绑；跨 sidecar 重启与
// 迟到点击同样自然降级为文案告知，无内存注册表可失配（与 perm/elicit 卡同哲学）。
package bot

import (
	"context"
	"database/sql/driver"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"gorm.io/gen/field"
	"gorm.io/gorm"

	"ocean-harness/server/internal/dal/enums"
	"ocean-harness/server/internal/dal/model"
	"ocean-harness/server/internal/dal/query"
)

// TaskID 前缀族（与 acp- 前缀族互斥，TryHandleInteraction 按前缀分流）。
const (
	wsbindCardPrefix   = "wsbind-"
	taskbindCardPrefix = "taskbind-"
)

// bindingFingerprintLen 指纹 hex 字符数；解析按此精确校验（域内形态互斥）。
const bindingFingerprintLen = 8

// bindingOptionLimit 两类绑定卡的候选/选项上限（企微 vote 选项上限 20，风险 §6.5——
// workspace 卡与任务卡同受该约束）；超出截断，完整标题列表由触发链（T2.2）在同帧
// content 附带。
const bindingOptionLimit = 20

// issueTerminalStateCodes 绑定候选的终态集（bot 域 SSOT，与前端 isDevIssue 同判定语义
// ——事实 9）：归档 = state_code 流转 DONE / CANCELLED，终态任务不再进任务卡候选；子任务
// 经 #issue 关键词恒可绑定的通道不受此口径影响。元素类型取 driver.Valuer（StateCode 字段
// 表达式 NotIn 的形参面，StateCode 枚举原生实现该接口）。
var issueTerminalStateCodes = []driver.Valuer{enums.STATE_CODE_DONE, enums.STATE_CODE_CANCELLED}

// 绑定卡点击的通用文案（读库/落库失败文案在各 handler 内联，故障面各自区分）。
const (
	bindingListChangedText   = "列表已变化，请重新发送指令获取新卡片。"
	bindingInvalidOptionText = "点击的选项无效，请重新点击卡片中的选项。"
)

// botApplier wsbind 落库后的 bot 热重载消费面（消费侧缝，惯例同 acpSessions）：supervisor
// 实现——goroutine + 延迟 ApplyBot（终帧先发；同步 Stop→Start 持锁拆渠道 WS 连接，会吃掉
// UpdateCard/Flush 的 5 秒窗口，见 supervisor.ApplyBotAsync）。nil = 跳过热重载仅落库
// （测试装配；落库行 DB 侧已生效，运行时 cfg 须经重启连接收敛）。
type botApplier interface {
	ApplyBotAsync(botID int)
}

// bindingFingerprint 候选列表指纹：有序 id 串（逗号连接）的 fnv64a 摘要前 8 位 hex——
// 内容与次序双因子，任一 id 增删或换位即不符。候选排序由查询侧确定序保证（SortOrder +
// ID 次序键 / workspace ID 升序），同列表恒同指纹。
func bindingFingerprint(ids []string) string {
	h := fnv.New64a()
	h.Write([]byte(strings.Join(ids, ",")))
	return fmt.Sprintf("%016x", h.Sum64())[:bindingFingerprintLen]
}

// parseWsbindTaskID / parseTaskbindTaskID 解码本域投递锚（尾段指纹）；非本域形态（前缀
// 不符 / 尾段非恰 8 位小写 hex）ok=false，调用方 miss 交下一分支或拦截步兜底。
func parseWsbindTaskID(taskID string) (fingerprint string, ok bool) {
	rest, hit := strings.CutPrefix(taskID, wsbindCardPrefix)
	if !hit || !isBindingFingerprint(rest) {
		return "", false
	}
	return rest, true
}

func parseTaskbindTaskID(taskID string) (fingerprint string, ok bool) {
	rest, hit := strings.CutPrefix(taskID, taskbindCardPrefix)
	if !hit || !isBindingFingerprint(rest) {
		return "", false
	}
	return rest, true
}

// isBindingFingerprint 恰 8 位小写 hex（生成侧 %x 恒小写，解析同口径收紧——大小写混排
// 视为非本域形态）。
func isBindingFingerprint(s string) bool {
	if len(s) != bindingFingerprintLen {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

// bindingWorkspaces workspace 卡候选：全部工作空间 ID 升序取前 bindingOptionLimit
// （确定序——指纹与选项下标都依赖确定序；上限对齐任务卡的企微 vote 选项约束，防止
// 超限卡被适配器拒收）。
func bindingWorkspaces(db *gorm.DB) ([]*model.Workspace, error) {
	q := query.Use(db)
	return q.Workspace.WithContext(context.Background()).Order(q.Workspace.ID).Limit(bindingOptionLimit).Find()
}

// bindingIssueCandidates 任务卡候选（候选口径拍板）：bot workspace 域内非终态
// （state_code ∉ 终态集）且顶级（parent_id 空——对齐前端 isDevIssue 左树口径，写入方
// 空串与 NULL 两形态都算顶级）的 issue，SortOrder 排序 + ID 次序键（防并列抖动致指纹
// 假性不符）取前 bindingOptionLimit。
func bindingIssueCandidates(db *gorm.DB, workspaceID int) ([]*model.ProjectIssue, error) {
	q := query.Use(db)
	return q.ProjectIssue.WithContext(context.Background()).Where(
		q.ProjectIssue.WorkspaceID.Eq(workspaceID),
		q.ProjectIssue.StateCode.NotIn(issueTerminalStateCodes...),
		field.Or(q.ProjectIssue.ParentID.IsNull(), q.ProjectIssue.ParentID.Eq("")),
	).Order(q.ProjectIssue.SortOrder, q.ProjectIssue.ID).Limit(bindingOptionLimit).Find()
}

// workspaceCardSpec workspace 绑定卡构造（首发与置灰重建的共用 SSOT，对齐 permissionCardSpec
// 惯例——指纹一致保证两态字段一致，企微整卡替换不丢字段）。单选 vote 形态（Multiple /
// Submit 缺省 false，点击即答走 ActionIndex）；空列表 ok=false（无可选项不出卡，触发侧
// 回纯文本）。选项文案企微截 11 字，完整名称由触发链在同帧 content 附带。
func workspaceCardSpec(workspaces []*model.Workspace) (CardSpec, bool) {
	if len(workspaces) == 0 {
		return CardSpec{}, false
	}
	options := make([]CardOption, 0, len(workspaces))
	for _, ws := range workspaces {
		options = append(options, CardOption{ID: strconv.Itoa(ws.ID), Text: ws.Name})
	}
	return CardSpec{
		Title:       "关联工作空间",
		Description: "选择机器人归属的工作空间",
		Options:     options,
		TaskID:      wsbindCardPrefix + bindingFingerprint(workspaceIDKeys(workspaces)),
	}, true
}

// issueCardSpec 任务绑定卡构造（同上共用 SSOT 惯例）。
func issueCardSpec(issues []*model.ProjectIssue) (CardSpec, bool) {
	if len(issues) == 0 {
		return CardSpec{}, false
	}
	options := make([]CardOption, 0, len(issues))
	for _, issue := range issues {
		options = append(options, CardOption{ID: issue.ID, Text: issue.Name})
	}
	return CardSpec{
		Title:       "绑定任务",
		Description: "选择本会话绑定的任务",
		Options:     options,
		TaskID:      taskbindCardPrefix + bindingFingerprint(issueIDKeys(issues)),
	}, true
}

// workspaceIDKeys / issueIDKeys 候选 id 的指纹输入序列（与选项序同源——点击消费按下标
// 解析，指纹必须覆写下标所参照的同一次序列）。
func workspaceIDKeys(workspaces []*model.Workspace) []string {
	ids := make([]string, 0, len(workspaces))
	for _, ws := range workspaces {
		ids = append(ids, strconv.Itoa(ws.ID))
	}
	return ids
}

func issueIDKeys(issues []*model.ProjectIssue) []string {
	ids := make([]string, 0, len(issues))
	for _, issue := range issues {
		ids = append(ids, issue.ID)
	}
	return ids
}

// handleWsbindInteraction workspace 卡点击消费（wsbind- 域）。决策链：非选项 key miss →
// 重查列表（读库失败 fail visible，不置灰不落库）→ 指纹比对（不符回重取卡文案，同前）→
// 下标越界防御 → 落库 bot 行 workspace_id（直查 DO 惯例；bot 行已删视为失败）→ 置灰
// （重查列表重建完整 spec，指纹一致保证与原卡字段一致）+ 终帧 → applier 热重载（nil 跳过）。
func (r *driverRoute) handleWsbindInteraction(cfg BotRuntimeConfig, fingerprint string, in *Interaction) (string, *CardSpec, bool) {
	if in.ActionIndex < 0 {
		return "", nil, false // 非选项 key（如提交按钮）：单选卡不可达，防御 miss
	}
	wss, err := bindingWorkspaces(r.db)
	if err != nil {
		return "工作空间列表读取失败，请稍后重试。", nil, true
	}
	if bindingFingerprint(workspaceIDKeys(wss)) != fingerprint {
		return bindingListChangedText, nil, true
	}
	if in.ActionIndex >= len(wss) {
		return bindingInvalidOptionText, nil, true
	}
	ws := wss[in.ActionIndex]
	q := query.Use(r.db)
	res, err := q.ImBot.WithContext(context.Background()).Where(q.ImBot.ID.Eq(cfg.BotID)).UpdateSimple(
		q.ImBot.WorkspaceID.Value(ws.ID),
		q.ImBot.UpdatedAt.Value(time.Now()),
	)
	if err != nil || res.RowsAffected == 0 {
		return "工作空间关联失败，请稍后重试。", nil, true
	}
	gray, _ := workspaceCardSpec(wss)
	gray.Disabled = true
	if r.applier != nil {
		r.applier.ApplyBotAsync(cfg.BotID)
	}
	return "✅ 已关联工作空间「" + ws.Name + "」", &gray, true
}

// handleTaskbindInteraction 任务卡点击消费（taskbind- 域）。决策链同 wsbind 同构——重查
// 候选 → 指纹比对 → 越界防御 → 落库点击来源会话行（(botID, conversationKey) 粒度，行由
// 拦截步 HasSeen→GetOrCreate 保证在场）→ 置灰 + 终帧。绑定每回合现读，无需热重载。
func (r *driverRoute) handleTaskbindInteraction(cfg BotRuntimeConfig, msg InboundMessage, fingerprint string, in *Interaction) (string, *CardSpec, bool) {
	if in.ActionIndex < 0 {
		return "", nil, false // 非选项 key（如提交按钮）：单选卡不可达，防御 miss
	}
	issues, err := bindingIssueCandidates(r.db, cfg.WorkspaceID)
	if err != nil {
		return "任务列表读取失败，请稍后重试。", nil, true
	}
	if bindingFingerprint(issueIDKeys(issues)) != fingerprint {
		return bindingListChangedText, nil, true
	}
	if in.ActionIndex >= len(issues) {
		return bindingInvalidOptionText, nil, true
	}
	issue := issues[in.ActionIndex]
	store := &ConversationStore{DB: r.db}
	if err := store.SaveBoundIssueID(cfg.BotID, msg.ConversationKey, issue.ID); err != nil {
		return "任务绑定失败，请稍后重试。", nil, true
	}
	gray, _ := issueCardSpec(issues)
	gray.Disabled = true
	return "✅ 已绑定任务「" + issue.Name + "」，直接发消息即可向该任务下达任务。", &gray, true
}
