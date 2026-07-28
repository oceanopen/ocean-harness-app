// 编译 src-server 本地 HTTP 服务为 Tauri sidecar 二进制（dev/build 各自命名），产出 target-triple 后缀名。
//
// Tauri sidecar 约定（见 tauri.conf.json bundle.externalBin）：
// 打包时按当前 target triple 找 src-tauri/binaries/<baseName>-<triple>，去后缀随包分发，
// build 模式下放进 Contents/MacOS/（macOS）并随主 app 签名——arm64 下 AMFI 不再拦。
// dev 模式下 tauri-build（cargo build）把该文件拷到 target/<profile>/<baseName> 供 sidecar() 解析。
// 故本脚本须按目标 GOOS/GOARCH 产出带 triple 后缀的文件名（Tauri 不会自动补 triple）。
//
// baseName 复用各 conf 的 identifier，让进程名携带环境标识（ps/活动监视器可区分）：
//   dev 取 tauri.dev.conf.json、build 取 tauri.conf.json 的 identifier（见下方 outName 拼接）。
// 运行模式由 beforeDevCommand/beforeBuildCommand 经 cross-env 注入 TAURI_RUN_MODE=dev/build（跨平台）；Rust 侧
// app.shell().sidecar(app.config().identifier()+"-go_server_bin") 同源取 identifier，故 dev/build 用同一份代码。
//
// 目标平台：CI 通过 GOOS/GOARCH env 注入（按 matrix 交叉编译，见 .github/workflows/release-assets.yml）；
// 本地未设时按宿主 platform/arch 推断（process.arch 的 x64 对应 GOARCH=amd64）。Go 服务为纯 Go 实现（sqlite 用 glebarez 纯 Go 驱动，无 CGO），显式 CGO_ENABLED=0 可纯交叉编译。
//
// 只产目标架构一份：release 路径已不再跑 gen:bindings（host 构建），tauri-build 仅在 target 构建里找
// <baseName>-<target-triple>，故无需再补产 host triple。
import { execFileSync } from 'node:child_process';
import { mkdirSync, readFileSync } from 'node:fs';
import process from 'node:process';

// 宿主 GOOS/GOARCH：本地未设目标 env 时的回退默认值。
const hostGOOS
  = process.platform === 'win32'
    ? 'windows'
    : process.platform === 'darwin'
      ? 'darwin'
      : 'linux';
const hostGOARCH
  = process.arch === 'arm64'
    ? 'arm64'
    : process.arch === 'x64'
      ? 'amd64'
      : process.arch;

// 目标 GOOS/GOARCH：CI 通过 env 注入，本地未设时取宿主。
const targetGOOS = process.env.GOOS ?? hostGOOS;
const targetGOARCH = process.env.GOARCH ?? hostGOARCH;

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

function exeSuffixFor(goos) {
  return goos === 'windows' ? '.exe' : '';
}

// 运行模式（dev/build）：由 beforeDevCommand/beforeBuildCommand 经 cross-env 注入 TAURI_RUN_MODE。
// 用 cross-env 包裹保证跨平台（Unix shell 与 Windows cmd/PowerShell 都能设 env）。
// 决定读哪个 conf 的 identifier 作为 sidecar baseName，进而决定进程名环境标识。
// 未显式注入即拒绝执行：baseName 强依赖模式，产出错误名字会导致 sidecar 找不到、难排查。
const runMode = process.env.TAURI_RUN_MODE;
if (runMode !== 'dev' && runMode !== 'build') {
  throw new Error(
    `TAURI_RUN_MODE env required (dev|build), got: ${String(runMode)}. `
    + '由 beforeDevCommand/beforeBuildCommand 经 cross-env 注入；通过 pnpm tauri:dev / pnpm tauri:build 触发自动获得，直接运行本脚本时需自行设置 TAURI_RUN_MODE=dev|build。',
  );
}

// dev 读 dev.conf、build 读主 conf，取各自的 identifier 作为 sidecar baseName 前缀。
const confPath = runMode === 'dev' ? 'src-tauri/tauri.dev.conf.json' : 'src-tauri/tauri.conf.json';
const identifier = JSON.parse(readFileSync(confPath, 'utf8')).identifier;

// 全新克隆下 binaries/ 可能不存在，递归创建兜底（go build -o 不会自动建父目录）。
mkdirSync('src-tauri/binaries', { recursive: true });

// 产目标 triple 一份（app 实际运行的架构）。
// 文件名 = {identifier}-go_server_bin-{triple}{exe}：triple 是 Tauri sidecar 约定后缀，
// 去掉后即进程名（identifier 携带 dev/build 标识）。
const triple = targetTriple(targetGOOS, targetGOARCH);
const outName = `${identifier}-go_server_bin-${triple}${exeSuffixFor(targetGOOS)}`;
execFileSync(
  'go',
  ['build', '-C', 'src-server', '-o', `../src-tauri/binaries/${outName}`, './cmd/server'],
  { stdio: 'inherit', env: { ...process.env, GOOS: targetGOOS, GOARCH: targetGOARCH, CGO_ENABLED: '0' } },
);
