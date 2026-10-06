// 本文件是绑定卡域（T2.1 构造与点击消费 / T2.2 统一「搜索 → 卡片 → 提交绑定」，解绑同模型）：
// workspace / 任务两类绑定卡与任务解绑确认卡的候选查询、出站构造与入站点击消费。TaskID 无
// 状态自包含——<前缀><关键词 hex>-<候选列表指纹 8 位 hex>：候选列表即全部状态，搜索关键词
// 是复原候选集的必要因子（不在投递锚里则点击侧无法重查命中集，连下标到目标的映射都做不出）。
// 搜索卡（wsbind-/taskbind-）点击时解码关键词重查同款模糊查询算指纹比对，一致才按
// ActionIndex 下标解析目标落库——列表增删或换位均指纹不符，回「重新获取卡片」防索引错绑；
// 解绑卡（taskunbind-）无搜索语义：关键词段恒空、指纹 = 绑定 id 单元素序列，点击时与当前
// 绑定比对后清锚（issueRowByID 为触发/点击两侧共用取行口）。跨 sidecar 重启与迟到点击
// 同样自然降级为文案告知，无内存注册表可失配（与 perm/elicit 卡同哲学）。
package bot

import (
	"context"
	"database/sql/driver"
	"encoding/hex"
	"errors"
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
	wsbindCardPrefix     = "wsbind-"
	taskbindCardPrefix   = "taskbind-"
	taskunbindCardPrefix = "taskunbind-"
)

// bindingFingerprintLen 指纹 hex 字符数；解析按此精确校验（域内形态互斥）。
const bindingFingerprintLen = 8

// bindingOptionLimit 两类绑定卡的候选/选项上限（企微 vote 选项上限 20 之内留余量，T2.2
// 拍板统一 10）；超出截断，完整名称列表由触发链在同帧 content 附带并提示缩小范围。
const bindingOptionLimit = 10

// issueTerminalStateCodes 绑定候选的终态集（bot 域 SSOT，与前端 isDevIssue 同判定语义
// ——事实 9）：归档 = state_code 流转 DONE / CANCELLED，终态任务不再进任务卡候选；子任务
// 不在任务卡候选（顶级口径），与左树「所见即所绑」一致。元素类型取 driver.Valuer
// （StateCode 字段表达式 NotIn 的形参面，StateCode 枚举原生实现该接口）。
var issueTerminalStateCodes = []driver.Valuer{enums.STATE_CODE_DONE, enums.STATE_CODE_CANCELLED}

