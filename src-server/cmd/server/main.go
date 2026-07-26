// Package main 是 HTTP 本地服务的入口。
//
// 由 Tauri 应用（Rust 侧 src-tauri/src/shared/http_server.rs）在启动时拉起：
//   - dev 模式：先 `go build` 编译出二进制，再 spawn 该二进制（持有真正的服务进程 handle）
//   - build 模式：spawn 随包分发的二进制 go-server-bin
//
// dev/build 都 spawn 二进制而非 `go run`：避免孤儿进程。
//
// 运行模式通过环境变量 GO_SERVER_MODE（dev/build）注入，体现在 /api/sysinfo 的 mode 字段。
// 固定监听 127.0.0.1:9000，前端（HttpServerPage）直接 fetch，无需动态获取端口。
package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"we-claude-terminal/go-server/internal/handler"
)

// 固定监听地址：前端直接 fetch http://127.0.0.1:9000/api/sysinfo。
const listenAddr = "127.0.0.1:9000"

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sysinfo", handler.SysInfo)

	log.Printf("http-server listening on %s mode=%s", listenAddr, handler.Mode())

	server := &http.Server{Addr: listenAddr, Handler: mux}

	// 优雅退出：Rust 在应用退出时发 SIGTERM，监听后关闭连接。
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		<-sigCh
		log.Printf("http-server shutting down")
		_ = server.Close()
	}()

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve failed: %v", err)
	}
}
