// 编译 src-server 本地 HTTP 服务为 Tauri sidecar 二进制（dev/build 统一），产出 target-triple 后缀名。
//
// Tauri sidecar 约定（见 tauri.conf.json bundle.externalBin: ["binaries/go-server-bin"]）：
// 打包时按当前 target triple 找 src-tauri/binaries/go-server-bin-<triple>，去后缀随包分发，
// build 模式下放进 Contents/MacOS/（macOS）并随主 app 签名——arm64 下 AMFI 不再拦。
// dev 模式下 tauri-build（cargo build）把该文件拷到 target/<profile>/go-server-bin 供 sidecar() 解析。
// 故本脚本须按目标 GOOS/GOARCH 产出带 triple 后缀的文件名（Tauri 不会自动补 triple）。
//
// 目标平台：CI 通过 GOOS/GOARCH env 注入（按 matrix 交叉编译，见 .github/workflows/release-assets.yml）；
// 本地未设时按宿主 platform/arch 推断（process.arch 的 x64 对应 GOARCH=amd64）。
// Go 服务纯 stdlib 无 CGO，可纯交叉编译。
import { execFileSync } from 'node:child_process';
import { mkdirSync } from 'node:fs';
import process from 'node:process';

const targetGOOS = process.env.GOOS
  ?? (process.platform === 'win32'
    ? 'windows'
    : process.platform === 'darwin'
      ? 'darwin'
      : 'linux');

const targetGOARCH = process.env.GOARCH
  ?? (process.arch === 'arm64'
    ? 'arm64'
    : process.arch === 'x64'
      ? 'amd64'
      : process.arch);

// GOOS/GOARCH → Rust target triple（与 CI matrix args 的 --target 一致）。
function targetTriple(goos, goarch) {
  const arch = goarch === 'arm64' ? 'aarch64' : goarch === 'amd64' ? 'x86_64' : goarch;
  if (goos === 'darwin') {
    return `${arch}-apple-darwin`;
  }
  if (goos === 'windows') {
    return `${arch}-pc-windows-msvc`;
  }
  if (goos === 'linux') {
    return `${arch}-unknown-linux-gnu`;
  }
  throw new Error(`unsupported GOOS: ${goos} (GOARCH: ${goarch})`);
}

const triple = targetTriple(targetGOOS, targetGOARCH);
const exeSuffix = targetGOOS === 'windows' ? '.exe' : '';

// 全新克隆下 binaries/ 可能不存在，递归创建兜底（go build -o 不会自动建父目录）。
mkdirSync('src-tauri/binaries', { recursive: true });

execFileSync(
  'go',
  ['build', '-C', 'src-server', '-o', `../src-tauri/binaries/go-server-bin-${triple}${exeSuffix}`, './cmd/server'],
  { stdio: 'inherit' },
);
