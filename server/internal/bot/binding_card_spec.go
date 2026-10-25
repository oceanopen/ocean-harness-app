/*
 * 本文件是绑定族卡片的无状态构建面：TaskID 编码（<前缀><关键词 hex>-<候选指纹>-<随机段>）
 * 与 workspace/任务绑定卡、解绑确认卡的 spec/文案纯函数。交互消费链（#任务/#工作空间 指令、
 * last_message_card 闸、点击落库）已随绑定流退役；本域保留为卡片构建能力。
 *
 * TODO: 后续经 MCP 暴露为 AI 可调用的卡片工具（范式同 mcp_im_delivery 的 send_file）。
 */
package bot

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"ocean-harness/server/internal/dal/model"
)

// TaskID 前缀族（与 acp- 前缀族互斥）。
const (
	wsbindCardPrefix     = "wsbind-"
	taskbindCardPrefix   = "taskbind-"
	taskunbindCardPrefix = "taskunbind-"
)

// bindingFingerprintLen 指纹 hex 字符数；解析按此精确校验（域内形态互斥）。
const bindingFingerprintLen = 8

/*
 * bindingOptionLimit 绑定卡的候选/选项上限（企微 vote 选项上限 20 之内留余量）；超出截断，
 * 完整名称列表由触发方在同帧 content 附带。
 */
const bindingOptionLimit = 10

/*
 * bindingFingerprint 候选列表指纹：有序 id 串（逗号连接）的 fnv64a 摘要前 8 位 hex——
 * 内容与次序双因子，任一 id 增删或换位即不符。
 */
func bindingFingerprint(ids []string) string {
	h := fnv.New64a()
	h.Write([]byte(strings.Join(ids, ",")))
	return fmt.Sprintf("%016x", h.Sum64())[:bindingFingerprintLen]
}

/*
 * bindingTaskID 投递锚编码：<前缀><关键词 utf-8 hex>-<候选列表指纹>-<一次性随机段>
 * （空关键词 = 空段）。指纹输入是关键词过滤后的候选序列；随机段保证每次出卡 task_id
 * 全局唯一（企微约束 taskid 已存在即拒发）。
 */
func bindingTaskID(prefix, keyword string, ids []string) string {
	return prefix + hex.EncodeToString([]byte(keyword)) + "-" + bindingFingerprint(ids) + "-" + nonceHex8()
}

/*
 * nonceHex8 一次性随机段：crypto/rand 4 字节 → 8 位小写 hex。熵源故障退化时间戳低位
 * （撞车表现为出卡被拒可重试，不致命）。
 */
func nonceHex8() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%08x", time.Now().UnixNano()&0xffffffff)
	}
	return hex.EncodeToString(b[:])
}

/*
 * parseBindingTaskID 解码本域投递锚（搜索关键词 + 指纹 + 随机段三段式）：指纹与随机段
 * 均须恰 8 位小写 hex；关键词段非合法 hex 视为非本域形态。
 */
func parseBindingTaskID(taskID, prefix string) (keyword, fingerprint string, ok bool) {
	rest, hit := strings.CutPrefix(taskID, prefix)
	if !hit {
		return "", "", false
	}
	parts := strings.Split(rest, "-")
	if len(parts) != 3 || !isBindingHex8(parts[1]) || !isBindingHex8(parts[2]) {
		return "", "", false
	}
	kw, err := hex.DecodeString(parts[0])
	if err != nil {
		return "", "", false
	}
	return string(kw), parts[1], true
}

// parseWsbindTaskID / parseTaskbindTaskID / parseTaskunbindTaskID 解码各自域的投递锚。
func parseWsbindTaskID(taskID string) (keyword, fingerprint string, ok bool) {
	return parseBindingTaskID(taskID, wsbindCardPrefix)
}

func parseTaskbindTaskID(taskID string) (keyword, fingerprint string, ok bool) {
	return parseBindingTaskID(taskID, taskbindCardPrefix)
}

func parseTaskunbindTaskID(taskID string) (keyword, fingerprint string, ok bool) {
	return parseBindingTaskID(taskID, taskunbindCardPrefix)
}

