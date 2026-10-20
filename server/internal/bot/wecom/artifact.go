package wecom

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	aibot "github.com/oceanopen/wecom-aibot-go-sdk/aibot"
	aibottypes "github.com/oceanopen/wecom-aibot-go-sdk/aibot/types"

	"ocean-harness/server/internal/bot"
)

/*
 * artifactMaxBytes 企微临时素材单文件上限 20MB。超限拒绝投递：半截文件是误导，
 * 失败交 SendNotice 交代（完整文件保留在交付目录供用户自取路径）。
 */
const artifactMaxBytes = 20 << 20

/*
 * mediaClient 媒体与附注的出站窄接缝（UploadMedia → media_id → 媒体消息两步投递）：
 * 生产实现 *aibot.WsClient；独立于 replyClient，单测按需注入 fake，不波及既有回复流测试。
 */
type mediaClient interface {
	UploadMedia(fileBuffer []byte, opts aibottypes.UploadMediaOptions) (*aibottypes.UploadMediaFinishResult, error)
	SendMediaMessage(chatid string, mediaType aibottypes.WeComMediaType, mediaId string, videoOpts *aibot.VideoOptions) (*aibottypes.WsFrame[json.RawMessage], error)
	ReplyMedia(frame aibottypes.WsFrameHeaders, mediaType aibottypes.WeComMediaType, mediaId string, videoOpts *aibot.VideoOptions) (*aibottypes.WsFrame[json.RawMessage], error)
	ReplyMarkdown(frame aibottypes.WsFrameHeaders, content string, feedback *aibottypes.ReplyFeedback) (*aibottypes.WsFrame[json.RawMessage], error)
	SendMessage(chatid string, body any) (*aibottypes.WsFrame[json.RawMessage], error)
}

/*
 * imageExts 以图片卡片投递的扩展名：企微 image 消息仅支持位图形态，
 * 其余扩展名（html/md/pdf…）一律文件卡片。
 */
var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true}

// mediaTypeForExt 文件名 → 企微素材类型。
func mediaTypeForExt(name string) aibottypes.WeComMediaType {
	if imageExts[strings.ToLower(filepath.Ext(name))] {
		return aibottypes.WeComMediaImage
	}
	return aibottypes.WeComMediaFile
}

// 编译期断言：企微回复流具备产物投递能力（编排器交付目录机制据此对 wecom 点亮）。
var _ bot.ArtifactReplyStream = (*replyStream)(nil)

/*
 * SendArtifact 实现 bot.ArtifactReplyStream：读文件 → 分片上传得 media_id → 媒体消息投递。
 * 带推送面走 aibot_send_msg 独立气泡（与卡同款「流收尾后跟进」消息序，不占回复流）；
 * 旧 headers 载荷回落 respond 通道。deliveryKey 暂不消费：编排器每文件恰好调用一次，
 * aibot_send_msg 亦无幂等键位。
 */
func (s *replyStream) SendArtifact(deliveryKey string, name string, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("产物文件不可读: %w", err)
	}
	if info.Size() > artifactMaxBytes {
		return fmt.Errorf("文件超出企微 %dMB 上限（%d 字节）", artifactMaxBytes>>20, info.Size())
	}
	if info.Size() == 0 {
		return fmt.Errorf("空文件无法投递（0 字节），请改为在回复正文中说明")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("产物文件读取失败: %w", err)
	}
	media, err := s.mediaSnapshot()
	if err != nil {
		return err
	}
	mediaType := mediaTypeForExt(name)
	up, err := media.UploadMedia(data, aibottypes.UploadMediaOptions{Type: mediaType, Filename: name})
	if err != nil {
		return fmt.Errorf("企微素材上传失败: %w", err)
	}
	if s.push != nil {
		_, err = media.SendMediaMessage(s.push.chatID, mediaType, up.MediaId, nil)
		return err
	}
	_, err = media.ReplyMedia(s.headers, mediaType, up.MediaId, nil)
	return err
}

/*
 * SendNotice 实现 bot.ArtifactReplyStream：产物链路附带说明的纯文本独立投递（markdown
 * 渲染与流一致）。带推送面走 aibot_send_msg；旧 headers 载荷回落 respond 通道单发。
 */
func (s *replyStream) SendNotice(text string) error {
	media, err := s.mediaSnapshot()
	if err != nil {
		return err
	}
	if s.push != nil {
		body := pushMarkdownBody{ChatType: s.push.chatType, MsgType: "markdown"}
		body.Markdown.Content = text
		_, err = media.SendMessage(s.push.chatID, body)
		return err
	}
	_, err = media.ReplyMarkdown(s.headers, text, nil)
	return err
}

// mediaSnapshot 媒体出站面快照（nil = 已停止/未启动；不满足 = 测试注入的窄 fake）。
func (s *replyStream) mediaSnapshot() (mediaClient, error) {
	client := s.rt.clientSnapshot()
	if client == nil {
		return nil, errors.New("企微连接已关闭，无法投递")
	}
	media, ok := client.(mediaClient)
	if !ok {
		return nil, errors.New("企微客户端不支持媒体投递")
	}
	return media, nil
}
