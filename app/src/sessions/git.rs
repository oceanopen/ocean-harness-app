// git 待提交检测：判断 cwd 是否存在未提交改动（含 untracked）。
//
// 设计：
//   - 用 `git -C <cwd> status --porcelain`，stdout 非空即 dirty
//   - 失败 / 非 git 目录 / git 未安装 / cwd 无效 → false（视作 clean，不显示徽章）
//   - 同步阻塞调用，与 enrich.rs 的 ps_field 风格一致（项目不用 tokio）
//
// 不引入 timeout：std::process::Command 无原生 timeout；idle 会话数少（通常 1-5），
// `git status --porcelain` 通常 <100ms，串行可接受。store::rescan 用 RescanLock
// 互斥，避免 watcher/poll/命令三线程并发跑 git 串行堆积。

use std::path::Path;
use std::process::Command;

/// 判断 cwd 是否为有未提交改动的 git 工作区。
///
/// true = 存在改动（tracked modified/staged 或 untracked 文件）；
/// false = 干净 / 非 git 目录 / git 未安装 / cwd 无效。
///
/// 用 `-C <cwd>` 而非 `Command::current_dir`：让 git 自身报错走 `status.success()`
/// 兜底路径，错误处理统一（current_dir 在 dir 不存在时直接 spawn 失败，分支更多）。
/// 参数顺序必须是 `git -C <cwd> status ...`，`-C` 在子命令之前。
pub fn is_dirty(cwd: &str) -> bool {
    // 防御：cwd 必须是存在的绝对路径。Claude json 写绝对路径，此为脏数据兜底。
    if cwd.is_empty() || !Path::new(cwd).is_absolute() {
        return false;
    }

    let out = match Command::new("git")
        .args(["-C", cwd, "status", "--porcelain"])
        // 非 git 目录 git 会向 stderr 输出 `fatal: not a repository`，
        // 重定向 null 避免污染父进程（ocean-harness 的终端）日志。
        .stderr(std::process::Stdio::null())
        .output()
    {
        Ok(o) => o,
        Err(_) => return false, // git 未安装
    };

    if !out.status.success() {
        return false; // 非 git 目录 / git 内部错误
    }

    // porcelain 每行对应一个改动文件；空 stdout = clean。
    !out.stdout.is_empty()
}