// 绑定卡通用文案（触发侧与点击侧共用；落库失败文案在各 handler 内联，故障面各自区分
// ——taskunbind 例外：解绑链路读/写失败跨故障面收敛一句，经 issue_command.go 的
// issueUnbindFailedText 共享）。
const (
	bindingListChangedText    = "列表已变化，请重新发送指令获取新卡片。"
	bindingInvalidOptionText  = "点击的选项无效，请重新点击卡片中的选项。"
	workspaceLookupFailedText = "工作空间列表读取失败，请稍后重试。"
	issueLookupFailedText     = "任务列表读取失败，请稍后重试。"
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

// bindingTaskID 投递锚编码：<前缀><关键词 utf-8 hex>-<候选列表指纹>（空关键词 = 空段，
// 形如 wsbind--<fp>；关键词经 normalizeBindingKeyword 截限，hex 后总长留足 128 字节上限
// 余量）。指纹输入是关键词过滤后的候选序列——与点击侧重查所见同源。
func bindingTaskID(prefix, keyword string, ids []string) string {
	return prefix + hex.EncodeToString([]byte(keyword)) + "-" + bindingFingerprint(ids)
}

// parseBindingTaskID 解码本域投递锚（搜索关键词 + 尾段指纹）：末个 "-" 前为关键词 hex 段
// （空段 = 空关键词）、其后须恰 8 位小写 hex 指纹；关键词段非合法 hex（奇数长度 / 非 hex
// 字符）视为非本域形态。T2.1 旧形态 <prefix><fp8>（无关键词段）同样 miss——升级窗口内的
// 旧卡点击自然降级为拦截步兜底文案。
func parseBindingTaskID(taskID, prefix string) (keyword, fingerprint string, ok bool) {
	rest, hit := strings.CutPrefix(taskID, prefix)
	if !hit {
		return "", "", false
	}
	i := strings.LastIndex(rest, "-")
	if i < 0 {
		return "", "", false
	}
	kwHex, fp := rest[:i], rest[i+1:]
	if !isBindingFingerprint(fp) {
		return "", "", false
	}
	kw, err := hex.DecodeString(kwHex)
	if err != nil {
		return "", "", false
	}
	return string(kw), fp, true
}

// parseWsbindTaskID / parseTaskbindTaskID 解码各自域的投递锚（尾段指纹）；非本域形态
// ok=false，调用方 miss 交下一分支或拦截步兜底。
func parseWsbindTaskID(taskID string) (keyword, fingerprint string, ok bool) {
	return parseBindingTaskID(taskID, wsbindCardPrefix)
}

func parseTaskbindTaskID(taskID string) (keyword, fingerprint string, ok bool) {
	return parseBindingTaskID(taskID, taskbindCardPrefix)
}

// parseTaskunbindTaskID 解码解绑卡投递锚：关键词段恒空（解绑无搜索语义，形如
// taskunbind--<fp8>），保留对称签名供 TryHandleInteraction 统一分流。
func parseTaskunbindTaskID(taskID string) (keyword, fingerprint string, ok bool) {
	return parseBindingTaskID(taskID, taskunbindCardPrefix)
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

// bindingWorkspaces workspace 卡候选：可选关键词名称模糊（空 = 全部），ID 升序取前
// bindingOptionLimit（确定序——指纹与选项下标都依赖确定序）。LIKE 通配符不转义：误放大
// 退化为多候选，无害（触发侧与点击侧重查同款语义，指纹可比性不受影响）。
func bindingWorkspaces(db *gorm.DB, keyword string) ([]*model.Workspace, error) {
	q := query.Use(db)
	ws := q.Workspace.WithContext(context.Background())
	if keyword != "" {
		ws = ws.Where(q.Workspace.Name.Like("%" + keyword + "%"))
	}
	return ws.Order(q.Workspace.ID).Limit(bindingOptionLimit).Find()
}

// bindingIssueCandidates 任务卡候选（候选口径拍板）：bot workspace 域内非终态
// （state_code ∉ 终态集）且顶级（parent_id 空——对齐前端 isDevIssue 左树口径，写入方
// 空串与 NULL 两形态都算顶级）的 issue，可选关键词名称模糊，SortOrder 排序 + ID 次序键
// （防并列抖动致指纹假性不符）取前 bindingOptionLimit。
func bindingIssueCandidates(db *gorm.DB, workspaceID int, keyword string) ([]*model.ProjectIssue, error) {
	q := query.Use(db)
	pi := q.ProjectIssue.WithContext(context.Background())
	if keyword != "" {
		pi = pi.Where(q.ProjectIssue.Name.Like("%" + keyword + "%"))
	}
	return pi.Where(
		q.ProjectIssue.WorkspaceID.Eq(workspaceID),
		q.ProjectIssue.StateCode.NotIn(issueTerminalStateCodes...),
		field.Or(q.ProjectIssue.ParentID.IsNull(), q.ProjectIssue.ParentID.Eq("")),
	).Order(q.ProjectIssue.SortOrder, q.ProjectIssue.ID).Limit(bindingOptionLimit).Find()
}

// workspaceCardSpec workspace 绑定卡构造（首发与置灰重建的共用 SSOT，对齐 permissionCardSpec
// 惯例——指纹一致保证两态字段一致，企微整卡替换不丢字段）。单选 vote 形态（Multiple /
// Submit 缺省 false，点击即答走 ActionIndex）；空列表 ok=false（无可选项不出卡，触发侧
// 回纯文本）。选项文案企微截 11 字，完整名称由触发链在同帧 content 附带。
func workspaceCardSpec(workspaces []*model.Workspace, keyword string) (CardSpec, bool) {
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
		TaskID:      bindingTaskID(wsbindCardPrefix, keyword, workspaceIDKeys(workspaces)),
	}, true
}

// issueCardSpec 任务绑定卡构造（同上共用 SSOT 惯例）。
func issueCardSpec(issues []*model.ProjectIssue, keyword string) (CardSpec, bool) {
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
		TaskID:      bindingTaskID(taskbindCardPrefix, keyword, issueIDKeys(issues)),
	}, true
}

