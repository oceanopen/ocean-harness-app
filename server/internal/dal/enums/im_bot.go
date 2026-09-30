package enums

import (
	"database/sql/driver"
	"fmt"
)

// Channel IM 渠道枚举（t_im_bots.channel）。bot 域为渠道无关核心设计：渠道适配器按此
// 枚举路由（internal/bot 核心不感知具体渠道，见 internal/bot/channel.go 的 Factory 注册表）。
// 取值 wecom（企业微信智能机器人）；后续 feishu 等新渠道在此追加枚举值 + 对应适配器包。
type Channel string

const (
	CHANNEL_WECOM Channel = "wecom"
)

// Value 实现 driver.Valuer：写库时校验合法值并返回底层 string；非法值返回错误，由 gorm 在 INSERT/UPDATE 时触发。
func (c Channel) Value() (driver.Value, error) {
	switch c {
	case CHANNEL_WECOM:
		return string(c), nil
	default:
		return nil, fmt.Errorf("invalid Channel: %v", c)
	}
}
