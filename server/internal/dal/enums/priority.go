package enums

import (
	"database/sql/driver"
	"fmt"
)

// Priority issue 优先级枚举（t_project_issues.priority）。取值 urgent/high/medium/low/none。
type Priority string

const (
	PRIORITY_URGENT Priority = "urgent"
	PRIORITY_HIGH   Priority = "high"
	PRIORITY_MEDIUM Priority = "medium"
	PRIORITY_LOW    Priority = "low"
	PRIORITY_NONE   Priority = "none"
)

// Value 实现 driver.Valuer：写库时校验合法值并返回底层 string；非法值返回错误，由 gorm 在 INSERT/UPDATE 时触发。
func (p Priority) Value() (driver.Value, error) {
	switch p {
	case
		PRIORITY_URGENT,
		PRIORITY_HIGH,
		PRIORITY_MEDIUM,
		PRIORITY_LOW,
		PRIORITY_NONE:
		return string(p), nil
	default:
		return nil, fmt.Errorf("invalid Priority: %v", p)
	}
}
