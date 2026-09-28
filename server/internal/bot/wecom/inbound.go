package wecom

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	aibottypes "github.com/oceanopen/wecom-aibot-go-sdk/aibot/types"
	"go.uber.org/zap"

	"ocean-harness/server/internal/bot"
	"ocean-harness/server/internal/dal/enums"
)

// quoteMaxRunes 引用文本抽取上限（rune）：引用是参考数据不是任务本体，截断防 prompt 膨胀。
const quoteMaxRunes = 8000

// submitMsg 入站构建任务的标准包装：先填公共字段（含 Route），再执行类型专属填充
// （下载等重活发生在处理通道消费 goroutine，不阻塞 ws readLoop）。
func (r *channelRuntime) submitMsg(
	headers aibottypes.WsFrameHeaders,
	base aibottypes.BaseMessage,
	fill func(inbound *bot.InboundMessage),
) {
	r.submit(func(inbound *bot.InboundMessage) {
		r.fillCommon(headers, base, inbound)
		fill(inbound)
	})
}

// fillCommon BaseMessage → InboundMessage 公共字段 + 引用抽取。
func (r *channelRuntime) fillCommon(headers aibottypes.WsFrameHeaders, base aibottypes.BaseMessage, in *bot.InboundMessage) {
	chatType := bot.ChatDirect
	convID := base.From.UserId
	if base.ChatType == "group" {
		chatType = bot.ChatGroup
		convID = base.ChatId // 群聊以 chatid 为会话粒度（全群共享一个 claude 会话）
	}
	in.MessageID = base.MsgId
	in.ChatType = chatType
	in.ConversationKey = bot.FormatConversationKey(chatType, convID)
	in.SenderID = base.From.UserId
	in.Route = bot.RouteInfo{Channel: enums.CHANNEL_WECOM, Payload: headers}
	in.Quote = extractQuote(base.Quote)
}

// extractQuote 引用内容抽取：text/voice（转写文本）直接提取；image/file/mixed 引用不做下载
// （引用是参考上下文，占位说明引导用户直接重发该附件——确定性语义，不做隐式下载魔法）。
func extractQuote(q *aibottypes.QuoteContent) *bot.QuoteRef {
	if q == nil {
		return nil
	}
	ref := &bot.QuoteRef{}
	var text string
	switch q.MsgType {
	case "text":
		text = q.Text.Content
	case "voice":
		text = "（引用了一条语音消息，转写内容）：" + q.Voice.Content
	case "image", "file", "mixed":
		text = "（引用了一条" + quoteKindName(q.MsgType) + "消息，内容未提取；如需处理该内容请直接重新发送）"
	default:
		text = "（引用了一条暂不支持提取的消息）"
	}
	ref.Text = text
	if rs := []rune(text); len(rs) > quoteMaxRunes {
		ref.Text = string(rs[:quoteMaxRunes])
		ref.Truncated = true
	}
	return ref
}

func quoteKindName(msgType string) string {
	switch msgType {
	case "image":
		return "图片"
	case "file":
		return "文件"
	case "mixed":
		return "图文"
	default:
		return msgType
	}
}

// buildWithAttachment 带附件消息（image/file）填充：同步下载解密到工作目录（URL 五分钟有效，
// 必须先落地后编排），失败不阻断正文（正文照发 + 附件缺失说明）。
func (r *channelRuntime) buildWithAttachment(in *bot.InboundMessage, url, aesKey, kind, fallbackName string) {
	if url == "" {
		return
	}
	path, name, ok := r.downloadAttachment(in.MessageID, len(in.Files)+1, url, aesKey, kind, fallbackName)
	if !ok {
		in.Text = strings.TrimSpace(in.Text + "\n（附件下载失败，未包含在本次消息中）")
		return
	}
	in.Files = append(in.Files, bot.InboundFile{Path: path, Name: name})
}

// buildMixed 图文混排：text 项拼接正文，image 项逐个下载。
func (r *channelRuntime) buildMixed(mixed aibottypes.MixedContent, in *bot.InboundMessage) {
	var texts []string
	for _, item := range mixed.MsgItem {
		switch item.MsgType {
		case "text":
			if t := strings.TrimSpace(item.Text.Content); t != "" {
				texts = append(texts, t)
			}
		case "image":
			before := len(in.Files)
			r.buildWithAttachment(in, item.Image.Url, item.Image.AesKey, "image", "image.png")
			if len(in.Files) == before {
				texts = append(texts, "（一张图片下载失败）")
			}
		}
	}
	in.Text = strings.Join(texts, "\n")
}

// downloadAttachment 下载解密并落盘到 <workspaceDir>/.wecom-attachments/<msgId>/<n>-<name>。
// 返回 (绝对路径, 展示文件名, 是否成功)。
func (r *channelRuntime) downloadAttachment(msgID string, index int, url, aesKey, kind, fallbackName string) (string, string, bool) {
	r.mu.Lock()
	client := r.client
	workspaceDir := ""
	if r.cfg.WorkspaceDir != "" {
		workspaceDir = r.cfg.WorkspaceDir
	}
	r.mu.Unlock()
	if client == nil || workspaceDir == "" {
		return "", "", false
	}

	data, filename, err := client.DownloadFile(url, aesKey)
	if err != nil {
		r.log.Warn("bot 附件下载失败", zap.String("kind", kind), zap.Error(err))
		return "", "", false
	}
	name := bot.SanitizeFileName(strings.TrimSpace(filename), fallbackName)
	dir := filepath.Join(workspaceDir, ".wecom-attachments", bot.SanitizeFileName(msgID, "msg"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		r.log.Warn("bot 附件目录创建失败", zap.Error(err))
		return "", "", false
	}
	path := filepath.Join(dir, fmt.Sprintf("%d-%s", index, name))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		r.log.Warn("bot 附件写盘失败", zap.Error(err))
		return "", "", false
	}
	return path, name, true
}
