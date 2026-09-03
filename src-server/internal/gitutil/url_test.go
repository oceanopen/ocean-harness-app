package gitutil

import (
	"strings"
	"testing"
)

// TestParseRemoteURL 覆盖三种 URL 形态、企业 host、大小写归一与非法输入（T4.1）。
func TestParseRemoteURL(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		want      RemoteRef
		wantErr   bool
		errSubstr string // wantErr 时错误文案应包含的片段
	}{
		// scp 风格（实际场景最常见）
		{name: "scp-github", raw: "git@github.com:octocat/Hello-World.git", want: RemoteRef{Host: "github.com", Owner: "octocat", Repo: "Hello-World"}},
		{name: "scp-no-git-suffix", raw: "git@github.com:octocat/Hello-World", want: RemoteRef{Host: "github.com", Owner: "octocat", Repo: "Hello-World"}},
		{name: "scp-enterprise", raw: "git@git.weoa.com:webank/foo-api.git", want: RemoteRef{Host: "git.weoa.com", Owner: "webank", Repo: "foo-api"}},
		{name: "scp-host-case", raw: "git@GitHub.COM:org/repo.git", want: RemoteRef{Host: "github.com", Owner: "org", Repo: "repo"}},
		// https
		{name: "https", raw: "https://github.com/octocat/Hello-World.git", want: RemoteRef{Host: "github.com", Owner: "octocat", Repo: "Hello-World"}},
		{name: "https-no-suffix-trailing-slash", raw: "https://github.com/octocat/Hello-World/", want: RemoteRef{Host: "github.com", Owner: "octocat", Repo: "Hello-World"}},
		{name: "https-enterprise", raw: "https://git.weoa.com/webank/foo-api.git", want: RemoteRef{Host: "git.weoa.com", Owner: "webank", Repo: "foo-api"}},
		{name: "https-with-user", raw: "https://user@github.com/octocat/Hello-World.git", want: RemoteRef{Host: "github.com", Owner: "octocat", Repo: "Hello-World"}},
		// ssh:// 带协议
		{name: "ssh-with-port", raw: "ssh://git@github.com:2222/octocat/Hello-World.git", want: RemoteRef{Host: "github.com", Owner: "octocat", Repo: "Hello-World"}},
		{name: "ssh-no-port", raw: "ssh://git@github.com/octocat/Hello-World.git", want: RemoteRef{Host: "github.com", Owner: "octocat", Repo: "Hello-World"}},
		// 非法输入
		{name: "empty", raw: "", wantErr: true, errSubstr: "为空"},
		{name: "no-at-no-scheme", raw: "not-a-url", wantErr: true},
		{name: "scp-missing-repo", raw: "git@github.com:onlyowner", wantErr: true, errSubstr: "owner/repo"},
		{name: "https-missing-segments", raw: "https://github.com/", wantErr: true, errSubstr: "owner/repo"},
		{name: "unsupported-scheme", raw: "file:///srv/git/repo.git", wantErr: true, errSubstr: "不支持"},
		// 残留分隔符拦截（scp 双冒号 / 嵌套 group）
		{name: "scp-with-port-colon", raw: "git@github.com:22:octocat/Hello-World.git", wantErr: true, errSubstr: "不支持的路径形态"},
		{name: "nested-group", raw: "git@gitlab.com:group/sub/repo.git", wantErr: true, errSubstr: "不支持的路径形态"},
		{name: "spaces-trimmed", raw: "  git@github.com:octocat/Hello-World.git  ", want: RemoteRef{Host: "github.com", Owner: "octocat", Repo: "Hello-World"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRemoteURL(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际解析成功: %+v", got)
				}
				if tc.errSubstr != "" && !strings.Contains(err.Error(), tc.errSubstr) {
					t.Fatalf("错误文案 %q 未包含期望片段 %q", err.Error(), tc.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("意外报错: %v", err)
			}
			if got != tc.want {
				t.Fatalf("解析结果 %+v，期望 %+v", got, tc.want)
			}
		})
	}
}
