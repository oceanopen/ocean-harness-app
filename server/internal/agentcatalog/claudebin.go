// claude 二进制定位（P5 定稿终态：vendored 自带 claude 全链路 SSOT）。adapter
// vendored 安装内包的 Claude Agent SDK 以 optionalDependencies 形式携带平台原生
// claude 二进制（@anthropic-ai/claude-agent-sdk-<平台triple>，构建期 npm 按构建机
// 平台装入其一，CI 按平台构建各自就位），本包按运行时平台定位并校验存在性，作为
// 终端直启 / ACP / bot headless / marketplace 统一消费的 claude 可执行文件——
// claude 路径探测链退役；终端手动路径保留本机 claude（shell 自解析）。

package agentcatalog

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// claudePlatformTriple 运行时平台 → SDK 平台二进制包尾缀（npm os/cpu 命名约定）。
// musl 变体（linux-x64-musl / linux-arm64-musl）暂不区分：SDK 的 glibc 形态原生
// 二进制为静态链接，musl 环境兼容性 TODO(P5) 随 Linux 分发面评估（当前桌面分发
// 以 darwin/win32 为主）。
func claudePlatformTriple() string {
	goos, goarch := runtime.GOOS, runtime.GOARCH
	switch {
	case goos == "darwin" && goarch == "arm64":
		return "darwin-arm64"
	case goos == "darwin" && goarch == "amd64":
		return "darwin-x64"
	case goos == "linux" && goarch == "amd64":
		return "linux-x64"
	case goos == "linux" && goarch == "arm64":
		return "linux-arm64"
	case goos == "windows" && goarch == "amd64":
		return "win32-x64"
	case goos == "windows" && goarch == "arm64":
		return "win32-arm64"
	default:
		return ""
	}
}

// claudeBinName 平台二进制文件名（npm 包 files 命名约定：Windows 带 .exe 后缀）。
func claudeBinName() string {
	if runtime.GOOS == "windows" {
		return "claude.exe"
	}
	return "claude"
}

// VendoredClaudeBin 定位 vendored 安装内包的 SDK 平台原生 claude 二进制并校验存在
// 性（installDir 为 EnsureVendored 返回值）。路径形如
// <installDir>/node_modules/@anthropic-ai/claude-agent-sdk-<triple>/claude；
// SDK 自身对 CLAUDE_CODE_EXECUTABLE 缺省时的回落解析即同款布局，显式定位保持
// 「探测通过 = 生产链路能拉起」零差异。
func VendoredClaudeBin(installDir string) (string, error) {
	triple := claudePlatformTriple()
	if triple == "" {
		return "", fmt.Errorf("平台 %s/%s 无 SDK 自带 claude 二进制形态", runtime.GOOS, runtime.GOARCH)
	}
	p := filepath.Join(installDir, "node_modules", "@anthropic-ai", "claude-agent-sdk-"+triple, claudeBinName())
	if info, err := os.Stat(p); err != nil || info.IsDir() {
		return "", fmt.Errorf("vendored claude 二进制缺失: %s（vendored 安装不完整：请重新检测，持续失败请重新安装应用）", p)
	}
	return p, nil
}

// vendoring 两目录（config 可选字段的进程级注入，main 启动装配）：agentcatalog 不
// 反向依赖 config/global，由组装根注入一次；空 = 未配置，EnsureVendored 调用期显式
// 报错（与 acpdoctor.Dirs 显式传参同语义）。
var (
	vendoredDirsMu sync.RWMutex
	resourcesDir   string
	adaptersDir    string
)

// SetVendoredDirs 注入 vendoring 两目录（main 启动装配，进程级一次）。
func SetVendoredDirs(resources, adapters string) {
	vendoredDirsMu.Lock()
	defer vendoredDirsMu.Unlock()
	resourcesDir, adaptersDir = resources, adapters
}

func vendoredDirs() (string, string) {
	vendoredDirsMu.RLock()
	defer vendoredDirsMu.RUnlock()
	return resourcesDir, adaptersDir
}

// 成功缓存（失败不缓存——重新 vendoring / 修复安装后无需重启 sidecar 即生效，
// 对齐原 clibin 缓存口径）。
var (
	claudeBinMu     sync.Mutex
	claudeBinCached string
)

// ResolveVendoredClaudeBin 一步式解析 vendored 自带 claude：catalog 条目 →
// EnsureVendored 受管安装 → 平台二进制定位。vendoring 两目录取启动期注入值
// （SetVendoredDirs）。成功结果进程级缓存，供 bot driver 与 marketplace 共用；
// doctor 走分步编排以归因探测阶段，真握手测试用显式目录装配，均不经此缓存路径。
func ResolveVendoredClaudeBin(agentCode string) (string, error) {
	claudeBinMu.Lock()
	defer claudeBinMu.Unlock()
	if claudeBinCached != "" {
		return claudeBinCached, nil
	}
	entry, ok := GetAgentCatalogInfoByCode(agentCode)
	if !ok {
		return "", fmt.Errorf("catalog 无 agent %q 条目", agentCode)
	}
	resources, adapters := vendoredDirs()
	installDir, err := EnsureVendored(entry, resources, adapters)
	if err != nil {
		return "", err
	}
	bin, err := VendoredClaudeBin(installDir)
	if err != nil {
		return "", err
	}
	claudeBinCached = bin
	return bin, nil
}
