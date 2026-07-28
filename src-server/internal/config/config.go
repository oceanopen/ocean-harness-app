// Package config 负责加载并校验服务运行配置。
//
// 配置来源优先级：环境变量 > 配置文件（yaml）。
//   - 环境变量（统一 GO_SERVER_ 前缀）始终优先，由 Rust 侧 spawn 时注入
//     （见 src-tauri/src/shared/http_server.rs），生产环境覆盖配置文件值。
//   - 配置文件经 -config flag 指定（本地调试用，如 config/settings.dev.yaml），
//     为环境变量缺失的字段提供默认值；flag 为空（生产环境，Rust 不传该 flag）则不读
//     任何文件，纯走环境变量，与改造前行为完全一致。
//
// 校验：mode 仅允许 gin debug/release/test；port ∈ [1,65535]；两个目录转绝对路径并 MkdirAll。
package config

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/goccy/go-yaml"
)

// 环境变量名统一大写，沿用现有 GO_SERVER_ 前缀（与 GO_SERVER_MODE 同源）。
const (
	EnvMode      = "GO_SERVER_MODE"       // gin 运行模式：debug / release / test
	EnvPort      = "GO_SERVER_PORT"       // HTTP 监听端口
	EnvLogDir    = "GO_SERVER_LOG_DIR"    // 日志目录（绝对路径）
	EnvSqliteDir = "GO_SERVER_SQLITE_DIR" // sqlite 数据目录（绝对路径）
)

// settingsFile 是 yaml 配置文件的结构（字段名采用 camelCase 对齐配置文件）。
// 仅作为环境变量缺失时的默认值来源，不对外暴露。
type settingsFile struct {
	Mode      string `yaml:"mode"`      // gin 运行模式：debug / release / test
	Port      int    `yaml:"port"`      // HTTP 监听端口
	LogDir    string `yaml:"logDir"`    // 日志目录（相对 src-server，运行时转绝对路径）
	SqliteDir string `yaml:"sqliteDir"` // sqlite 数据目录（相对 src-server，运行时转绝对路径）
}

// Config 是合并后的服务运行配置。
type Config struct {
	Mode      string // gin 运行模式：debug / release / test
	Port      int    // HTTP 监听端口
	LogDir    string // 日志目录（绝对路径）
	SqliteDir string // sqlite 数据目录（绝对路径）
}

// MustLoadConfig 读取并校验配置。优先级：环境变量 > 配置文件（-config 指定，可选）。
//
// -config flag 为空时不读文件，纯环境变量模式（生产环境）。任一必填字段最终缺失或非法
// 即 log.Fatalf 终止启动。校验项：mode 仅允许 gin 三种模式并同步 gin.SetMode；
// port ∈ [1,65535]；两个目录转绝对路径并 MkdirAll。
func MustLoadConfig() *Config {
	// 解析 -config flag（默认空：不读配置文件，纯环境变量模式）。
	configPath := flag.String("config", "", "配置文件路径（yaml）；为空则仅从环境变量加载")
	flag.Parse()

	// 1) 可选：加载 yaml 配置文件作为默认基底（flag 非空时）。文件缺失或解析失败即 Fatal。
	var sf settingsFile
	if *configPath != "" {
		data, err := os.ReadFile(*configPath)
		if err != nil {
			log.Fatalf("[config] read config file %q failed: %v", *configPath, err)
		}
		if err := yaml.Unmarshal(data, &sf); err != nil {
			log.Fatalf("[config] parse config file %q failed: %v", *configPath, err)
		}
	}

	// 2) 逐字段合并：环境变量优先，缺失则回退配置文件值。
	mode := firstNonEmpty(os.Getenv(EnvMode), sf.Mode)
	portStr := os.Getenv(EnvPort)
	logDir := firstNonEmpty(os.Getenv(EnvLogDir), sf.LogDir)
	sqliteDir := firstNonEmpty(os.Getenv(EnvSqliteDir), sf.SqliteDir)

	// 3) 必填校验：mode / 两个目录均不可为空（env 与文件都未提供即缺失）。
	if mode == "" {
		log.Fatalf("[config] %s is required (debug/release/test), set via env or config file", EnvMode)
	}
	if logDir == "" {
		log.Fatalf("[config] %s is required, set via env or config file", EnvLogDir)
	}
	if sqliteDir == "" {
		log.Fatalf("[config] %s is required, set via env or config file", EnvSqliteDir)
	}

	// 4) Mode 仅允许 gin 三种模式（统一小写后比对），并同步设置 gin 全局模式。
	mode = strings.ToLower(mode)
	switch mode {
	case gin.DebugMode, gin.ReleaseMode, gin.TestMode:
		// ok
	default:
		log.Fatalf("[config] mode=%q invalid, must be one of: %s/%s/%s",
			mode, gin.DebugMode, gin.ReleaseMode, gin.TestMode)
	}
	gin.SetMode(mode)

	// 关闭 gin 的 debug 横幅（"Switch to release" 提示 + 路由表打印）：
	// gin 在 debug/test 模式会经 gin.DefaultWriter 输出这些提示，桥接到 zap 后会污染日志。
	// 注意须设为 no-op 而非 nil——nil 时 gin 会回落到写 DefaultWriter，提示仍会出现。
	// release 模式下 debugPrint 自身提前 return，本就静默，此处仅影响 debug/test。
	gin.DebugPrintFunc = func(string, ...any) {}

	// 5) Port：env 非空则必须合法，否则回退配置文件值；最终必填且 ∈ [1,65535]。
	var port int
	if portStr != "" {
		p, err := strconv.Atoi(portStr)
		if err != nil {
			log.Fatalf("[config] env %s=%q invalid", EnvPort, portStr)
		}
		port = p
	} else {
		port = sf.Port
	}
	if port < 3000 || port > 10000 {
		log.Fatalf("[config] %s is required and must be int in [3000,10000], set via env or config file", EnvPort)
	}

	// 6) 目录转绝对路径 + MkdirAll 确保，便于日志/sqlite 直接落盘。
	logDirAbs, err := ensureDir(logDir)
	if err != nil {
		log.Fatalf("[config] %s=%q invalid: %v", EnvLogDir, logDir, err)
	}
	sqliteDirAbs, err := ensureDir(sqliteDir)
	if err != nil {
		log.Fatalf("[config] %s=%q invalid: %v", EnvSqliteDir, sqliteDir, err)
	}

	return &Config{
		Mode:      mode,
		Port:      port,
		LogDir:    logDirAbs,
		SqliteDir: sqliteDirAbs,
	}
}

// firstNonEmpty 返回首个非空字符串（用于"环境变量优先，回退配置文件值"）。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
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
