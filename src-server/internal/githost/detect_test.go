package githost

import "testing"

func TestDetectProvider(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantKind ProviderKind
		wantOK   bool
	}{
		{"github ssh", "git@github.com:org/repo.git", PROVIDER_KIND_GITHUB, true},
		{"github https", "https://github.com/org/repo.git", PROVIDER_KIND_GITHUB, true},
		{"gitlab ssh", "git@gitlab.com:group/sub/repo.git", PROVIDER_KIND_GITLAB, true},
		{"gitlab https", "https://gitlab.com/group/repo.git", PROVIDER_KIND_GITLAB, true},
		{"self-hosted ghe", "git@ghe.company.com:org/repo.git", "", false},
		{"self-hosted gitlab", "https://gitlab.self.com/org/repo.git", "", false},
		{"empty", "", "", false},
		{"local path", "/path/to/repo.git", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := DetectProvider(c.in)
			if c.wantOK {
				if err != nil {
					t.Fatalf("DetectProvider(%q) unexpected error: %v", c.in, err)
				}
				if p.Kind() != c.wantKind {
					t.Errorf("DetectProvider(%q).Kind() = %q, want %q", c.in, p.Kind(), c.wantKind)
				}
			} else if err == nil {
				t.Errorf("DetectProvider(%q) expected error, got nil (kind=%q)", c.in, p.Kind())
			}
		})
	}
}

func TestDetectProvider_BindsOwnerRepo(t *testing.T) {
	// 验证 DetectProvider 把解析出的 host/ownerRepo 绑定到 Provider（供 1.5 展示与调用）。
	p, err := DetectProvider("git@gitlab.com:group/sub/repo.git")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Host() != "gitlab.com" || p.OwnerRepo() != "group/sub/repo" {
		t.Errorf("DetectProvider bound Host=%q OwnerRepo=%q, want gitlab.com / group/sub/repo", p.Host(), p.OwnerRepo())
	}
}
