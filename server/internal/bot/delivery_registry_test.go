package bot

import (
	"testing"
	"time"
)

// TestDeliveryRegistryLifecycle 登记/覆盖/注销矩阵：未登记不命中、同 token 覆盖、不在场注销静默。
func TestDeliveryRegistryLifecycle(t *testing.T) {
	r := NewDeliveryRegistry()
	if _, ok := r.Lookup("t1"); ok {
		t.Fatal("空注册表不应命中")
	}
	a, b := &fakeArtifactReplyStream{}, &fakeArtifactReplyStream{}
	r.Register("t1", a)
	if got, ok := r.Lookup("t1"); !ok || got != ArtifactReplyStream(a) {
		t.Fatalf("注册后应命中原条目: %v %v", got, ok)
	}
	r.Register("t1", b)
	if got, ok := r.Lookup("t1"); !ok || got != ArtifactReplyStream(b) {
		t.Fatalf("同 token 应覆盖: %v %v", got, ok)
	}
	r.Unregister("missing")
	r.Unregister("t1")
	if _, ok := r.Lookup("t1"); ok {
		t.Fatal("注销后不应命中")
	}
}

// TestDeliveryRegistryLazyExpiry 过期条目懒清扫：Lookup 拒绝并顺带删除。
func TestDeliveryRegistryLazyExpiry(t *testing.T) {
	r := NewDeliveryRegistry()
	r.Register("t1", &fakeArtifactReplyStream{})
	r.mu.Lock()
	r.byToken["t1"] = deliveryEntry{ars: &fakeArtifactReplyStream{}, deadline: time.Now().Add(-time.Second)}
	r.mu.Unlock()
	if _, ok := r.Lookup("t1"); ok {
		t.Fatal("过期条目应被拒绝")
	}
	r.mu.Lock()
	_, exists := r.byToken["t1"]
	r.mu.Unlock()
	if exists {
		t.Fatal("过期条目应被顺带删除")
	}
}
