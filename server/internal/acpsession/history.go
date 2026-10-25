package acpsession

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

/*
 * HistoryDirName 历史目录名：main 以 <SqliteDir>/acp-history 装配为 Manager.historyDir；
 * 域内持久化仅依赖 os + 注入目录，不感知 DB。
 */
const HistoryDirName = "acp-history"

// historyFileExt 历史文件名后缀（<conversationId>.jsonl，一行一条目快照）。
const historyFileExt = ".jsonl"

/*
 * historyRecord 历史文件单行记录：落盘时刻 + 条目快照（载荷即视图域 ConversationEntry）。
 * chunk/工具更新按同 EntryID 多行追加，回放侧按序 last-wins 折算；EntryID 已带 per-entry
 * 代次前缀（见 Manager.attachHistory），跨进程代次互不折叠。
 */
type historyRecord struct {
	RecordedAt time.Time         `json:"recordedAt"`
	Entry      ConversationEntry `json:"entry"`
}

// historyMu 域级互斥：全部历史文件写读删串行（单次 append 小，无并发热点）。
var historyMu sync.Mutex

/*
 * appendHistoryEntry 条目快照增量追加（JSONL；目录缺失自建）。持久化尽力而为：调用方
 * 只落日志，不因落盘失败阻断会话。
 */
func appendHistoryEntry(dir, conversationID string, entry ConversationEntry) error {
	historyMu.Lock()
	defer historyMu.Unlock()
	path, err := historyFilePath(dir, conversationID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建历史目录失败: %w", err)
	}
	line, err := json.Marshal(historyRecord{RecordedAt: time.Now(), Entry: entry})
	if err != nil {
		return fmt.Errorf("序列化历史记录失败: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

/*
 * loadHistoryEntries 全量读回（按文件序，不折算）。文件缺失（从未落盘）返回空不报错；
 * 崩溃残留的半行逐行解码时静默跳过。
 */
func loadHistoryEntries(dir, conversationID string) ([]historyRecord, error) {
	historyMu.Lock()
	defer historyMu.Unlock()
	path, err := historyFilePath(dir, conversationID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var records []historyRecord
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec historyRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		records = append(records, rec)
	}
	return records, nil
}

/*
 * RemoveHistoryFile 级联删除历史文件（Discard 级联与启动孤儿清理共用；不存在不报错）。
 */
func RemoveHistoryFile(dir, conversationID string) error {
	historyMu.Lock()
	defer historyMu.Unlock()
	path, err := historyFilePath(dir, conversationID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

/*
 * HistoryConversationIDs 列出历史目录已有历史的会话 id（启动孤儿清理入口：main 持会话
 * 行活集比对后逐个 RemoveHistoryFile）。目录缺失返回空。
 */
func HistoryConversationIDs(dir string) ([]string, error) {
	historyMu.Lock()
	defer historyMu.Unlock()
	des, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, de := range des {
		if de.IsDir() {
			continue
		}
		if id, ok := strings.CutSuffix(de.Name(), historyFileExt); ok && id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// historyFilePath 历史文件路径拼装；畸形会话 id（路径分隔符等）显式拒绝，不外逸目录。
func historyFilePath(dir, conversationID string) (string, error) {
	if dir == "" || conversationID == "" {
		return "", errors.New("历史目录或会话 id 为空")
	}
	if conversationID == "." || conversationID == ".." || strings.ContainsAny(conversationID, `/\`) {
		return "", fmt.Errorf("会话 id 含非法路径字符: %q", conversationID)
	}
	return filepath.Join(dir, conversationID+historyFileExt), nil
}
