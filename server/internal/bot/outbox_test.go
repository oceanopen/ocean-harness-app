package bot

import (
	"path/filepath"
	"testing"
)

/*
 * TestInboundFilePath 入站附件落盘路径：msgId 前缀 + 文件名净化（Unicode 保留、hostile
 * 剔除、首部 '_-' 去除、全非法回落）+ 同消息重名追序号。
 */
func TestInboundFilePath(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		label, raw, fallback, want string
	}{
		{"中文名保留", "需求文档.pdf", "file.bin", "需求文档.pdf"},
		{"首部下划线去除", "_4ce58485-a.png", "image.png", "4ce58485-a.png"},
		{"路径字符剔除", "../evil.txt", "file.bin", "evil.txt"},
		{"尾部点去除", "file..", "file.bin", "file"},
		{"全非法回落", `/\:*?"<>|`, "file.bin", "file.bin"},
	}
	for _, c := range cases {
		path, name, err := InboundFilePath(root, "m1abc23de", c.raw, c.fallback, nil)
		if err != nil {
			t.Fatalf("%s: %v", c.label, err)
		}
		if name != c.want {
			t.Fatalf("%s: name = %q, want %q", c.label, name, c.want)
		}
		wantPath := filepath.Join(root, ".bot-outbox", "download", "m1abc23d-"+c.want)
		if path != wantPath {
			t.Fatalf("%s: path = %q, want %q", c.label, path, wantPath)
		}
	}

	// 同消息重名追序号（跨消息由 msgId 前缀隔离）。
	_, n1, err := InboundFilePath(root, "m1abc23de", "a.txt", "file.bin", nil)
	if err != nil || n1 != "a.txt" {
		t.Fatalf("首个附件: name=%q err=%v", n1, err)
	}
	_, n2, err := InboundFilePath(root, "m1abc23de", "a.txt", "file.bin", []InboundFile{{Name: n1}})
	if err != nil || n2 != "a-2.txt" {
		t.Fatalf("重名追序号: name=%q err=%v", n2, err)
	}
}
