// pnpm release 入口：仅允许在默认分支发版，须工作区干净，且执行前先拉取远端最新
// 校验通过后转发 bumpp（透传额外参数），任一步失败即非零退出。
import { execFileSync } from 'node:child_process';
import { dirname, resolve } from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';

// 仓库根：锚定脚本自身位置，对调用方 CWD 无假设
const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');

// 取 git 命令输出（trim 后）供校验逻辑消费
function gitOutput(args: string[]): string {
  return execFileSync('git', args, { cwd: repoRoot }).toString().trim();
}

// 向用户报错并中止（非零退出，pnpm 层面即报红）
function fail(message: string): never {
  console.error(`✖ ${message}`);
  process.exit(1);
}

const currentBranch = gitOutput(['branch', '--show-current']);
if (!currentBranch) {
  fail('当前处于 detached HEAD，release 需在默认分支上执行');
}

// 默认分支从 origin/HEAD 动态检测，仓库更换默认分支无需改本脚本
let defaultBranch: string;
try {
  defaultBranch = gitOutput(['symbolic-ref', '--short', 'refs/remotes/origin/HEAD']).replace(/^origin\//, '');
} catch {
  fail('无法检测默认分支（origin/HEAD 未设置），请先执行 git remote set-head origin --auto');
}
if (currentBranch !== defaultBranch) {
  fail(`当前分支 ${currentBranch} 不是默认分支 ${defaultBranch}，release 已中止`);
}

// 含未跟踪文件；脏工作区会让 pull/bumpp 中途失败留下半成品状态
if (gitOutput(['status', '--porcelain'])) {
  fail('工作区不干净（存在未提交改动或未跟踪文件），请先提交或清理后再 release');
}

try {
  // 输出直显（stdio 继承）；rebase 避免产生多余 merge commit
  execFileSync('git', ['pull', '--rebase'], { cwd: repoRoot, stdio: 'inherit' });
} catch {
  fail('git pull --rebase 失败，请处理后再重试 release');
}

try {
  // 转发 bumpp（pnpm 运行时已注入 node_modules/.bin 到 PATH），透传额外参数保持交互式选版本可用
  execFileSync('bumpp', process.argv.slice(2), { cwd: repoRoot, stdio: 'inherit' });
} catch {
  fail('bumpp 执行失败');
}
