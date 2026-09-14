package cli

import (
	"testing"

	"ocean-harness/server/internal/buildinfo"
)

// withMode 临时改写编译期注入的 Mode（测试结束后还原，避免污染并行包级状态）。
func withMode(t *testing.T, mode string) {
	t.Helper()
	old := buildinfo.Mode
	buildinfo.Mode = mode
	t.Cleanup(func() { buildinfo.Mode = old })
}

func TestResolvePort(t *testing.T) {
	t.Run("默认跟随构建模式", func(t *testing.T) {
		withMode(t, "test")
		if port, err := resolvePort(); err != nil || port != portTest {
			t.Fatalf("mode=test: got (%d, %v), want (%d, nil)", port, err, portTest)
		}
		withMode(t, "release")
		if port, err := resolvePort(); err != nil || port != portRelease {
			t.Fatalf("mode=release: got (%d, %v), want (%d, nil)", port, err, portRelease)
		}
	})

	t.Run("env 覆盖默认", func(t *testing.T) {
		withMode(t, "test")
		t.Setenv(EnvPort, "9200")
		if port, err := resolvePort(); err != nil || port != 9200 {
			t.Fatalf("env=9200: got (%d, %v), want (9200, nil)", port, err)
		}
	})

	t.Run("env 非空但非法报错", func(t *testing.T) {
		for _, raw := range []string{"abc", "0", "-1", "65536", " 9000x "} {
			t.Setenv(EnvPort, raw)
			if port, err := resolvePort(); err == nil {
				t.Fatalf("env=%q: got (%d, nil), want error", raw, port)
			}
		}
	})

	t.Run("env 空白视同未设置", func(t *testing.T) {
		withMode(t, "release")
		t.Setenv(EnvPort, "   ")
		if port, err := resolvePort(); err != nil || port != portRelease {
			t.Fatalf("env=blank: got (%d, %v), want (%d, nil)", port, err, portRelease)
		}
	})
}
