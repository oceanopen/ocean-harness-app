package bot

import (
	"fmt"
	"strings"
	"testing"

	"ocean-harness/server/internal/dal/model"
)

/*
 * TestBindingTaskIDRoundtrip 投递锚往返三段式（关键词 + 指纹 + 一次性随机段）+ 域互斥 +
 * 非法形态全 miss + 卡契约合法 + 同输入两次出卡锚不同（task_id 全局唯一约束）。
 */
func TestBindingTaskIDRoundtrip(t *testing.T) {
	ids := []string{"1", "22", "333"}
	// 空关键词与中文关键词两形态往返。
	wsEmpty := bindingTaskID(wsbindCardPrefix, "", ids)
	if kw, fp, ok := parseWsbindTaskID(wsEmpty); !ok || kw != "" || fp != bindingFingerprint(ids) {
		t.Fatalf("空关键词往返不符: kw=%q fp=%q ok=%v", kw, fp, ok)
	}
	wsKw := bindingTaskID(wsbindCardPrefix, "前端 仓", ids)
	if kw, fp, ok := parseWsbindTaskID(wsKw); !ok || kw != "前端 仓" || fp != bindingFingerprint(ids) {
		t.Fatalf("带关键词往返不符: kw=%q fp=%q ok=%v", kw, fp, ok)
	}
	itKw := bindingTaskID(taskbindCardPrefix, "登录", []string{"a", "b"})
	if kw, _, ok := parseTaskbindTaskID(itKw); !ok || kw != "登录" {
		t.Fatalf("taskbind 往返不符: kw=%q ok=%v", kw, ok)
	}
	// 解绑锚：关键词段恒空（单元素指纹），往返与卡契约同款。
	unAnchor := bindingTaskID(taskunbindCardPrefix, "", []string{"i-1"})
	if kw, fp, ok := parseTaskunbindTaskID(unAnchor); !ok || kw != "" || fp != bindingFingerprint([]string{"i-1"}) {
		t.Fatalf("taskunbind 往返不符: kw=%q fp=%q ok=%v", kw, fp, ok)
	}
	// 一次性随机段：同输入两次出卡锚不同（企微 taskid 已存在即拒发，errcode=42014 实证）。
	if again := bindingTaskID(wsbindCardPrefix, "", ids); again == wsEmpty {
		t.Fatalf("同输入两次出卡锚应不同: %q", again)
	}
	/*
	 * 域互斥 + 非法形态：对侧域锚、无前缀、段数不符、指纹与随机段空/过短/过长/非 hex、
	 * 关键词段非合法 hex 全部 miss（各域各验各的）。
	 */
	for _, bad := range []string{itKw, unAnchor, "acp-x@y-perm-1", "wsbind-", "wsbind-abc", "wsbind-123456789", "wsbind-01234567", "wsbind-xy-01234567", "wsbind--0123456", "wsbind-abc-0123456789", "wsbind--01234567", "wsbind--01234567-zzzzzzzz", "wsbind--01234567-0123456", "wsbind--01234567-01234567-extra"} {
		if _, _, ok := parseWsbindTaskID(bad); ok {
			t.Fatalf("parseWsbindTaskID(%q) 应 miss", bad)
		}
	}
	for _, bad := range []string{wsKw, unAnchor, "acp-x@y-perm-1", "taskbind-", "taskbind-abc", "taskbind-123456789", "taskbind-zzzzzzzz", "taskbind-01234567", "taskbind-xyz-01234567", "taskbind--01234567", "taskbind--01234567-0123456789abcdef"} {
		if _, _, ok := parseTaskbindTaskID(bad); ok {
			t.Fatalf("parseTaskbindTaskID(%q) 应 miss", bad)
		}
	}
	for _, bad := range []string{wsKw, itKw, "taskunbind-", "taskunbind-abc", "taskunbind-xyz-01234567", "taskunbind--01234567"} {
		if _, _, ok := parseTaskunbindTaskID(bad); ok {
			t.Fatalf("parseTaskunbindTaskID(%q) 应 miss", bad)
		}
	}
	// 卡契约：前缀 + hex + 指纹字符集天然过 ValidateCardTaskID。
	if err := ValidateCardTaskID(wsKw); err != nil {
		t.Fatalf("wsbind 锚应过卡契约: %v", err)
	}
	if err := ValidateCardTaskID(itKw); err != nil {
		t.Fatalf("taskbind 锚应过卡契约: %v", err)
	}
	if err := ValidateCardTaskID(unAnchor); err != nil {
		t.Fatalf("taskunbind 锚应过卡契约: %v", err)
	}
	// 40 字节关键词（≈13 汉字）hex 后锚长仍在 128 字节内。
	long := bindingTaskID(taskbindCardPrefix, strings.Repeat("k", 40), ids)
	if err := ValidateCardTaskID(long); err != nil {
		t.Fatalf("40 字节关键词锚应过卡契约: %v", err)
	}
}

/*
 * TestBindingCardTexts 出卡同帧文本契约：引导语区分带/不带关键词、完整名称列表、
 * 缩小范围提示（触及上限才出）。
 */
