// Package initialize 存放各基础设施（logger / sqlite / gin writer）的初始化逻辑。
package initialize

import (
	"fmt"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"

	"ocean-harness/src-server/internal/config"
	"ocean-harness/src-server/internal/global"
)

// LoggerTimeFormat 日志时间格式（毫秒精度）。
const LoggerTimeFormat = "2006-01-02 15:04:05.000"

// 日志轮转默认参数（目录来自环境变量，轮转策略固定）。
const (
	logMaxSizeMB  = 20 // 单文件最大 MB
	logMaxAgeDays = 10 // 保留天数
	logMaxBackups = 30
)

// MustInitZapLogger 初始化 zap：三路 tee（error 日志 / 全量日志 / 控制台），
// 文件走 lumberjack 轮转。日志目录来自环境变量（cfg.LogDir）。
func MustInitZapLogger(cfg *config.Config) {
	// 控制台同步器走 stderr：日志属诊断信息（非程序数据输出），按 Unix 惯例应走 stderr；
	// 更关键的是 Tauri sidecar 模式下，Rust（http_server.rs 事件线程）把子进程 stderr 归为
	// CommandEvent::Stderr → log::warn!（release 级别留痕），而 stdout 归为 log::info!（release 被过滤）。
	// Fatal 级别的启动失败原因（如端口占用）必须走 stderr 才能被 Rust 捕获并回传前端 toast。
	consoleSyncer := zapcore.AddSync(os.Stderr)

	jsonEncoder := zapcore.NewJSONEncoder(newEncoderConfig())
	consoleEncoder := zapcore.NewConsoleEncoder(newEncoderConfig())

	core := zapcore.NewTee(
		zapcore.NewCore(jsonEncoder, fileSyncer(fmt.Sprintf("%s/app.error.log", cfg.LogDir)), zap.ErrorLevel),
		zapcore.NewCore(jsonEncoder, fileSyncer(fmt.Sprintf("%s/app.log", cfg.LogDir)), zap.DebugLevel),
		zapcore.NewCore(consoleEncoder, consoleSyncer, zap.DebugLevel),
	)

	logger := zap.New(core, zap.AddCaller())
	global.Logger = logger
	// 同时替换 zap 包级全局实例，便于未显式持引用的包用 zap.L()/zap.S()。
	zap.ReplaceGlobals(logger)

	global.Logger.Info("zap logger initialized")
}

// newEncoderConfig 统一的 zap 编码配置：时间带毫秒、Level 大写、Caller 短路径。
func newEncoderConfig() zapcore.EncoderConfig {
	ec := zap.NewProductionEncoderConfig()
	ec.EncodeTime = zapcore.TimeEncoderOfLayout(LoggerTimeFormat)
	ec.TimeKey = "time"
	ec.EncodeLevel = zapcore.CapitalLevelEncoder
	ec.EncodeCaller = zapcore.ShortCallerEncoder
	ec.EncodeDuration = zapcore.StringDurationEncoder // 用 d.String() 渲染，如 1.234ms / 456µs
	return ec
}

// fileSyncer 构造一个带大小/时间轮转的文件 WriteSyncer（lumberjack）。
func fileSyncer(filePath string) zapcore.WriteSyncer {
	lj := &lumberjack.Logger{
		Filename:   filePath,
		MaxSize:    logMaxSizeMB,
		MaxAge:     logMaxAgeDays,
		MaxBackups: logMaxBackups,
		LocalTime:  true,
		Compress:   false,
	}
	return zapcore.AddSync(lj)
}
