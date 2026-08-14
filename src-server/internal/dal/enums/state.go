package enums

import (
	"database/sql/driver"
	"fmt"
)

// 本文件定义 issue 状态体系的固定常量：StateCode 类型枚举（t_project_issues.state_code）+
// 展示元数据目录 StateCatalog。状态为扁平模型（无 state_group/子状态层级、无 state_id、无 sortorder）——
// 固定 5 个状态，顺序即 StateCatalog 数组序。数据层不国际化——name/色由 Go 直出中文，前端原样展示。

// StateCode issue 状态枚举（t_project_issues.state_code）。
// 取值 BACKLOG/TODO/IN_PROGRESS/DONE/CANCELLED；DONE 触发 issue.completed_at 写入。
type StateCode string

const (
	STATE_CODE_BACKLOG     StateCode = "BACKLOG"
	STATE_CODE_TODO        StateCode = "TODO"
	STATE_CODE_IN_PROGRESS StateCode = "IN_PROGRESS"
	STATE_CODE_DONE        StateCode = "DONE"
	STATE_CODE_CANCELLED   StateCode = "CANCELLED"
)

// STATE_CODE_DEFAULT 新建 issue 的默认状态（固定常量，不再有项目级 default_state_id）。
const STATE_CODE_DEFAULT = STATE_CODE_BACKLOG

// Value 实现 driver.Valuer：写库时校验合法值并返回底层 string；非法值返回错误，由 gorm 在 INSERT/UPDATE 时触发。
func (s StateCode) Value() (driver.Value, error) {
	switch s {
	case
		STATE_CODE_BACKLOG,
		STATE_CODE_TODO,
		STATE_CODE_IN_PROGRESS,
		STATE_CODE_DONE,
		STATE_CODE_CANCELLED:
		return string(s), nil
	default:
		return nil, fmt.Errorf("invalid StateCode: %v", s)
	}
}

// StateMeta 描述一个状态的展示元数据（列表分组头/徽章/看板列头用）。
type StateMeta struct {
	Code  StateCode `json:"code"`  // state_code
	Name  string    `json:"name"`  // 中文展示名，Go 直出，不走 i18n
	Color string    `json:"color"`
}

// StateCatalog 全部 5 个状态的固定展示元数据，数组序即固定顺序（backlog→todo→in_progress→done→cancelled）。
// 前端在 src/state/tracker/stateMeta.ts 维护同构常量（双端常量，无 catalog 接口）。
var StateCatalog = []StateMeta{
	{Code: STATE_CODE_BACKLOG, Name: "待办池", Color: "#94a3b8"},
	{Code: STATE_CODE_TODO, Name: "待办", Color: "#475569"},
	{Code: STATE_CODE_IN_PROGRESS, Name: "进行中", Color: "#f59e0b"},
	{Code: STATE_CODE_DONE, Name: "已完成", Color: "#16a34a"},
	{Code: STATE_CODE_CANCELLED, Name: "已取消", Color: "#ef4444"},
}

// FindStateMeta 按 code 查状态元数据；未命中表示非法状态码。
func FindStateMeta(code StateCode) (StateMeta, bool) {
	for _, s := range StateCatalog {
		if s.Code == code {
			return s, true
		}
	}
	return StateMeta{}, false
}