func TestBindingCardTexts(t *testing.T) {
	wss := []*model.Workspace{{ID: "ws-1", Name: "前端仓"}, {ID: "ws-2", Name: "后端仓"}}
	if got := workspaceCardText("", wss); got != "请点击卡片选择要关联的工作空间：\n- 前端仓\n- 后端仓" {
		t.Fatalf("全列表引导文本不符: %q", got)
	}
	if got := workspaceCardText("前端", wss[:1]); got != "匹配到 1 个工作空间，请点击卡片选择：\n- 前端仓" {
		t.Fatalf("关键词引导文本不符: %q", got)
	}
	issues := []*model.ProjectTask{{ID: "i-1", Name: "登录重构"}}
	if got := issueCardText("", issues); got != "请点击卡片选择要绑定的任务：\n- 登录重构" {
		t.Fatalf("任务卡引导文本不符: %q", got)
	}
	if got := issueCardText("登录", issues); got != "匹配到 1 个任务，请点击卡片选择：\n- 登录重构" {
		t.Fatalf("任务卡关键词文本不符: %q", got)
	}
	// 缩小范围提示：仅恰满上限时附带。
	full := make([]*model.Workspace, bindingOptionLimit)
	for i := range full {
		full[i] = &model.Workspace{ID: fmt.Sprintf("ws-%02d", i), Name: fmt.Sprintf("仓%02d", i)}
	}
	if got := workspaceCardText("", full); !strings.Contains(got, "缩小关键词范围") {
		t.Fatalf("满上限应附缩小范围提示: %q", got)
	}
	if got := workspaceCardText("", wss); strings.Contains(got, "缩小关键词范围") {
		t.Fatalf("未满上限不应附提示: %q", got)
	}
}

/*
 * TestBindingFingerprintStability 指纹稳定性：同列表恒同值；内容因子（增删）与次序因子
 * （换位）任一变化即不符——候选查询确定序的依据。
 */
func TestBindingFingerprintStability(t *testing.T) {
	ids := []string{"1", "22", "333"}
	a := bindingFingerprint(ids)
	if b := bindingFingerprint(ids); a != b || len(a) != bindingFingerprintLen {
		t.Fatalf("同列表应恒同指纹: %q vs %q", a, b)
	}
	if bindingFingerprint([]string{"1", "22"}) == a || bindingFingerprint([]string{"1", "22", "333", "4"}) == a {
		t.Fatal("候选集增删应致指纹不符")
	}
	if bindingFingerprint([]string{"333", "22", "1"}) == a {
		t.Fatal("同集换位应致指纹不符")
	}
}

/*
 * bindingTaskIDMatches 解析式卡锚断言助手：TaskID 含一次性随机段（企微 taskid 全局唯一
 * 约束），字面/重算比对恒假——按「关键词 + 指纹」语义段比对（随机段不参与语义）。
 */
func bindingTaskIDMatches(taskID, prefix, keyword string, ids []string) bool {
	kw, fp, ok := parseBindingTaskID(taskID, prefix)
	return ok && kw == keyword && fp == bindingFingerprint(ids)
}

/*
 * TestBindingCardSpecConstruction 卡构造 SSOT：单选点击即答形态、TaskID 关键词 + 指纹锚、
 * 置灰重建同构（企微整卡替换；TaskID 随机段不参与比对）、空列表不出卡。
 */
func TestBindingCardSpecConstruction(t *testing.T) {
	wss := []*model.Workspace{{ID: "ws-3", Name: "前端仓"}, {ID: "ws-7", Name: "后端仓"}}
	spec, ok := workspaceCardSpec(wss, "")
	if !ok {
		t.Fatal("非空列表应出卡")
	}
	if spec.Multiple || spec.Submit {
		t.Fatal("绑定卡为单选点击即答形态")
	}
	if spec.Title == "" || spec.Description == "" || len(spec.Options) != 2 ||
		spec.Options[0].ID != "ws-3" || spec.Options[0].Text != "前端仓" || spec.Options[1].ID != "ws-7" {
		t.Fatalf("workspace 卡字段不符: %+v", spec)
	}
	if !bindingTaskIDMatches(spec.TaskID, wsbindCardPrefix, "", workspaceIDKeys(wss)) {
		t.Fatalf("TaskID 应为关键词 + 候选指纹锚: %q", spec.TaskID)
	}
	// 带关键词构造：锚编码过滤关键词（指纹输入是过滤后序列）。
	kwSpec, _ := workspaceCardSpec(wss[:1], "前端")
	if !bindingTaskIDMatches(kwSpec.TaskID, wsbindCardPrefix, "前端", []string{"ws-3"}) {
		t.Fatalf("带关键词 TaskID 不符: %q", kwSpec.TaskID)
	}
	// 置灰重建共用 SSOT：同构造字段一致（随机段每次出新；语义段关键词 + 指纹须一致）。
	gray, _ := workspaceCardSpec(wss, "")
	gray.Disabled = true
	if !bindingTaskIDMatches(gray.TaskID, wsbindCardPrefix, "", workspaceIDKeys(wss)) ||
		!gray.Disabled || len(gray.Options) != len(spec.Options) {
		t.Fatal("置灰卡应与原卡同构仅 Disabled")
	}
	// 空列表不出卡（触发侧回纯文本）。
	if _, ok := workspaceCardSpec(nil, ""); ok {
		t.Fatal("空 workspace 列表不应出卡")
	}
	if _, ok := issueCardSpec(nil, ""); ok {
		t.Fatal("空候选列表不应出卡")
	}
	// 任务卡同构。
	issues := []*model.ProjectTask{{ID: "i-1", Name: "登录重构"}, {ID: "i-2", Name: "接口联调"}}
	ispec, ok := issueCardSpec(issues, "")
	if !ok || len(ispec.Options) != 2 || ispec.Options[1].Text != "接口联调" ||
		!bindingTaskIDMatches(ispec.TaskID, taskbindCardPrefix, "", issueIDKeys(issues)) {
		t.Fatalf("任务卡字段不符: %+v", ispec)
	}
}
