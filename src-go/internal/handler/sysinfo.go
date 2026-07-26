// Package handler 存放 go-server 的 HTTP handler，仅包内可见（internal/ 约定）。
package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"runtime"
)

// Mode 返回当前运行模式，由 Rust spawn 时通过 GO_SERVER_MODE 环境变量注入。
// dev：go run 调试模式；build：编译后二进制模式。未设置时默认 dev。
func Mode() string {
	if m := os.Getenv("GO_SERVER_MODE"); m != "" {
		return m
	}
	return "dev"
}

// SysInfoResponse 是 GET /api/sysinfo 的返回结构，前端 HttpServerPage 直接消费。
// JSON tag 与前端 SysInfoData interface 对齐（src/windows/panel/HttpServerPage.tsx）。
type SysInfoResponse struct {
	Hostname  string `json:"hostname"`
	GoVersion string `json:"goVersion"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Mode      string `json:"mode"`
}

// SysInfo 处理 GET /api/sysinfo：返回本地系统信息 + 运行模式。
// 前端按钮通过 fetch('http://127.0.0.1:<port>/api/sysinfo') 直连调用。
func SysInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	hostname, _ := os.Hostname()

	resp := SysInfoResponse{
		Hostname:  hostname,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Mode:      Mode(),
	}

	// CORS：前端 webview origin（tauri://localhost / http://localhost:7102）与 127.0.0.1 不同源。
	// 简单 GET 无预检也能拿到响应体，但显式放行便于未来扩展（自定义头 / POST）。
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