// isBindingHex8 恰 8 位小写 hex（指纹段与一次性随机段共用形态）。
func isBindingHex8(s string) bool {
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

/*
 * workspaceCardSpec workspace 绑定卡构造。单选 vote 形态；空列表 ok=false（无可选项不出卡）。
 * 选项文案企微截 11 字，完整名称由触发方在同帧 content 附带。
 */
func workspaceCardSpec(workspaces []*model.Workspace, keyword string) (CardSpec, bool) {
	if len(workspaces) == 0 {
		return CardSpec{}, false
	}
	options := make([]CardOption, 0, len(workspaces))
	for _, ws := range workspaces {
		options = append(options, CardOption{ID: ws.ID, Text: ws.Name})
	}
	return CardSpec{
		Title:       "关联工作空间",
		Description: "选择机器人归属的工作空间",
		Options:     options,
		TaskID:      bindingTaskID(wsbindCardPrefix, keyword, workspaceIDKeys(workspaces)),
	}, true
}

// issueCardSpec 任务绑定卡构造（同上）。
func issueCardSpec(issues []*model.ProjectTask, keyword string) (CardSpec, bool) {
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

// taskunbindCardSpec 解绑确认卡构造：单选项 = 出卡时绑定的任务，指纹 = 绑定 id 单元素序列。
func taskunbindCardSpec(issue *model.ProjectTask) CardSpec {
	return CardSpec{
		Title:       "解绑任务",
		Description: "点击确认解除本会话的任务绑定",
		Options:     []CardOption{{ID: issue.ID, Text: issue.Name}},
		TaskID:      bindingTaskID(taskunbindCardPrefix, "", []string{issue.ID}),
	}
}

/*
 * workspaceCardText / issueCardText 出卡同帧文本：引导语 + 完整名称列表（卡面选项截 11 字，
 * 完整信息在此）；触及候选上限时提示缩小范围。
 */
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
	b.WriteString(bindingMoreHint(len(workspaces)))
	return b.String()
}

func issueCardText(keyword string, issues []*model.ProjectTask) string {
	lead := "请点击卡片选择要绑定的任务："
	if keyword != "" {
		lead = "匹配到 " + strconv.Itoa(len(issues)) + " 个任务，请点击卡片选择："
	}
	var b strings.Builder
	b.WriteString(lead)
	for _, issue := range issues {
		b.WriteString("\n- " + issue.Name)
	}
	b.WriteString(bindingMoreHint(len(issues)))
	return b.String()
}

// taskunbindCardText 解绑卡同帧文本：确认引导 + 任务全名。
func taskunbindCardText(issue *model.ProjectTask) string {
	return "当前会话已绑定任务，点击卡片确认解绑：\n- " + issue.Name
}

// bindingMoreHint 触及候选上限的缩小范围提示（n < 上限时为空串）。
func bindingMoreHint(n int) string {
	if n < bindingOptionLimit {
		return ""
	}
	return "\n（列表已截前 " + strconv.Itoa(bindingOptionLimit) + " 项，更多请缩小关键词范围）"
}

/*
 * numberedOptionList 编号列表渲染（1 基）：无卡渠道的绑定选择降级形态，编号回复按选项序
 * 消费。
 */
func numberedOptionList(lead string, texts []string) string {
	var b strings.Builder
	b.WriteString(lead)
	for i, t := range texts {
		b.WriteString("\n" + strconv.Itoa(i+1) + ". " + t)
	}
	return b.String()
}

// workspaceNumberedText workspace 绑定的编号文本渲染（无卡渠道）。
func workspaceNumberedText(keyword string, workspaces []*model.Workspace) string {
	lead := "请回复编号选择要关联的工作空间："
	if keyword != "" {
		lead = "匹配到 " + strconv.Itoa(len(workspaces)) + " 个工作空间，请回复编号选择："
	}
	texts := make([]string, 0, len(workspaces))
	for _, ws := range workspaces {
		texts = append(texts, ws.Name)
	}
	return numberedOptionList(lead, texts) + bindingMoreHint(len(workspaces))
}

// issueNumberedText 任务绑定的编号文本渲染（无卡渠道）。
func issueNumberedText(keyword string, issues []*model.ProjectTask) string {
	lead := "请回复编号选择要绑定的任务："
	if keyword != "" {
		lead = "匹配到 " + strconv.Itoa(len(issues)) + " 个任务，请回复编号选择："
	}
	texts := make([]string, 0, len(issues))
	for _, issue := range issues {
		texts = append(texts, issue.Name)
	}
	return numberedOptionList(lead, texts) + bindingMoreHint(len(issues))
}

// taskunbindNumberedText 解绑确认的编号文本渲染（无卡渠道，单选项恒 1）。
func taskunbindNumberedText(issue *model.ProjectTask) string {
	return numberedOptionList("当前会话已绑定任务，回复 1 确认解绑：", []string{issue.Name})
}

/*
 * workspaceIDKeys / issueIDKeys 候选 id 的指纹输入序列（与选项序同源——按下标解析时指纹
 * 必须覆写下标所参照的同一次序列）。
 */
func workspaceIDKeys(workspaces []*model.Workspace) []string {
	ids := make([]string, 0, len(workspaces))
	for _, ws := range workspaces {
		ids = append(ids, ws.ID)
	}
	return ids
}

func issueIDKeys(issues []*model.ProjectTask) []string {
	ids := make([]string, 0, len(issues))
	for _, issue := range issues {
		ids = append(ids, issue.ID)
	}
	return ids
}
