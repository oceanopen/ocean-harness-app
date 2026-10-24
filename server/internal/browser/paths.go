package browser

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

/*
 * 目录布局 SSOT（D2/D3/D10/T7）：profiles/<name>/（per-profile Chrome user-data-dir，登录态活 SSOT，Chromium 自管；内含 prefs.json 非凭据偏好 D10、.engine.pid pid 锚点）、downloads/<name>/（引擎 --output-dir，下载与产物落点）、auth-states/default.json（全局共享登录态，T7.2：登录一次全局共用，凭据仅此一份落盘，不进数据库）。
 * prefs.json / .engine.pid 落在 user-data-dir 根部：Chrome 忽略未知文件，无冲突。
 */

var (
	pathsMu       sync.RWMutex
	pathsRootOver string // 测试注入的根目录覆盖（空 = 缺省 ~/.ocean-harness/browser）
)

// SetRoot 覆盖 browser 根目录（测试专用；空串恢复缺省）。
func SetRoot(root string) {
	pathsMu.Lock()
	defer pathsMu.Unlock()
	pathsRootOver = root
}

// Root 返回 browser 根目录（可覆盖点，见 SetRoot）。
func Root() string {
	pathsMu.RLock()
	over := pathsRootOver
	pathsMu.RUnlock()
	if over != "" {
		return over
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".ocean-harness", "browser") // 取不到 home 的降级（相对路径，调用期报错可见）
	}
	return filepath.Join(home, ".ocean-harness", "browser")
}

/*
 * profileNameRe profile 名合法性：字母数字开头，仅限字母数字与 - _，长度 1-128。
 * 防路径穿越（"/"、".."、前导点均被拒；名字不含点，Windows 保留设备名另行整名拒绝）；
 * 上限容纳会话维度双 uuid 名（issue-<uuid>_session-<uuid>-NN ≈ 90）。
 */
var profileNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)

/*
 * windowsReservedNames Windows 保留设备名（con/nul/com1-9/lpt1-9；正则已禁点，扩展名
 * 形态如 con.x 不可能命中，整名匹配即够）——放行会导致 MkdirAll/WriteFile 行为异常。
 */
var windowsReservedNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
}

func init() {
	for i := 1; i <= 9; i++ {
		windowsReservedNames[fmt.Sprintf("com%d", i)] = true
		windowsReservedNames[fmt.Sprintf("lpt%d", i)] = true
	}
}

// ValidateProfileName 校验 profile 名（所有目录推导的前置门槛）。
func ValidateProfileName(name string) error {
	if !profileNameRe.MatchString(name) {
		return fmt.Errorf("profile 名 %q 非法：需以字母数字开头，仅含字母数字与 - _，长度 1-128", name)
	}
	if windowsReservedNames[strings.ToLower(name)] {
		return fmt.Errorf("profile 名 %q 非法：与 Windows 保留设备名冲突，请换名", name)
	}
	return nil
}

/*
 * profile 命名空间前缀与会话段分隔（前后端各单点 SSOT，前端 derive.ts 同规则，D11）：
 * <前缀><ownerId>_session-<conversationId>-NN 会话维度派生；issue- 任务 / workspace-
 * 工作空间两族，后缀两位数字显式存在（无裸基名形态）。
 */
const (
	TaskProfilePrefix      = "issue-"
	WorkspaceProfilePrefix = "workspace-"
	SessionSegment         = "_session-"
)

/*
 * 会话归属型别取值域（FormatConversationProfile 的 ownerType 入参；与前端 derive.ts
 * ownerKind 同域）。
 */
const (
	ConversationOwnerTask      = "issue"
	ConversationOwnerWorkspace = "workspace"
)

// conversationPrefix 归属型别 → profile 前缀映射（未知型别 false）。
func conversationPrefix(ownerType string) (string, bool) {
	switch ownerType {
	case ConversationOwnerTask:
		return TaskProfilePrefix, true
	case ConversationOwnerWorkspace:
		return WorkspaceProfilePrefix, true
	}
	return "", false
}

