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

// 目录布局 SSOT（D2/D3/D10）：
//
//	~/.ocean-harness/browser/
//	  ├─ profiles/<name>/       per-profile Chrome user-data-dir（登录态活 SSOT，Chromium 自管）
//	  │   ├─ prefs.json         非凭据偏好元数据（headless 等，D10）
//	  │   └─ .engine.pid        引擎进程 pid 文件（T1.3 recoverOrphans 消费）
//	  └─ downloads/<name>/      引擎 --output-dir（下载与产物落点）
//
// prefs.json / .engine.pid 落在 user-data-dir 根部：Chrome 忽略未知文件，无冲突。

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

// profileNameRe profile 名合法性：字母数字开头，仅限字母数字与 - _，长度 1-64。
// 防路径穿越（"/"、".."、前导点均被拒；名字不含点，Windows 保留设备名另行整名拒绝）。
var profileNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// windowsReservedNames Windows 保留设备名（con/nul/com1-9/lpt1-9；正则已禁点，扩展名
// 形态如 con.x 不可能命中，整名匹配即够）——放行会导致 MkdirAll/WriteFile 行为异常。
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

// ProfileDir 返回 per-profile Chrome user-data-dir（profiles/<name>/）。只推导不创建——
// 目录由 Chrome 首次启动自建（user-data-dir 语义）。
func ProfileDir(name string) (string, error) {
	if err := ValidateProfileName(name); err != nil {
		return "", err
	}
	return filepath.Join(Root(), "profiles", name), nil
}

// DownloadsDir 返回 per-profile下载/产物落点（downloads/<name>/），并确保目录存在
// （引擎 --output-dir 的前置；spike 实锤缺省会落 cwd/.playwright-mcp，故显式传值）。
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

// ProfilePrefs per-profile 偏好元数据（非凭据，不违 D3——D3 约束的是登录态凭据不落盘）。
// headless 缺省 false（有头）：登录态获取配方必须有头——人工登录介入窗口（D10）。
type ProfilePrefs struct {
	Headless bool `json:"headless"`
}

// prefsFile 偏好文件名（user-data-dir 根部，Chrome 忽略未知文件）。
const prefsFile = "prefs.json"

// ReadProfilePrefs 读取 profile 偏好（文件缺失 = 零值偏好 + nil error，即有头缺省）。
func ReadProfilePrefs(profileDir string) (ProfilePrefs, error) {
	var prefs ProfilePrefs
	data, err := os.ReadFile(filepath.Join(profileDir, prefsFile))
	if os.IsNotExist(err) {
		return prefs, nil
	}
	if err != nil {
		return prefs, fmt.Errorf("读取 profile 偏好: %w", err)
	}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return prefs, fmt.Errorf("解析 profile 偏好 %s: %w", prefsFile, err)
	}
	return prefs, nil
}

// WriteProfilePrefs 落盘 profile 偏好（ensure 重拉时读偏好、显式传参覆写回存——D10）。
func WriteProfilePrefs(profileDir string, prefs ProfilePrefs) error {
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return fmt.Errorf("创建 profile 目录: %w", err)
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 profile 偏好: %w", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, prefsFile), data, 0o644); err != nil {
		return fmt.Errorf("写入 profile 偏好: %w", err)
	}
	return nil
}
