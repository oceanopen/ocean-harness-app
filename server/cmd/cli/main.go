// ocean-harness CLI 入口：进程寿命 = 单命令，main 只做流绑定 + os.Exit。
// 全部逻辑在 internal/cli（Run 返回退出码而非直接退出，保证可测性）。
package main

import (
	"os"

	"ocean-harness/server/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], cli.Streams{Stdout: os.Stdout, Stderr: os.Stderr}))
}
