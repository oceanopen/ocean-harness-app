package wecom

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	aibot "github.com/oceanopen/wecom-aibot-go-sdk/aibot"
	aibottypes "github.com/oceanopen/wecom-aibot-go-sdk/aibot/types"
)

/*
 * fakeMediaClient mediaClient 接缝的测试替身：记录上传参数与媒体出站调用，可对上传注错。
 * 内嵌既有 replyClient fake 使其同时可注入 replyOverride。
 */
type fakeMediaClient struct {
	*fakeReplyClient
	upOpts  aibottypes.UploadMediaOptions
	upErr   error
	callsJS []string // 媒体出站序：send:<chatid>:<type>:<mediaId> / reply:<type>:<mediaId> / md:<text> / sendmd:<chatid>:<chatType>:<text>
}

func (f *fakeMediaClient) UploadMedia(fileBuffer []byte, opts aibottypes.UploadMediaOptions) (*aibottypes.UploadMediaFinishResult, error) {
	f.upOpts = opts
	if f.upErr != nil {
		return nil, f.upErr
	}
	return &aibottypes.UploadMediaFinishResult{Type: opts.Type, MediaId: "MEDIA-1"}, nil
}

func (f *fakeMediaClient) SendMediaMessage(chatid string, mediaType aibottypes.WeComMediaType, mediaId string, videoOpts *aibot.VideoOptions) (*aibottypes.WsFrame[json.RawMessage], error) {
	f.callsJS = append(f.callsJS, fmt.Sprintf("send:%s:%s:%s", chatid, mediaType, mediaId))
	return nil, nil
}

func (f *fakeMediaClient) ReplyMedia(frame aibottypes.WsFrameHeaders, mediaType aibottypes.WeComMediaType, mediaId string, videoOpts *aibot.VideoOptions) (*aibottypes.WsFrame[json.RawMessage], error) {
	f.callsJS = append(f.callsJS, fmt.Sprintf("reply:%s:%s", mediaType, mediaId))
	return nil, nil
}

func (f *fakeMediaClient) ReplyMarkdown(frame aibottypes.WsFrameHeaders, content string, feedback *aibottypes.ReplyFeedback) (*aibottypes.WsFrame[json.RawMessage], error) {
	f.callsJS = append(f.callsJS, "md:"+content)
	return nil, nil
}

func (f *fakeMediaClient) SendMessage(chatid string, body any) (*aibottypes.WsFrame[json.RawMessage], error) {
	if b, ok := body.(pushMarkdownBody); ok {
		f.callsJS = append(f.callsJS, fmt.Sprintf("sendmd:%s:%d:%s", chatid, b.ChatType, b.Markdown.Content))
		return nil, nil
	}
	return f.fakeReplyClient.SendMessage(chatid, body)
}

// newArtifactTestStream 经接缝注入 fake 的 replyStream（产物投递域测试装配；push 可 nil）。
func newArtifactTestStream(fake *fakeMediaClient, push *pushTarget) *replyStream {
	return &replyStream{rt: &channelRuntime{replyOverride: fake}, headers: aibottypes.WsFrameHeaders{ReqId: "req-t"}, streamId: "st-t", push: push}
}

/*
 * TestSendArtifactMediaRouting 扩展名 → 素材类型映射与出站通道：png/jpg（含大写扩展名）
 * 走图片卡片，md/html 走文件卡片；带推送面恒走 aibot_send_msg 主动推送。
 */
func TestSendArtifactMediaRouting(t *testing.T) {
	dir := t.TempDir()
	write := func(name string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
			t.Fatalf("测试文件写入失败: %v", err)
		}
		return path
	}
	cases := []struct{ name, wantType string }{
		{"架构图.png", "image"},
		{"照片.JPG", "image"},
		{"总结.md", "file"},
		{"页面.html", "file"},
	}
	for _, tc := range cases {
		fake := &fakeMediaClient{fakeReplyClient: &fakeReplyClient{}}
		s := newArtifactTestStream(fake, &pushTarget{chatID: "chat-1", chatType: 1})
		if err := s.SendArtifact("n/"+tc.name, tc.name, write(tc.name)); err != nil {
			t.Fatalf("%s 投递失败: %v", tc.name, err)
		}
		want := "send:chat-1:" + tc.wantType + ":MEDIA-1"
		if len(fake.callsJS) != 1 || fake.callsJS[0] != want {
			t.Fatalf("%s 出站不符: got %v want %q", tc.name, fake.callsJS, want)
		}
		if fake.upOpts.Filename != tc.name || string(fake.upOpts.Type) != tc.wantType {
			t.Fatalf("%s 上传参数不符: %+v", tc.name, fake.upOpts)
		}
	}
}

