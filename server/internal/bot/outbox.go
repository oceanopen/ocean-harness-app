package bot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

/*
 * outboxDirName bot 会话工作目录下的固定落盘区：出站 overflow/delivery 与入站 download
 * 三类用途统一收口（渠道无关，核心与适配器共用）。
 */
const outboxDirName = ".bot-outbox"

/*
 * InboundFilePath 入站附件落盘路径与展示名：路径 <workspaceDir>/.bot-outbox/download/<msgId前8>-
 * <文件名>，展示名为净化后文件名（不含 msgId 前缀）。文件名净化 + 同消息重名追序号；目录懒建。
 */
func InboundFilePath(workspaceDir, msgID, rawName, fallback string, existing []InboundFile) (string, string, error) {
	name := uniqueFileName(existing, SanitizeFileName(strings.TrimSpace(rawName), fallback))
	dir := filepath.Join(workspaceDir, outboxDirName, "download")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	return filepath.Join(dir, shortID(msgID)+"-"+name), name, nil
}

/*
 * uniqueFileName 同消息内同名附件追加序号（image.png → image-2.png），避免相互覆盖。
 */
func uniqueFileName(existing []InboundFile, name string) string {
	taken := make(map[string]bool, len(existing))
	for _, f := range existing {
		taken[f.Name] = true
	}
	if !taken[name] {
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d%s", base, i, ext)
		if !taken[candidate] {
			return candidate
		}
	}
}
