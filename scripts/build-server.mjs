// 编译 src-server 本地 HTTP 服务为 Tauri sidecar 二进制（dev/build 统一），产出 target-triple 后缀名。
//
// Tauri sidecar 约定（见 tauri.conf.json bundle.externalBin: ["binaries/go-server-bin"]）：
// 打包时按当前 target triple 找 src-tauri/binaries/go-server-bin-<triple>，去后缀随包分发，
// build 模式下放进 Contents/MacOS/（macOS）并随主 app 签名——arm64 下 AMFI 不再拦。
// dev 模式下 tauri-build（cargo build）把该文件拷到 target/<profile>/go-server-bin 供 sidecar() 解析。
// 故本脚本须按目标 GOOS/GOARCH 产出带 triple 后缀的文件名（Tauri 不会自动补 triple）。
//
// 跨编译时还要补产一份 host triple：gen:bindings（cargo run --bin export_bindings，不带 --target）走 host
// 构建，tauri-build 的 build script 按 TARGET=host triple 找 sidecar，缺失即硬报错（copy_binaries 用 ?）。
// CI 的 x86_64-apple-darwin job 在 arm64 runner 上交叉编译，host=aarch64 ≠ target=x86_64，只产 target
// 会让 gen:bindings 失败（实测可复现）。故 host≠target 时额外产 host triple 喂给 gen:bindings 的 host 构建。
//
// 目标平台：CI 通过 GOOS/GOARCH env 注入（按 matrix 交叉编译，见 .github/workflows/release-assets.yml）；
// 本地未设时按宿主 platform/arch 推断（process.arch 的 x64 对应 GOARCH=amd64）。Go 服务纯 stdlib 无 CGO，可纯交叉编译。
import { execFileSync } from 'node:child_process';
import { mkdirSync } from 'node:fs';
import process from 'node:process';

// 宿主 GOOS/GOARCH（gen:bindings 的 host 构建按此找 sidecar）。
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

// 全新克隆下 binaries/ 可能不存在，递归创建兜底（go build -o 不会自动建父目录）。
mkdirSync('src-tauri/binaries', { recursive: true });

// 先产 target triple（app 实际运行的架构）；若 host triple 与之不同，再补产 host triple
// （gen:bindings 走 host 构建需要它）。host==target 时按 triple 去重只产一份。
const buildTargets = [{ goos: targetGOOS, goarch: targetGOARCH }, { goos: hostGOOS, goarch: hostGOARCH }];
const produced = new Set();
for (const { goos, goarch } of buildTargets) {
  const triple = targetTriple(goos, goarch);
  if (produced.has(triple)) {
    continue;
  }
  produced.add(triple);
  execFileSync(
    'go',
    ['build', '-C', 'src-server', '-o', `../src-tauri/binaries/go-server-bin-${triple}${exeSuffixFor(goos)}`, './cmd/server'],
    // 显式按本次目标的 GOOS/GOARCH 覆盖 env：循环里 host/target 架构不同，不能依赖外层 process.env
    // （否则两份都会编成外层 env 的架构，文件名却标成不同 triple → 内容与名字不符）。
    { stdio: 'inherit', env: { ...process.env, GOOS: goos, GOARCH: goarch } },
  );
}