// TestSendArtifactLegacyReplyMedia 无推送面（旧 headers 载荷）回落 respond 通道媒体消息。
func TestSendArtifactLegacyReplyMedia(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.html")
	if err := os.WriteFile(path, []byte("<p>hi</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &fakeMediaClient{fakeReplyClient: &fakeReplyClient{}}
	s := newArtifactTestStream(fake, nil)
	if err := s.SendArtifact("n/a.html", "a.html", path); err != nil {
		t.Fatalf("投递失败: %v", err)
	}
	if len(fake.callsJS) != 1 || fake.callsJS[0] != "reply:file:MEDIA-1" {
		t.Fatalf("出站不符: %v", fake.callsJS)
	}
}

// TestSendArtifactGuards 守卫路径：超上限拒绝且不发起上传、文件缺失报错、上传错误透传。
func TestSendArtifactGuards(t *testing.T) {
	dir := t.TempDir()

	big := filepath.Join(dir, "big.bin")
	if err := os.WriteFile(big, make([]byte, artifactMaxBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &fakeMediaClient{fakeReplyClient: &fakeReplyClient{}}
	s := newArtifactTestStream(fake, &pushTarget{chatID: "c", chatType: 1})
	if err := s.SendArtifact("n/big.bin", "big.bin", big); err == nil {
		t.Fatal("超上限文件应拒绝投递")
	}
	if len(fake.callsJS) != 0 {
		t.Fatalf("超上限不得发起上传: %v", fake.callsJS)
	}

	if err := s.SendArtifact("n/none.txt", "none.txt", filepath.Join(dir, "none.txt")); err == nil {
		t.Fatal("缺失文件应报错")
	}

	small := filepath.Join(dir, "a.md")
	if err := os.WriteFile(small, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake2 := &fakeMediaClient{fakeReplyClient: &fakeReplyClient{}, upErr: errors.New("boom")}
	s2 := newArtifactTestStream(fake2, &pushTarget{chatID: "c", chatType: 1})
	if err := s2.SendArtifact("n/a.md", "a.md", small); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("上传错误应透传: %v", err)
	}
}

// TestSendArtifactMediaUnsupported 出站面不满足媒体能力时显式报错（不静默吞投递）。
func TestSendArtifactMediaUnsupported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.md")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &replyStream{
		rt:       &channelRuntime{replyOverride: &fakeReplyClient{}},
		headers:  aibottypes.WsFrameHeaders{ReqId: "req-t"},
		streamId: "st-t",
		push:     &pushTarget{chatID: "c", chatType: 1},
	}
	if err := s.SendArtifact("n/a.md", "a.md", path); err == nil {
		t.Fatal("媒体能力缺失应报错")
	}
}

// TestSendNotice 附带说明：推送面走 markdown 独立气泡，旧 headers 载荷回落 respond 单发。
func TestSendNotice(t *testing.T) {
	fake := &fakeMediaClient{fakeReplyClient: &fakeReplyClient{}}
	s := newArtifactTestStream(fake, &pushTarget{chatID: "chat-9", chatType: 2})
	if err := s.SendNotice("文件「a」投递失败：boom"); err != nil {
		t.Fatalf("SendNotice 失败: %v", err)
	}
	want := "sendmd:chat-9:2:文件「a」投递失败：boom"
	if len(fake.callsJS) != 1 || fake.callsJS[0] != want {
		t.Fatalf("推送面出站不符: got %v want %q", fake.callsJS, want)
	}

	fake2 := &fakeMediaClient{fakeReplyClient: &fakeReplyClient{}}
	s2 := newArtifactTestStream(fake2, nil)
	if err := s2.SendNotice("提示"); err != nil {
		t.Fatalf("SendNotice 失败: %v", err)
	}
	if len(fake2.callsJS) != 1 || fake2.callsJS[0] != "md:提示" {
		t.Fatalf("回落出站不符: %v", fake2.callsJS)
	}
}
