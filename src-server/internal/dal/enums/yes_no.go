package enums

import (
	"database/sql/driver"
	"fmt"
)

// YesNo 布尔语义的 Y/N 枚举，替代 bool 用于 is_default / is_triage / is_draft 等字段。
// 用因：bool 字段前端易把字符串 "true"/"false" 当布尔传，导致 JSON 解析异常；统一 "Y"/"N" 字符串契约可消除歧义。
type YesNo string

const (
	YES_NO_Y YesNo = "Y" // 是 / true
	YES_NO_N YesNo = "N" // 否 / false
)

// Value 实现 driver.Valuer：写库时校验合法值并返回底层 string；非法值返回错误，由 gorm 在 INSERT/UPDATE 时触发。
func (y YesNo) Value() (driver.Value, error) {
	switch y {
	case
		YES_NO_Y,
		YES_NO_N:
		return string(y), nil
	default:
		return nil, fmt.Errorf("invalid YesNo: %v", y)
	}
}