// MaxSessionsPerConversation 单会话浏览器实例槽位数（-01..-10 两位后缀）。
const MaxSessionsPerConversation = 10

/*
 * FormatConversationProfile 会话第 slot 个实例的 profile 名（-01 起两位后缀显式存在）。
 * slot 越界或型别未知显式报错；拼接后的最终名过 ValidateProfileName 校验（超长 id 在此拦下）。
 */
func FormatConversationProfile(ownerType, ownerID, conversationID string, slot int) (string, error) {
	if slot < 1 || slot > MaxSessionsPerConversation {
		return "", fmt.Errorf("会话槽位 %d 非法（1-%d）", slot, MaxSessionsPerConversation)
	}
	prefix, ok := conversationPrefix(ownerType)
	if !ok {
		return "", fmt.Errorf("归属型别 %q 非法（issue/workspace）", ownerType)
	}
	name := fmt.Sprintf("%s%s%s%s-%02d", prefix, ownerID, SessionSegment, conversationID, slot)
	if err := ValidateProfileName(name); err != nil {
		return "", err
	}
	return name, nil
}

/*
 * ParseConversationProfile 解析会话命名空间 profile（前后端同规则的解析端）：`_session-`
 * 右侧为「conversationId-NN」，尾两位数字（01-MaxSessionsPerConversation）识别为槽位后缀，
 * 越界两位尾数（-00/-11…）整体判非会话实例；前段须命中命名空间前缀且 ownerID 非空。
 */
func ParseConversationProfile(profile string) (ownerPrefix, ownerID, conversationID string, slot int, ok bool) {
	seg := strings.LastIndex(profile, SessionSegment)
	if seg < 0 {
		return "", "", "", 0, false
	}
	head, tail := profile[:seg], profile[seg+len(SessionSegment):]
	if len(tail) < 3 || tail[len(tail)-3] != '-' {
		return "", "", "", 0, false
	}
	digits := tail[len(tail)-2:]
	if !isASCIIDigit(digits[0]) || !isASCIIDigit(digits[1]) {
		return "", "", "", 0, false
	}
	slot = int(digits[0]-'0')*10 + int(digits[1]-'0')
	if slot < 1 || slot > MaxSessionsPerConversation {
		return "", "", "", 0, false
	}
	if ownerPrefix, ownerID, ok = splitProfileNamespace(head); !ok {
		return "", "", "", 0, false
	}
	return ownerPrefix, ownerID, tail[:len(tail)-3], slot, true
}

// splitProfileNamespace 前缀识别：命中 issue-/workspace- 且余量非空。
func splitProfileNamespace(head string) (prefix, id string, ok bool) {
	if id, hit := strings.CutPrefix(head, TaskProfilePrefix); hit && id != "" {
		return TaskProfilePrefix, id, true
	}
	if id, hit := strings.CutPrefix(head, WorkspaceProfilePrefix); hit && id != "" {
		return WorkspaceProfilePrefix, id, true
	}
	return "", "", false
}

// isASCIIDigit ASCII 数字判定（解析热路径免 regex）。
func isASCIIDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

/*
 * conversationQuotaBase 会话配额归组键（单会话并发上限用，宽于 ParseConversationProfile）：
 * `_session-` 右侧尾随「连字符+两位数字」一律剥掉后缀归基座——越界槽位名（-00/-11/-99…）
 * 与目标会话同配额，杜绝借名绕过；缺段或空 id 非会话实例 ok=false。
 */
