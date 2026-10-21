package mcpservers

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"ocean-harness/server/internal/bot"
)

/*
 * fakeDeliveryStream 投递面替身：记录产物与附言（可注入附言失败），send_file handler
 * 的断言入口。
 */
type fakeDeliveryStream struct {
	mu        sync.Mutex
	artifacts []string // "key|name|path" 拼接
	notices   []string
	noticeErr error
}

func (f *fakeDeliveryStream) SendArtifact(key, name, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.artifacts = append(f.artifacts, key+"|"+name+"|"+path)
	return nil
}

func (f *fakeDeliveryStream) SendNotice(text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.noticeErr != nil {
		return f.noticeErr
	}
	f.notices = append(f.notices, text)
	return nil
}

func (f *fakeDeliveryStream) snapshot() ([]string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.artifacts...), append([]string(nil), f.notices...)
}

/*
 * TestImSendFileHandler send_file 拒绝/执行矩阵：链路未启用、空 path、未知 token 拒绝；
 * 已登记 token 产物以基名展示 + 附言下发；附言失败不回滚文件、如实回告（防模型重发）。
 */
func TestImSendFileHandler(t *testing.T) {
	if _, _, err := imSendFileHandler("tk", imSendFileIn{Path: "/tmp/x.png"}); err == nil || !strings.Contains(err.Error(), "未启用") {
		t.Fatalf("未装配注册表应拒绝: %v", err)
	}

	reg := bot.NewDeliveryRegistry()
	SetIMDeliveryRegistry(reg)
	t.Cleanup(func() { SetIMDeliveryRegistry(nil) })

	if _, _, err := imSendFileHandler("tk", imSendFileIn{}); err == nil || !strings.Contains(err.Error(), "path") {
		t.Fatalf("空 path 应拒绝: %v", err)
	}
	if _, _, err := imSendFileHandler("ghost", imSendFileIn{Path: "/tmp/x.png"}); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("未知 token 应拒绝: %v", err)
	}

	fake := &fakeDeliveryStream{}
	reg.Register("tk", fake)
	_, out, err := imSendFileHandler("tk", imSendFileIn{Path: "/tmp/rep/output.md", Caption: "日报"})
	if err != nil || !out.OK {
		t.Fatalf("已登记 token 应成功: out=%+v err=%v", out, err)
	}
	arts, notices := fake.snapshot()
	if len(arts) != 1 || !strings.HasPrefix(arts[0], "sendfile-") ||
		!strings.HasSuffix(arts[0], "|output.md|/tmp/rep/output.md") {
		t.Fatalf("产物记录不符: %v", arts)
	}
	if len(notices) != 1 || notices[0] != "日报" {
		t.Fatalf("附言记录不符: %v", notices)
	}

	fail := &fakeDeliveryStream{noticeErr: errors.New("boom")}
	reg.Register("tk2", fail)
	_, out2, err := imSendFileHandler("tk2", imSendFileIn{Path: "/tmp/a.png", Caption: "看这个"})
	if err != nil || !out2.OK || !strings.Contains(out2.Message, "附言发送失败") {
		t.Fatalf("附言失败应回告不报错: out=%+v err=%v", out2, err)
	}
}
