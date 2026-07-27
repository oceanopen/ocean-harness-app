// Package config 负责从环境变量加载并校验服务运行配置。
//
// 不读取任何配置文件：端口、日志目录、sqlite 目录、运行模式均由 Rust 侧 spawn 时
// 通过环境变量注入（见 src-tauri/src/shared/http_server.rs）。
package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// 环境变量名统一大写，沿用现有 GO_SERVER_ 前缀（与 GO_SERVER_MODE 同源）。
const (
	EnvMode      = "GO_SERVER_MODE"       // gin 运行模式：debug / release / test
	EnvPort      = "GO_SERVER_PORT"       // HTTP 监听端口
	EnvLogDir    = "GO_SERVER_LOG_DIR"    // 日志目录（绝对路径）
	EnvSqliteDir = "GO_SERVER_SQLITE_DIR" // sqlite 数据目录（绝对路径）
)

// Config 是从环境变量加载的服务运行配置。
type Config struct {
	Mode      string // gin 运行模式：debug / release / test
	Port      int    // HTTP 监听端口
	LogDir    string // 日志目录（绝对路径）
	SqliteDir string // sqlite 数据目录（绝对路径）
}

// MustLoad 从环境变量读取并校验配置。任一必填项缺失或非法即 log.Fatalf 终止启动。
//
// 校验项：四项全部非空；Mode 仅允许 gin.DebugMode/gin.ReleaseMode/gin.TestMode；
// Port 必须是 [1,65535] 的整数；两个目录会被转为绝对路径并 MkdirAll 确保。
func MustLoadConfig() *Config {
	mode := strings.ToLower(os.Getenv(EnvMode))
	portStr := os.Getenv(EnvPort)
	logDir := os.Getenv(EnvLogDir)
	sqliteDir := os.Getenv(EnvSqliteDir)

	// 1) 全部必填，非空校验。
	if mode == "" {
		log.Fatalf("[config] env %s is required (debug/release/test)", EnvMode)
	}
	if portStr == "" {
		log.Fatalf("[config] env %s is required", EnvPort)
	}
	if logDir == "" {
		log.Fatalf("[config] env %s is required", EnvLogDir)
	}
	if sqliteDir == "" {
		log.Fatalf("[config] env %s is required", EnvSqliteDir)
	}

	// 2) Mode 仅允许 gin 三种模式，并同步设置 gin 全局模式。
	switch mode {
	case gin.DebugMode, gin.ReleaseMode, gin.TestMode:
		// ok
	default:
		log.Fatalf("[config] env %s=%q invalid, must be one of: %s/%s/%s",
			EnvMode, mode, gin.DebugMode, gin.ReleaseMode, gin.TestMode)
	}
	gin.SetMode(mode)

	// 关闭 gin 的 debug 横幅（"Switch to release" 提示 + 路由表打印）：
	// gin 在 debug/test 模式会经 gin.DefaultWriter 输出这些提示，桥接到 zap 后会污染日志。
	// 注意须设为 no-op 而非 nil——nil 时 gin 会回落到写 DefaultWriter，提示仍会出现。
	// release 模式下 debugPrint 自身提前 return，本就静默，此处仅影响 debug/test。
	gin.DebugPrintFunc = func(string, ...any) {}

	// 3) Port 必须是合法端口。
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		log.Fatalf("[config] env %s=%q invalid, must be int in [1,65535]", EnvPort, portStr)
	}

	// 4) 目录转绝对路径 + MkdirAll 确保，便于日志/sqlite 直接落盘。
	logDirAbs, err := ensureDir(logDir)
	if err != nil {
		log.Fatalf("[config] env %s=%q invalid: %v", EnvLogDir, logDir, err)
	}
	sqliteDirAbs, err := ensureDir(sqliteDir)
	if err != nil {
		log.Fatalf("[config] env %s=%q invalid: %v", EnvSqliteDir, sqliteDir, err)
	}

	return &Config{
		Mode:      mode,
		Port:      port,
		LogDir:    logDirAbs,
		SqliteDir: sqliteDirAbs,
	}
}

// ensureDir 将目录转为绝对路径并确保其存在（MkdirAll，已存在不报错）。
func ensureDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}
	return abs, nil
}
