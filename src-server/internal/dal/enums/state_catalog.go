package enums

import (
	"database/sql/driver"
	"fmt"
)

// 本文件集中定义 issue 状态体系的前两层固定常量（docs/issue.md §2.1/§2.2）：
//  1. StateGroup 类型枚举（t_project_states.state_group_code）+ 分组展示元数据目录 StateGroupCatalog；
//  2. 子状态目录 StateCatalog（扁平数组，每条自带 GroupCode 显式维系关联）。
// 数据层不国际化——name/组名/色由 Go 直出中文，前端原样展示。

// =====================================================================
// 第 1 层：StateGroup 类型枚举
// =====================================================================

// StateGroup issue 状态分组枚举（t_project_states.state_group_code）。
// 取值 backlog/unstarted/started/completed/cancelled；其中 completed 组触发 issue.completed_at 写入。
type StateGroup string

const (
	STATE_GROUP_BACKLOG   StateGroup = "backlog"
	STATE_GROUP_UNSTARTED StateGroup = "unstarted"
	STATE_GROUP_STARTED   StateGroup = "started"
	STATE_GROUP_COMPLETED StateGroup = "completed"
	STATE_GROUP_CANCELLED StateGroup = "cancelled"
)

// Value 实现 driver.Valuer：写库时校验合法值并返回底层 string；非法值返回错误，由 gorm 在 INSERT/UPDATE 时触发。
func (s StateGroup) Value() (driver.Value, error) {
	switch s {
	case
		STATE_GROUP_BACKLOG,
		STATE_GROUP_UNSTARTED,
		STATE_GROUP_STARTED,
		STATE_GROUP_COMPLETED,
		STATE_GROUP_CANCELLED:
		return string(s), nil
	default:
		return nil, fmt.Errorf("invalid StateGroup: %v", s)
	}
}

// =====================================================================
// 第 1 层补充：分组展示元数据目录 StateGroupCatalog（固定写死，Go 直出中文）
// =====================================================================

// StateGroupMeta 描述一个状态分组的展示元数据（列表分组头/分组排序用）。
type StateGroupMeta struct {
	Code      StateGroup `json:"code"`      // state_group_code
	Name      string     `json:"name"`      // 中文展示名，Go 直出，不走 i18n
	Color     string     `json:"color"`
	SortOrder float64    `json:"sortOrder"`
}

// StateGroupCatalog 全部 5 个状态分组的固定展示元数据。
var StateGroupCatalog = []StateGroupMeta{
	{Code: STATE_GROUP_BACKLOG, Name: "待办池", Color: "#94a3b8", SortOrder: 10000},
	{Code: STATE_GROUP_UNSTARTED, Name: "未开始", Color: "#475569", SortOrder: 20000},
	{Code: STATE_GROUP_STARTED, Name: "进行中", Color: "#f59e0b", SortOrder: 30000},
	{Code: STATE_GROUP_COMPLETED, Name: "已完成", Color: "#16a34a", SortOrder: 40000},
	{Code: STATE_GROUP_CANCELLED, Name: "已取消", Color: "#ef4444", SortOrder: 50000},
}

// FindStateGroupMeta 按 group code 查分组元数据；未命中表示非法分组码。
func FindStateGroupMeta(code StateGroup) (StateGroupMeta, bool) {
	for _, g := range StateGroupCatalog {
		if g.Code == code {
			return g, true
		}
	}
	return StateGroupMeta{}, false
}

// =====================================================================
// 第 2 层：状态目录 StateCatalog（固定可选集，扁平数组）
// =====================================================================

// StateMeta 描述一个子状态。GroupCode 显式维系与分组的关联；无 devPhase——开发步骤即
// started 组里除「进行中」外的子 state，步骤内容按 Code 匹配（见 docs/issue.md §8）。
type StateMeta struct {
	GroupCode StateGroup `json:"groupCode"` // 归属 state_group_code（对应 StateGroupMeta.Code）
	Code      string     `json:"code"`      // state_code（普通 TEXT，非 typed enum；开发工作台按此 switch 步骤内容）
	Name      string     `json:"name"`      // 中文，Go 直出
	Color     string     `json:"color"`
	Icon      string     `json:"icon"`
	SortOrder float64    `json:"sortOrder"`
}

// StateCatalog 全部子状态的固定可选集。每条自带 GroupCode，与 t_project_states 存的
// (state_group_code, state_code) 一一对应；项目只能从中勾选，不能自造。
var StateCatalog = []StateMeta{
	{GroupCode: STATE_GROUP_BACKLOG, Code: "backlog", Name: "待办池", Color: "#94a3b8", SortOrder: 10000},
	{GroupCode: STATE_GROUP_UNSTARTED, Code: "todo", Name: "未开始", Color: "#475569", SortOrder: 20000},
	{GroupCode: STATE_GROUP_STARTED, Code: "in_progress", Name: "进行中", Color: "#f59e0b", SortOrder: 30000},
	{GroupCode: STATE_GROUP_STARTED, Code: "wt_init", Name: "worktree初始化", Color: "#0ea5e9", SortOrder: 31000},
	{GroupCode: STATE_GROUP_STARTED, Code: "developing", Name: "开发中", Color: "#2563eb", SortOrder: 32000},
	{GroupCode: STATE_GROUP_STARTED, Code: "pr_open", Name: "待合并PR", Color: "#7c3aed", SortOrder: 33000},
	{GroupCode: STATE_GROUP_STARTED, Code: "cleanup", Name: "待清理", Color: "#ea580c", SortOrder: 34000},
	{GroupCode: STATE_GROUP_COMPLETED, Code: "done", Name: "已完成", Color: "#16a34a", SortOrder: 40000},
	{GroupCode: STATE_GROUP_CANCELLED, Code: "cancelled", Name: "已取消", Color: "#ef4444", SortOrder: 50000},
}

// FindStateMeta 按 (groupCode, code) 查子状态定义；未命中表示该组合不在固定目录内（非法）。
func FindStateMeta(groupCode StateGroup, code string) (StateMeta, bool) {
	for _, s := range StateCatalog {
		if s.GroupCode == groupCode && s.Code == code {
			return s, true
		}
	}
	return StateMeta{}, false
}
