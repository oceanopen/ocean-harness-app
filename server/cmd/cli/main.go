// ocean-harness CLI 入口：进程寿命 = 单命令，main 只做 os.Exit。
// 全部逻辑在 internal/cli（Execute 返回退出码而非直接退出，保证可测性）。
package main

import (
	"os"

	"ocean-harness/server/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