// workspaceCardText / issueCardText 出卡同帧文本：引导语 + 完整名称列表（卡面选项截 11 字，
// 完整信息在此）；触及候选上限时提示用指令关键词缩小范围。
func workspaceCardText(keyword string, workspaces []*model.Workspace) string {
	lead := "请点击卡片选择要关联的工作空间："
	if keyword != "" {
		lead = "匹配到 " + strconv.Itoa(len(workspaces)) + " 个工作空间，请点击卡片选择："
	}
	var b strings.Builder
	b.WriteString(lead)
	for _, ws := range workspaces {
		b.WriteString("\n- " + ws.Name)
	}
	b.WriteString(bindingMoreHint(len(workspaces), workspaceCommandToken))
	return b.String()
}

func issueCardText(keyword string, issues []*model.ProjectIssue) string {
	lead := "请点击卡片选择要绑定的任务："
	if keyword != "" {
		lead = "匹配到 " + strconv.Itoa(len(issues)) + " 个任务，请点击卡片选择："
	}
	var b strings.Builder
	b.WriteString(lead)
	for _, issue := range issues {
		b.WriteString("\n- " + issue.Name)
	}
	b.WriteString(bindingMoreHint(len(issues), issueCommandToken))
	return b.String()
}

// bindingMoreHint 触及候选上限的缩小范围提示（n < 上限时为空串；恰满上限时误报无害）。
func bindingMoreHint(n int, token string) string {
	if n < bindingOptionLimit {
		return ""
	}
	return "\n（列表已截前 " + strconv.Itoa(bindingOptionLimit) + " 项，更多请用 " + token + " <关键词> 缩小范围）"
}

// workspaceEmptyText / issueEmptyText 触发侧零候选文案（库空与关键词零命中分开教学）。
func workspaceEmptyText(keyword string) string {
	if keyword == "" {
		return "暂无可选的工作空间，请先在桌面端创建工作空间后再发送 " + workspaceCommandToken + "。"
	}
	return "未找到匹配「" + keyword + "」的工作空间，可换个关键词或发送 " + workspaceCommandToken + " 查看全部。"
}

