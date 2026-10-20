package bot

import (
	"sync"
	"time"
)

/*
 * DeliveryRegistry 会话内文件投递注册表：回合受理时登记 token → 产物投递面，回合收尾注销。
 * MCP im 投递端点经 token 取回投递面发起推送——通道知识（chatID/素材上传）留在渠道
 * replyStream 内，注册表只认 ArtifactReplyStream 接口。单实例进程级共享（装配根注入
 * 编排器与 MCP 挂载两侧）。
 */
type DeliveryRegistry struct {
	mu      sync.Mutex
	byToken map[string]deliveryEntry
}

type deliveryEntry struct {
	ars      ArtifactReplyStream
	deadline time.Time // 注册时刻 + TTL；懒清扫兜底（正常路径回合收尾即注销）
}

// deliveryEntryTTL 注册条目兜底 TTL（注销丢失时的资源回收上界）。
const deliveryEntryTTL = 2 * time.Hour

func NewDeliveryRegistry() *DeliveryRegistry {
	return &DeliveryRegistry{byToken: make(map[string]deliveryEntry)}
}

// Register 登记投递面（同 token 覆盖）。
func (r *DeliveryRegistry) Register(token string, ars ArtifactReplyStream) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byToken[token] = deliveryEntry{ars: ars, deadline: time.Now().Add(deliveryEntryTTL)}
}

// Unregister 注销（回合收尾/Stop 统一清理；不在场静默）。
func (r *DeliveryRegistry) Unregister(token string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byToken, token)
}

// Lookup 取投递面（顺带懒清扫过期条目）。不存在/已过期返回 false。
func (r *DeliveryRegistry) Lookup(token string) (ArtifactReplyStream, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.byToken[token]
	if !ok {
		return nil, false
	}
	if time.Now().After(e.deadline) {
		delete(r.byToken, token)
		return nil, false
	}
	return e.ars, true
}
