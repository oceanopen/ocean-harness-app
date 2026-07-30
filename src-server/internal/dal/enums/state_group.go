package enums

import (
	"database/sql/driver"
	"fmt"
)

// StateGroup issue 状态分组枚举（t_project_states.state_group）。
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