func conversationQuotaBase(profile string) (string, bool) {
	seg := strings.LastIndex(profile, SessionSegment)
	if seg < 0 {
		return "", false
	}
	head, tail := profile[:seg], profile[seg+len(SessionSegment):]
	prefix, id, ok := splitProfileNamespace(head)
	if !ok || tail == "" {
		return "", false
	}
	if n := len(tail); n >= 3 && tail[n-3] == '-' && isASCIIDigit(tail[n-2]) && isASCIIDigit(tail[n-1]) {
		tail = tail[:n-3]
	}
	if tail == "" {
		return "", false
	}
	return prefix + id + SessionSegment + tail, true
}

/*
 * ProfileDir 返回 per-profile Chrome user-data-dir（profiles/<name>/）。只推导不创建——
 * 目录由 Chrome 首次启动自建（user-data-dir 语义）。
 */
func ProfileDir(name string) (string, error) {
	if err := ValidateProfileName(name); err != nil {
		return "", err
	}
	return filepath.Join(Root(), "profiles", name), nil
}

/*
 * DownloadsDir 返回 per-profile下载/产物落点（downloads/<name>/），并确保目录存在
 * （引擎 --output-dir 的前置；spike 实锤缺省会落 cwd/.playwright-mcp，故显式传值）。
 */
func DownloadsDir(name string) (string, error) {
	if err := ValidateProfileName(name); err != nil {
		return "", err
	}
	dir := filepath.Join(Root(), "downloads", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建 profile 下载目录: %w", err)
	}
	return dir, nil
}

/*
 * AuthStatePath 全局共享登录态文件（auth-states/default.json，Playwright storageState
 * 格式；T7.2 登录一次全局共用）。只推导不创建——目录与文件归写入方（原子写）负责。
 */
func AuthStatePath() string {
	return filepath.Join(Root(), "auth-states", "default.json")
}

/*
 * ProfilePrefs per-profile 偏好元数据（非凭据，不违 D3——D3 约束的是登录态凭据不落盘）。
 * 缺省无头（D10 修订：D9 面板实时画面下人工登录/介入均在面板内完成，桌面零弹窗）；
 * 有头为显式选项，结构体零值语义不承载缺省——缺省判定在 ReadProfilePrefs。
 */
type ProfilePrefs struct {
	Headless bool `json:"headless"`
}

// prefsFile 偏好文件名（user-data-dir 根部，Chrome 忽略未知文件）。
const prefsFile = "prefs.json"

/*
 * ReadProfilePrefs 读取 profile 偏好（文件缺失 = 无头缺省，即新建 profile 缺省口径
 * D10 修订；已落盘 prefs 的既有 profile 行为不变）。
 */
func ReadProfilePrefs(profileDir string) (ProfilePrefs, error) {
	var prefs ProfilePrefs
	data, err := os.ReadFile(filepath.Join(profileDir, prefsFile))
	if os.IsNotExist(err) {
		return ProfilePrefs{Headless: true}, nil
	}
	if err != nil {
		return prefs, fmt.Errorf("读取 profile 偏好: %w", err)
	}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return prefs, fmt.Errorf("解析 profile 偏好 %s: %w", prefsFile, err)
	}
	return prefs, nil
}

/*
 * WriteProfilePrefs 原子落盘 profile 偏好（tmp + rename，并发读不见截断文件；
 * ensure 重拉时读偏好、显式传参覆写回存——D10）。
 */
func WriteProfilePrefs(profileDir string, prefs ProfilePrefs) error {
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return fmt.Errorf("创建 profile 目录: %w", err)
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 profile 偏好: %w", err)
	}
	tmp, err := os.CreateTemp(profileDir, ".prefs-*.tmp")
	if err != nil {
		return fmt.Errorf("创建 profile 偏好临时文件: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // rename 成功后为 no-op
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("写入 profile 偏好临时文件: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭 profile 偏好临时文件: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("profile 偏好临时文件权限: %w", err)
	}
	if err := os.Rename(tmpName, filepath.Join(profileDir, prefsFile)); err != nil {
		return fmt.Errorf("替换 profile 偏好: %w", err)
	}
	return nil
}
