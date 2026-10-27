package weixin

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"ocean-harness/server/internal/bot"
	"ocean-harness/server/internal/dal/enums"
)

// quoteTextMaxRunes 引用文本截断上限。
const quoteTextMaxRunes = 2000

// replyRoute 回复路由载荷（OpenReply 依据；context_token 为协议会话亲和标识，原样回传）。
type replyRoute struct {
	ToUserID     string
	ContextToken string
}

/*
 * normalizeInbound iLink 消息 → 核心规范化模型。仅私聊（协议无群聊入站）；附件同步下载落盘
 * （CDN 参数有时效，对齐企微五分钟 URL 约束），下载/解密失败降级为无附件文本（不吞用户消息）。
 */
func (r *channelRuntime) normalizeInbound(cg connGen, m *inboundMessage) bot.InboundMessage {
	if m.MessageType == 2 {
		return bot.InboundMessage{} // 自发消息回声，丢弃（无幂等键即被消费侧滤掉）
	}
	out := bot.InboundMessage{
		MessageID:       inboundMessageID(m),
		ConversationKey: bot.FormatConversationKey(bot.ChatDirect, m.FromUserID),
		ChatType:        bot.ChatDirect,
		SenderID:        m.FromUserID,
		Route: bot.RouteInfo{
			Channel: enums.CHANNEL_WEIXIN,
			Payload: replyRoute{ToUserID: m.FromUserID, ContextToken: m.ContextToken},
		},
	}
	for i := range m.ItemList {
		item := &m.ItemList[i]
		switch {
		case item.Type == 1 && item.TextItem != nil && strings.TrimSpace(item.TextItem.Text) != "":
			out.Text = strings.TrimSpace(item.TextItem.Text)
		case item.Type == 3 && item.VoiceItem != nil && strings.TrimSpace(item.VoiceItem.Text) != "":
			out.Text = strings.TrimSpace(item.VoiceItem.Text) // 语音转写文本直接作正文（协议无裸语音下载）
		case item.Type == 2 && item.ImageItem != nil:
			r.stageAttachment(cg, &out, item.ImageItem.AESKey, item.ImageItem.Media, "image.png", "image.png")
		case item.Type == 4 && item.FileItem != nil:
			r.stageAttachment(cg, &out, item.FileItem.AESKey, item.FileItem.Media,
				attachmentName(item.FileItem.FileName, "file.bin"), "file.bin")
		case item.Type == 5 && item.VideoItem != nil:
			r.stageAttachment(cg, &out, item.VideoItem.AESKey, item.VideoItem.Media, "video.mp4", "video.mp4")
		}
		if item.RefMsg != nil {
			out.Quote = quoteFromRef(item.RefMsg)
		}
	}
	return out
}

// inboundMessageID 幂等键：message_id 优先（大整数原文字符串），client_id 兜底。
func inboundMessageID(m *inboundMessage) string {
	if id := m.MessageID.String(); id != "" {
		return id
	}
	return m.ClientID
}

/*
 * quoteFromRef 引用快照 → QuoteRef（作者 id 协议未提供，留空；文本取被引消息正文，
 * 缺失回落 title；引用附件不回溯下载，仅保留文本语义）。
 */
func quoteFromRef(ref *inboundRefMsg) *bot.QuoteRef {
	text := ""
	if ref.MessageItem != nil {
		if ref.MessageItem.TextItem != nil {
			text = ref.MessageItem.TextItem.Text
		} else if ref.MessageItem.VoiceItem != nil {
			text = ref.MessageItem.VoiceItem.Text
		}
	}
	if strings.TrimSpace(text) == "" {
		text = ref.Title
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	runes := []rune(trimmed)
	if len(runes) > quoteTextMaxRunes {
		return &bot.QuoteRef{Text: string(runes[:quoteTextMaxRunes]), Truncated: true}
	}
	return &bot.QuoteRef{Text: trimmed}
}

// attachmentName 附件名兜底与路径穿越防御（file_name 为远端可控字符串，只取基名）。
func attachmentName(raw, fallback string) string {
	name := filepath.Base(strings.TrimSpace(raw))
	if name == "" || name == "." || name == ".." || name == "/" {
		return fallback
	}
	return name
}

/*
 * stageAttachment 下载解密落盘（<workspaceDir>/.bot-outbox/download/<msgId前8>-<name>）并追加到
 * Files。任一步失败仅告警并降级为无附件——媒体丢失不应阻塞文本主链路。
 */
func (r *channelRuntime) stageAttachment(cg connGen, out *bot.InboundMessage, keyDirect string, media inboundMedia, rawName, fallback string) {
	if cg.cfg.WorkspaceDir == "" || out.MessageID == "" {
		return
	}
	key, err := parseMediaAESKey(keyDirect, media.AESKey)
	if err != nil {
		cg.log.Warn("微信附件密钥无效，跳过下载", zap.String("msgID", out.MessageID), zap.Error(err))
		return
	}
	downloadURL, err := cdnDownloadURL(media.EncryptQueryParam, media.FullURL)
	if err != nil {
		cg.log.Warn("微信附件下载地址无效，跳过下载", zap.String("msgID", out.MessageID), zap.Error(err))
		return
	}
	ciphertext, err := cg.client.DownloadCdn(context.Background(), downloadURL)
	if err != nil {
		cg.log.Warn("微信附件下载失败，降级为无附件", zap.String("msgID", out.MessageID), zap.Error(err))
		return
	}
	plain, err := aesECBDecrypt(ciphertext, key)
	if err != nil {
		cg.log.Warn("微信附件解密失败，降级为无附件", zap.String("msgID", out.MessageID), zap.Error(err))
		return
	}
	dest, name, err := bot.InboundFilePath(cg.cfg.WorkspaceDir, out.MessageID, rawName, fallback, out.Files)
	if err != nil {
		cg.log.Warn("微信附件目录创建失败，降级为无附件", zap.String("msgID", out.MessageID), zap.Error(err))
		return
	}
	if err := os.WriteFile(dest, plain, 0o644); err != nil {
		cg.log.Warn("微信附件落盘失败，降级为无附件", zap.Error(err))
		return
	}
	out.Files = append(out.Files, bot.InboundFile{Path: dest, Name: name})
}
