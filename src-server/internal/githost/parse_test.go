package githost

import "testing"

func TestParseRemoteURL(t *testing.T) {
	cases := []struct {
		name          string
		in            string
		wantHost      string
		wantOwnerRepo string
		wantOK        bool
	}{
		{"ssh with .git", "git@github.com:org/repo.git", "github.com", "org/repo", true},
		{"ssh without .git", "git@github.com:org/repo", "github.com", "org/repo", true},
		{"https with .git", "https://github.com/org/repo.git", "github.com", "org/repo", true},
		{"https without .git", "https://github.com/org/repo", "github.com", "org/repo", true},
		{"gitlab subgroup ssh", "git@gitlab.com:group/sub/repo.git", "gitlab.com", "group/sub/repo", true},
		{"gitlab subgroup https", "https://gitlab.com/group/sub/repo.git", "gitlab.com", "group/sub/repo", true},
		{"gitlab deep subgroup", "git@gitlab.com:a/b/c/repo.git", "gitlab.com", "a/b/c/repo", true},
		{"https trailing slash", "https://github.com/org/repo/", "github.com", "org/repo", true},
		{"https with port", "https://gitlab.example.com:8443/group/repo.git", "gitlab.example.com:8443", "group/repo", true},
		{"empty", "", "", "", false},
		{"local path", "/Users/x/repos/repo.git", "", "", false},
		{"garbage", "not-a-url", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			host, ownerRepo, ok := ParseRemoteURL(c.in)
			if ok != c.wantOK || host != c.wantHost || ownerRepo != c.wantOwnerRepo {
				t.Errorf("ParseRemoteURL(%q) = (%q, %q, %t), want (%q, %q, %t)",
					c.in, host, ownerRepo, ok, c.wantHost, c.wantOwnerRepo, c.wantOK)
			}
		})
	}
}
