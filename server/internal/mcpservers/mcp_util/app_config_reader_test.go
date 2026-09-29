package mcputil

import (
	"testing"

	"ocean-harness/server/internal/config"
	"ocean-harness/server/internal/global"
)

func TestReadGithubPATNotConfigured(t *testing.T) {
	origConfig := global.Config
	t.Cleanup(func() { global.Config = origConfig })

	t.Run("AppDbPath 未注入", func(t *testing.T) {
		global.Config = &config.Config{}
		if _, err := ReadGithubPAT(); err == nil {
			t.Fatal("want error, got nil")
		}
	})
}
