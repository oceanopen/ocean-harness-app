package clibin

import "testing"

func TestLastEnvPath(t *testing.T) {
	cases := []struct {
		name   string
		stdout string
		want   string
	}{
		{
			name:   "env 输出取最后一条 PATH",
			stdout: "OCEAN_CLIBIN_PROBE_OPEN\n/usr/local/bin/claude\nOCEAN_CLIBIN_PROBE_CLOSE\nPATH=/usr/bin:/bin\nHOME=/Users/x\n",
			want:   "/usr/bin:/bin",
		},
		{
			name:   "rc 噪声 PATH 被末尾 env 覆盖",
			stdout: "PATH=/stale/bin\nOCEAN_CLIBIN_PROBE_OPEN\n/opt/homebrew/bin/claude\nOCEAN_CLIBIN_PROBE_CLOSE\nPATH=/real/bin:/opt/bin\n",
			want:   "/real/bin:/opt/bin",
		},
		{
			name:   "行尾 CR 被去除",
			stdout: "PATH=/usr/bin:/bin\r\n",
			want:   "/usr/bin:/bin",
		},
		{
			name:   "无 PATH 行返回空",
			stdout: "HOME=/Users/x\nSHELL=/bin/zsh\n",
			want:   "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := lastEnvPath(c.stdout); got != c.want {
				t.Fatalf("lastEnvPath = %q, want %q", got, c.want)
			}
		})
	}
}
