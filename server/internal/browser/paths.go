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
 * 目录布局 SSOT（D2/D3/D10）：profiles/<name>/（per-profile Chrome user-data-dir，登录态活 SSOT，Chromium 自管；内含 prefs.json 非凭据偏好 D10、.engine.pid pid 锚点）、downloads/<name>/（引擎 --output-dir，下载与产物落点）、auth-states/<workspaceId>.json（工作空间共享登录态，D3 修订：凭据仅此一份落盘，不进数据库）。
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
 * profileNameRe profile 名合法性：字母数字开头，仅限字母数字与 - _，长度 1-64。
 * 防路径穿越（"/"、".."、前导点均被拒；名字不含点，Windows 保留设备名另行整名拒绝）。
 */
var profileNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

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
		return fmt.Errorf("profile 名 %q 非法：需以字母数字开头，仅含字母数字与 - _，长度 1-64", name)
	}
	if windowsReservedNames[strings.ToLower(name)] {
		return fmt.Errorf("profile 名 %q 非法：与 Windows 保留设备名冲突，请换名", name)
	}
	return nil
}

/*
 * profile 命名空间前缀（前后端各单点 SSOT，前端 derive.ts 同规则）：issue-<issueId>
 * 任务派生；workspace-<id> 工作空间维度（预留入口，共享登录态直通）。
 */
const (
	IssueProfilePrefix     = "issue-"
	WorkspaceProfilePrefix = "workspace-"
)

/*
 * ParseProfileNamespace 解析 profile 命名空间：返回命中前缀与前缀后的 id；
 * default/自定义名无命名空间（前缀为空，即无共享登录态）。
 */
func ParseProfileNamespace(profile string) (prefix, id string) {
	if id, ok := strings.CutPrefix(profile, IssueProfilePrefix); ok && id != "" {
		return IssueProfilePrefix, id
	}
	if id, ok := strings.CutPrefix(profile, WorkspaceProfilePrefix); ok && id != "" {
		return WorkspaceProfilePrefix, id
	}
	return "", ""
}

// MaxSessionsPerTask 单任务浏览器会话槽位数（第 1 会话为基名，2 起加 -N 后缀）。
const MaxSessionsPerTask = 5

/*
 * FormatIssueProfile 任务第 slot 个会话的 profile 名（slot 1 = issue-<issueId> 基名，
 * 2-MaxSessionsPerTask 加 -N 后缀）。slot 越界显式报错。
 */
func FormatIssueProfile(issueID string, slot int) (string, error) {
	if slot < 1 || slot > MaxSessionsPerTask {
		return "", fmt.Errorf("会话槽位 %d 非法（1-%d）", slot, MaxSessionsPerTask)
	}
	name := IssueProfilePrefix + issueID
	if slot > 1 {
		name = fmt.Sprintf("%s-%d", name, slot)
	}
	// 校验拼接后的最终名：超长 issueId 下基名合法而 -N 名超限的不对称在此拦下。
	if err := ValidateProfileName(name); err != nil {
		return "", err
	}
	return name, nil
}

/*
 * ParseIssueProfile 解析任务命名空间 profile 为基座 issueId 与槽位：尾「连字符+单数字」
 * （-2..-MaxSessionsPerTask）识别为槽位后缀，越界数字后缀（-0/-1/-6…）整体判非任务会话；
 * 其余（含 uuid 自带连字符尾）为槽位 1 基名——uuid 段内无连字符，尾「-数字」必为后缀。
 */
func ParseIssueProfile(profile string) (issueID string, slot int, ok bool) {
	id, isIssue := strings.CutPrefix(profile, IssueProfilePrefix)
	if !isIssue || id == "" {
		return "", 0, false
	}
	if len(id) >= 2 && id[len(id)-2] == '-' && id[len(id)-1] >= '0' && id[len(id)-1] <= '9' {
		n := int(id[len(id)-1] - '0')
		if n < 2 || n > MaxSessionsPerTask {
			return "", 0, false
		}
		return id[:len(id)-2], n, true
	}
	return id, 1, true
}

/*
 * issueQuotaBase 任务配额归组键（每任务并发上限用，宽于 ParseIssueProfile）：issue-
 * 前缀且尾随「连字符+单个数字」一律剥掉后缀归基座——越界槽位名（-0/-1/-6…）与其目标
 * 任务同配额，杜绝借名绕过；其余整体为基座，非 issue- 前缀 ok=false。
 */
func issueQuotaBase(profile string) (string, bool) {
	id, ok := strings.CutPrefix(profile, IssueProfilePrefix)
	if !ok || id == "" {
		return "", false
	}
	if n := len(id); n >= 2 && id[n-2] == '-' && id[n-1] >= '0' && id[n-1] <= '9' {
		id = id[:n-2]
	}
	return id, true
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
 * AuthStatePath 工作空间共享登录态文件（auth-states/<workspaceId>.json，Playwright
 * storageState 格式）。只推导不创建——目录与文件归写入方（原子写）负责。
 */
func AuthStatePath(workspaceID string) (string, error) {
	if err := ValidateProfileName(workspaceID); err != nil {
		return "", fmt.Errorf("工作空间 id 非法: %w", err)
	}
	return filepath.Join(Root(), "auth-states", workspaceID+".json"), nil
}

/*
 * ProfilePrefs per-profile 偏好元数据（非凭据，不违 D3——D3 约束的是登录态凭据不落盘）。
 * 缺省无头（D10 修订：D9 面板实时画面下人工登录/介入均在面板内完成，桌面零弹窗）；
 * 有头为显式选项，结构体零值语义不承载缺省——缺省判定在 ReadProfilePrefs。
 */
type ProfilePrefs struct {
	Headless bool `json:"headless"`
	// WorkspaceID 共享登录态归属工作空间（ensure 显式传入时落盘持久化，idle 冷启动/
	// 无头切换重建读回注入——共享键不依赖调用方每次携带；空 = 无共享）。
	WorkspaceID string `json:"workspaceId,omitempty"`
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
