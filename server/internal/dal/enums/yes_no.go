package enums

import (
	"database/sql/driver"
	"fmt"
)

// YesNo 公共是否型映射（凡「启用/停用」之类二值字段一律用它，不自建 bool/0-1 列）。
// 取值 "Y"/"N"，与 Rust 侧 app/src/shared/types.rs 的 YesNo enum（app_config 裸字符串存储）
// 语义对齐——跨端同一套 Y/N 词汇。TEXT 列存储，typed 枚举约定（无 DB 默认值，代码显式赋值）。
type YesNo string

const (
	YES_NO_YES YesNo = "Y"
	YES_NO_NO  YesNo = "N"
)

// IsYes 判断是否为 Y。
func (y YesNo) IsYes() bool { return y == YES_NO_YES }

// Value 实现 driver.Valuer：写库时校验合法值并返回底层 string；非法值返回错误，由 gorm 在 INSERT/UPDATE 时触发。
func (y YesNo) Value() (driver.Value, error) {
	switch y {
	case YES_NO_YES, YES_NO_NO:
		return string(y), nil
	default:
		return nil, fmt.Errorf("invalid YesNo: %v", y)
	}
}
