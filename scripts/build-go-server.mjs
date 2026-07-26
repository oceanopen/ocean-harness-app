// 编译 src-go 本地 HTTP 服务到 src-tauri/resources/，供 Tauri 打包（build 模式由 Rust spawn）。
//
// 为什么需要 node 包一层而不是直接 `go build`：
// `go build -o <name>` 用了显式 -o 时不会自动追加可执行扩展名，Windows 目标会产出
// `go-server-bin`（无 .exe），但 Rust bin_name() 在 Windows 找的是 `go-server-bin.exe`
// （src-tauri/src/shared/http_server.rs，build 模式 resource_dir().join(bin_name())）。
// 这里按目标 GOOS 手动补 .exe，保证产出名与 Rust 一致。
//
// 目标 GOOS：CI 通过 GOOS env 注入（按 matrix 目标交叉编译，见 .github/workflows/release-assets.yml）；
// 本地未设时按宿主平台推断。pnpm 保证脚本以仓库根为 cwd，故 -C src-go / -o ../... 相对路径与原 npm 脚本一致。
import { execFileSync } from 'node:child_process';
import process from 'node:process';

const targetGOOS
  = process.env.GOOS
    ?? (process.platform === 'win32'
      ? 'windows'
      : process.platform === 'darwin'
        ? 'darwin'
        : 'linux');

// Go 服务纯 stdlib 无 CGO，可纯交叉编译。
const exeSuffix = targetGOOS === 'windows' ? '.exe' : '';

execFileSync(
  'go',
  ['build', '-C', 'src-go', '-o', `../src-tauri/resources/go-server-bin${exeSuffix}`, './cmd/server'],
  { stdio: 'inherit' },
);
