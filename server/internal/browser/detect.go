package browser

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// 本文件只做「错误文案增强」级探测（D2：浏览器探测成熟度交给 playwright 的
// --browser chrome channel 机制，本包不参与拉起决策）。

// chromeCandidates 系统标准安装路径候选（按常见度排序）。
func chromeCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			filepath.Join(os.Getenv("HOME"), "Applications/Google Chrome.app/Contents/MacOS/Google Chrome"),
		}
	case "windows":
		return []string{
			filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("LocalAppData"), "Google", "Chrome", "Application", "chrome.exe"),
		}
	default: // linux 及其余 unix：非当前打包目标，unix 分支天然兼容
		return []string{
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/snap/bin/chromium",
		}
	}
}

// DetectChrome 探测系统 Chrome 可执行文件（仅用于 spawn 失败时的错误文案增强：
// 未探测到时提示安装 Chrome，而非让 playwright 的 channel 报错裸奔）。
func DetectChrome() (string, bool) {
	for _, cand := range chromeCandidates() {
		if cand == "" {
			continue
		}
		if info, err := os.Stat(cand); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return cand, true
		}
	}
	return "", false
}

// chromeMissingHint Chrome 缺失时的用户提示（spawn 失败文案增强用）。
func chromeMissingHint() string {
	if _, ok := DetectChrome(); ok {
		return "" // 探测到了：失败原因在别处，不出误导文案
	}
	return "（未探测到系统 Chrome：请安装 Google Chrome 后重试——引擎经 --browser chrome 拉起系统浏览器，不自带浏览器二进制）"
}

// Chrome 在 user-data-dir 根部落的单实例锁文件（darwin/linux 为符号链接 + socket，
// Windows 走内核 mutex 不落文件——Windows 侧检测不到，属已知局限）。
var singletonLockNames = []string{"SingletonLock", "SingletonSocket", "SingletonCookie"}

// HasProfileLock 识别 profile 目录是否被残留 Chrome 会话占住（单实例锁存在）。
// 引擎 spawn 失败且锁存在时，提示「检测到残留会话」；实际回收（pid 文件 + 杀组）
// 归 manager 的 recoverOrphans（T1.3）。
func HasProfileLock(profileDir string) bool {
	for _, name := range singletonLockNames {
		if _, err := os.Lstat(filepath.Join(profileDir, name)); err == nil {
			return true
		}
	}
	return false
}

// profileLockHint 残留会话提示文案（spawn 失败文案增强用）。
func profileLockHint(profileDir string) string {
	if !HasProfileLock(profileDir) {
		return ""
	}
	return fmt.Sprintf("（检测到残留会话占住 profile 目录 %s：Chrome 单实例锁存在，可能是上次会话未正常退出；请稍候重试或手动删除该目录）", profileDir)
}