func issueEmptyText(keyword string) string {
	if keyword == "" {
		return "当前工作空间暂无可绑定的任务。"
	}
	return "未找到匹配「" + keyword + "」的任务，可换个关键词或发送 " + issueCommandToken + " 查看全部。"
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
// 解码关键词重查列表（读库失败 fail visible，不置灰不落库）→ 指纹比对（不符回重取卡文案，
// 同前）→ 下标越界防御 → 落库 bot 行 workspace_id（直查 DO 惯例；bot 行已删视为失败）→
// 置灰（重查列表重建完整 spec，指纹一致保证与原卡字段一致）+ 终帧 → applier 热重载
// （nil 跳过）。
func (r *driverRoute) handleWsbindInteraction(cfg BotRuntimeConfig, keyword, fingerprint string, in *Interaction) (string, *CardSpec, bool) {
	if in.ActionIndex < 0 {
		return "", nil, false // 非选项 key（如提交按钮）：单选卡不可达，防御 miss
	}
	wss, err := bindingWorkspaces(r.db, keyword)
	if err != nil {
		return workspaceLookupFailedText, nil, true
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
	gray, _ := workspaceCardSpec(wss, keyword)
	gray.Disabled = true
	if r.applier != nil {
		r.applier.ApplyBotAsync(cfg.BotID)
	}
	return "✅ 已关联工作空间「" + ws.Name + "」", &gray, true
}

// handleTaskbindInteraction 任务卡点击消费（taskbind- 域）。决策链同 wsbind 同构——解码
// 关键词重查候选 → 指纹比对 → 越界防御 → 落库点击来源会话行（(botID, conversationKey)
// 粒度，行由拦截步 HasSeen→GetOrCreate 保证在场）→ 置灰 + 终帧。绑定每回合现读，无需热重载。
func (r *driverRoute) handleTaskbindInteraction(cfg BotRuntimeConfig, msg InboundMessage, keyword, fingerprint string, in *Interaction) (string, *CardSpec, bool) {
	if in.ActionIndex < 0 {
		return "", nil, false // 非选项 key（如提交按钮）：单选卡不可达，防御 miss
	}
	issues, err := bindingIssueCandidates(r.db, cfg.WorkspaceID, keyword)
	if err != nil {
		return issueLookupFailedText, nil, true
	}
	if bindingFingerprint(issueIDKeys(issues)) != fingerprint {
		return bindingListChangedText, nil, true
	}
	if in.ActionIndex >= len(issues) {
		return bindingInvalidOptionText, nil, true
	}
	issue := issues[in.ActionIndex]
	store := &ConversationStore{DB: r.db}
	rows, err := store.SaveBoundIssueID(cfg.BotID, msg.ConversationKey, issue.ID)
	if err != nil || rows == 0 { // 0 行 = 会话行不在场：绑定未生效，不得回成功文案
		return "任务绑定失败，请稍后重试。", nil, true
	}
	gray, _ := issueCardSpec(issues, keyword)
	gray.Disabled = true
	return "✅ 已绑定任务「" + issue.Name + "」，直接发消息即可向该任务下达任务。", &gray, true
}

// issueRowByID 按 id 直查 issue 行（解绑链路触发侧与点击侧共用的取行口，本域消费逻辑
// 不外溢）：found=false 收敛 ErrRecordNotFound（悬空锚语义由调用方分派文案），其余错误
// 原样透传。
func issueRowByID(db *gorm.DB, id string) (issue *model.ProjectIssue, found bool, err error) {
	q := query.Use(db)
	issue, err = q.ProjectIssue.WithContext(context.Background()).Where(q.ProjectIssue.ID.Eq(id)).First()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return issue, true, nil
}

// taskunbindCardSpec 解绑确认卡构造（首发与置灰重建的共用 SSOT，同上惯例）：单选项 =
// 出卡时绑定的任务（会话级绑定恰一个）。TaskID 关键词段恒空（解绑无搜索语义），指纹 =
// 绑定 issue id 单元素序列——点击时与当前绑定比对，出卡后已解绑/换绑均指纹不符。
func taskunbindCardSpec(issue *model.ProjectIssue) CardSpec {
	return CardSpec{
		Title:       "解绑任务",
		Description: "点击确认解除本会话的任务绑定",
		Options:     []CardOption{{ID: issue.ID, Text: issue.Name}},
		TaskID:      bindingTaskID(taskunbindCardPrefix, "", []string{issue.ID}),
	}
}

// taskunbindCardText 解绑卡同帧文本：确认引导 + 任务全名（卡面选项截 11 字，完整名在此）。
func taskunbindCardText(issue *model.ProjectIssue) string {
	return "当前会话已绑定任务，点击卡片确认解绑：\n- " + issue.Name
}

// handleTaskunbindInteraction 解绑卡点击消费（taskunbind- 域）。决策链与绑定卡同构——
// 读当前绑定（读库失败 fail visible）→ 指纹比对（出卡后已解绑/换绑均不符，回重取卡文案
// 不落库）→ 越界防御（单选项）→ 读绑定行取名（悬空锚按已变化收口）→ 清绑定锚（幂等
// 忽略行数，claude_session_id 不动——两锚正交）→ 置灰 + 终帧。
func (r *driverRoute) handleTaskunbindInteraction(cfg BotRuntimeConfig, msg InboundMessage, fingerprint string, in *Interaction) (string, *CardSpec, bool) {
	if in.ActionIndex < 0 {
		return "", nil, false // 非选项 key（如提交按钮）：单选卡不可达，防御 miss
	}
	store := &ConversationStore{DB: r.db}
	bound, err := store.BoundIssueID(cfg.BotID, msg.ConversationKey)
	if err != nil {
		return issueUnbindFailedText(), nil, true
	}
	if bindingFingerprint([]string{bound}) != fingerprint {
		return bindingListChangedText, nil, true
	}
	if in.ActionIndex >= 1 {
		return bindingInvalidOptionText, nil, true
	}
	issue, found, err := issueRowByID(r.db, bound)
	if err != nil {
		return issueUnbindFailedText(), nil, true
	}
	if !found {
		return bindingListChangedText, nil, true // 悬空锚（issue 删除级联清列后不应达）：防御收口
	}
	// 清锚幂等：空串覆盖，行数忽略（未绑定的点击已被指纹比对拦下，此处行恒在场）。
	if _, err := store.SaveBoundIssueID(cfg.BotID, msg.ConversationKey, ""); err != nil {
		return issueUnbindFailedText(), nil, true
	}
	gray := taskunbindCardSpec(issue)
	gray.Disabled = true
	return "✅ 已解绑任务「" + issue.Name + "」", &gray, true
}
