package gitutil

import "testing"

func TestRepoNameFromRemoteURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"ssh with .git", "git@github.com:oceanopen/we-claude-terminal-app.git", "we-claude-terminal-app"},
		{"ssh without .git", "git@github.com:oceanopen/repo", "repo"},
		{"https with .git", "https://github.com/oceanopen/we-claude-terminal-app.git", "we-claude-terminal-app"},
		{"https without .git", "https://github.com/oceanopen/repo", "repo"},
		{"gitlab subgroup ssh", "git@gitlab.com:group/sub/repo.git", "repo"},
		{"gitlab subgroup https", "https://gitlab.com/group/sub/repo.git", "repo"},
		{"local path bare", "/Users/x/repos/repo.git", "repo"},
		{"local path non-bare", "/Users/x/repos/my-project", "my-project"},
		{"empty", "", ""},
		{"trailing slash", "https://github.com/org/repo/", "repo"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RepoNameFromRemoteURL(c.in); got != c.want {
				t.Errorf("RepoNameFromRemoteURL(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
