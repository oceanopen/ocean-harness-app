package browser

import (
	"fmt"
	"os"
	"path/filepath"
)

// 本文件只做「错误文案增强」级探测（D2 修订：浏览器 = playwright 内置 Chromium 嵌入
// 分发，随应用打包、PLAYWRIGHT_BROWSERS_PATH 定位，本包不参与拉起决策；系统 Chrome
// 探测随引擎 --browser chrome channel 一并退役）。

// embeddedBrowsersDirName 内置浏览器目录名（条目版本目录内；scripts/
// vendor-browser-engine.ts 构建期产出，EnsureVendored 整树复制随之进受管目录，
// client.go 注入 PLAYWRIGHT_BROWSERS_PATH 与之同源）。
const embeddedBrowsersDirName = "browsers"

// embeddedBrowsersDir 内置浏览器目录绝对路径。
func embeddedBrowsersDir(installDir string) string {
	return filepath.Join(installDir, embeddedBrowsersDirName)
}

// HasEmbeddedBrowsers 内置浏览器在位检查（存在且非空即视为在位——目录由 playwright
// install 整批产出，半空状态不可达）。
func HasEmbeddedBrowsers(installDir string) bool {
	entries, err := os.ReadDir(embeddedBrowsersDir(installDir))
	return err == nil && len(entries) > 0
}

// embeddedBrowsersMissingHint 内置浏览器缺失时的用户提示（spawn 失败文案增强用）：
// 覆盖构建期未装（vendor 脚本 postInstall 失败）与应用安装损坏。
func embeddedBrowsersMissingHint(installDir string) string {
	if HasEmbeddedBrowsers(installDir) {
		return "" // 在位：失败原因在别处，不出误导文案
	}
	return fmt.Sprintf("（内置浏览器缺失或损坏：%s 不存在或为空，请重新安装应用）", embeddedBrowsersDir(installDir))
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
