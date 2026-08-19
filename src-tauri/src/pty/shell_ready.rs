// shell-ready 包装（terminal_01_auto_claude.md 任务 1）：shell 提示符就绪的精确锚定。
//
// 问题：spawn 后盲目注入 `claude\r`（fast 模式 = 首输出 + 30ms）会被慢 rc 文件
// （nvm/pyenv 等）吞掉或 ECHO 双显。解法（orca StartupCommandDelivery 同机制）：
// 包装文件让 shell 在「提示符真正可编辑」那一刻 emit marker（OSC 777），任务 2 的
// barrier 扫描到 marker 才放行排队的 stdin。
//
// 本文件只做三件事（任务 1 范围）：
//   1. marker 字节常量
//   2. zsh（ZDOTDIR 换装）+ bash（--rcfile）两套包装模板与幂等落盘
//   3. ShellReadyScanner（输出流剥除 marker 的流式扫描器雏形）
// barrier（stdin 排队 + flush gate + 超时）与 spawn 链路接入是任务 2（session.rs/
// local_provider.rs），本文件不碰。
//
// 设计取舍对照 orca（详见文档 §3.2/§5.4）：砍 OSC 133、CODEX_HOME 等 restore、
// WSL 路径自检、marker env 门（本项目包装 spawn 恒发 marker）；保留 ZDOTDIR discover、
// HISTFILE 修复（macOS /etc/zshrc 会把 HISTFILE 派生进 wrapper 目录）、widget 链式
// 注册（不能用 add-zle-hook-widget——azhw dispatcher 在前序 hook 非零退出时中断链，
// oh-my-zsh vi-mode 等用户 widget 会让 marker 被吞，orca 实证教训）。

use std::path::{Path, PathBuf};

/// shell 就绪 marker：OSC 777 ; we-term-shell-ready BEL。
/// 与 orca 同协议（OSC 777）但换应用前缀；全 ASCII，无 UTF-8 跨块切分问题。
/// 任务 2 接线：reader 线程扫描剥除 + barrier 放行（当前仅测试消费，暂 allow）。
#[allow(dead_code)]
pub const WE_TERM_SHELL_READY_MARKER: &[u8] = b"\x1b]777;we-term-shell-ready\x07";

/// POSIX 单引号转义：路径烘焙进 shell 文件时的安全引用（'…' 内除单引号外全字面）。
#[allow(dead_code)]
fn quote_posix_single(value: &str) -> String {
    // 单引号以 '\'' 关闭-转义-重开表达
    format!("'{}'", value.replace('\'', "'\\''"))
}

/// zsh 包装四件套内容（.zshenv/.zprofile/.zshrc/.zlogin）。
/// zdotdir 参数 = wrapper 目录（spawn 侧 ZDOTDIR 指向此处）。
/// 任务 2 接线：spawn_fresh 包装分支调用（当前仅测试消费，暂 allow）。
#[allow(dead_code)]
fn zsh_wrapper_files(zdotdir: &Path) -> [(&'static str, String); 4] {
    let dir = zdotdir.to_string_lossy().into_owned();
    let header = "# we-claude-terminal zsh shell-ready wrapper";

    // ---- .zshenv：ZDOTDIR discover（orca #8003 简化版，macOS-only 无 WSL 路径问题）----
    // 顺序：记录 spawn 传入的原始 ZDOTDIR（WE_TERM_ORIG_ZDOTDIR）→ 防递归归一
    //（继承值本身就是 wrapper 目录时回退 $HOME）→ unset ZDOTDIR 后 source 用户
    //.zshenv（顶层 source 保持导出/函数/fpath 正常作用域）→ 读 discover 出的用户
    // ZDOTDIR → 重锚定回 wrapper 目录（字面量烘焙，防 zsh 对环境值 0x84-0x9D 字节
    // 区间的损坏）。
    let zshenv = format!(
        r#"{header}
_we_term_user_zdotdir="${{WE_TERM_ORIG_ZDOTDIR:-$HOME}}"
case "${{_we_term_user_zdotdir%/}}" in
  */shell-ready/zsh) _we_term_user_zdotdir="$HOME" ;;
esac
unset ZDOTDIR
if [[ -f "$_we_term_user_zdotdir/.zshenv" ]]; then
  source "$_we_term_user_zdotdir/.zshenv"
fi
_we_term_discovered="${{ZDOTDIR:-$_we_term_user_zdotdir}}"
case "${{_we_term_discovered%/}}" in
  */shell-ready/zsh) _we_term_discovered="$HOME" ;;
esac
export WE_TERM_ORIG_ZDOTDIR="$_we_term_discovered"
export ZDOTDIR={}
unset _we_term_user_zdotdir _we_term_discovered
"#,
        quote_posix_single(&dir)
    );

    // ---- 透传 source 用户同名文件的公共块 ----
    // source 时临时把 ZDOTDIR 切到用户根（dotfiles 用户的 rc 从 $ZDOTDIR 解析插件/
    // 配置路径），完事还原 wrapper 目录（zsh 还要读后续 wrapper 文件）。
    let source_user_file = |name: &str| {
        format!(
            r#"_we_term_home="${{WE_TERM_ORIG_ZDOTDIR:-$HOME}}"
if [[ -f "$_we_term_home/{name}" ]]; then
  _we_term_wrapper_zdotdir="$ZDOTDIR"
  export ZDOTDIR="$_we_term_home"
  source "$_we_term_home/{name}"
  export ZDOTDIR="$_we_term_wrapper_zdotdir"
  unset _we_term_wrapper_zdotdir
fi
unset _we_term_home
"#
        )
    };

    // ---- widget 注册块（marker 发射点）----
    // 自持 zle-line-init（不用 add-zle-hook-widget，见文件头注释）；链式调用前一个
    // 用户 widget；re-source 防自捕获（发现自己已是绑定 widget 时保留首次捕获的
    // prev，避免把自己捕获成 prev 形成递归）。builtin/completion 形态的既有 widget
    //（zle-line-init 场景罕见）不链式。
    let marker_printf = format!(
        "printf '{}'",
        String::from_utf8_lossy(WE_TERM_SHELL_READY_MARKER)
            .replace('\x1b', "\\033")
            .replace('\x07', "\\007")
    );
    let widget_block = format!(
        r#"if [[ -z "${{widgets[zle-line-init]:-}}" || "${{widgets[zle-line-init]}}" == user:* ]]; then
  if [[ "${{widgets[zle-line-init]:-}}" == "user:__we_term_prompt_mark" ]]; then
    :
  elif (( ${{+widgets[zle-line-init]}} )); then
    __we_term_prev_line_init_fn="${{widgets[zle-line-init]#user:}}"
  else
    __we_term_prev_line_init_fn=""
  fi
  __we_term_prompt_mark() {{
    {marker_printf}
    if [[ -n "${{__we_term_prev_line_init_fn:-}}" ]]; then
      "${{__we_term_prev_line_init_fn}}" "$@"
    fi
  }}
  zle -N zle-line-init __we_term_prompt_mark
fi
"#
    );

    // ---- HISTFILE 修复（orca #11044）----
    // macOS /etc/zshrc 无条件 HISTFILE=${ZDOTDIR:-$HOME}/.zsh_history，ZDOTDIR 指向
    // wrapper 目录时历史会写进 app 数据目录。仅当仍为 wrapper 派生值时恢复用户
    // 路径（用户 rc 里 deliberate 设置的 HISTFILE 不碰）。
    let histfile_fix = r#"if [[ "${HISTFILE:-}" == "$ZDOTDIR/.zsh_history" ]]; then
  HISTFILE="${WE_TERM_ORIG_ZDOTDIR:-$HOME}/.zsh_history"
fi
"#;

    // ---- .zlogin 尾部：恢复用户 ZDOTDIR（登录 shell 与普通启动暴露一致的 ZDOTDIR）----
    let zdotdir_restore = r#"_we_term_home="${WE_TERM_ORIG_ZDOTDIR:-$HOME}"
case "${_we_term_home%/}" in
  */shell-ready/zsh) _we_term_home="$HOME" ;;
esac
export ZDOTDIR="$_we_term_home"
unset _we_term_home
"#;

    let zprofile = format!("{header}\n{}", source_user_file(".zprofile"));
    let zshrc = format!(
        "{header}\n{src_rc}{histfile_fix}\n{widget}",
        src_rc = source_user_file(".zshrc"),
        histfile_fix = histfile_fix,
        widget = widget_block
    );
    // widget 双放 .zshrc 与 .zlogin：-i 与 -l 两种 spawn 模式（zsh 交互登录 shell
    // 读 .zshrc + .zlogin，非登录交互只读 .zshrc）都能发 marker。.zlogin 尾部再
    // 恢复用户 ZDOTDIR。
    let zlogin = format!(
        "{header}\n{src_login}{widget}\n{restore}",
        src_login = source_user_file(".zlogin"),
        widget = widget_block,
        restore = zdotdir_restore
    );

    [
        (".zshenv", zshenv),
        (".zprofile", zprofile),
        (".zshrc", zshrc),
        (".zlogin", zlogin),
    ]
}

/// bash 包装 rcfile 内容。--rcfile 模式下 bash 跳过 /etc/profile 与 ~/.bash_profile
/// 系，需自 source 模拟 login 语义；marker 前置进 PROMPT_COMMAND（保留用户已有）。
#[allow(dead_code)]
fn bash_rcfile() -> String {
    let marker_printf = format!(
        "printf '{}'",
        String::from_utf8_lossy(WE_TERM_SHELL_READY_MARKER)
            .replace('\x1b', "\\033")
            .replace('\x07', "\\007")
    );
    format!(
        r#"# we-claude-terminal bash shell-ready wrapper
[[ -f /etc/profile ]] && source /etc/profile
if [[ -f "$HOME/.bash_profile" ]]; then
  source "$HOME/.bash_profile"
elif [[ -f "$HOME/.bash_login" ]]; then
  source "$HOME/.bash_login"
elif [[ -f "$HOME/.profile" ]]; then
  source "$HOME/.profile"
fi
# 开 bracketed paste：多行注入（未来扩展）作为单次字面粘贴送达；老 readline 构建
# 默认关闭，会把内嵌换行当回车。现代 readline 恒真，此行防御式。
[[ $- == *i* ]] && bind 'set enable-bracketed-paste on' 2>/dev/null
__we_term_precmd() {{
  local exit_code=$?
  {marker_printf}
  return "$exit_code"
}}
if [[ $(declare -p PROMPT_COMMAND 2>/dev/null) == "declare -a"* ]]; then
  PROMPT_COMMAND=(__we_term_precmd "${{PROMPT_COMMAND[@]}}")
else
  PROMPT_COMMAND="__we_term_precmd${{PROMPT_COMMAND:+;$PROMPT_COMMAND}}"
fi
"#
    )
}

/// 包装文件落盘结果：spawn 侧据此设 ZDOTDIR / --rcfile。
#[derive(Debug, Clone)]
#[allow(dead_code)]
pub struct ShellReadyWrappers {
    /// zsh wrapper 目录（spawn 侧 env ZDOTDIR 指向此处）。
    pub zsh_zdotdir: PathBuf,
    /// bash rcfile 路径（spawn 侧 --rcfile 参数）。
    pub bash_rcfile: PathBuf,
}

/// 幂等生成包装文件到 base/shell-ready/（文档 §3.2：首次 pty_spawn 带
/// startup_command 时按需生成；已存在不覆写——用户可自查/手改这些文件）。
/// 单文件写失败仅跳过该文件（其余照常）：最坏情况该 shell 无 marker，任务 2 的
/// 超时兜底会强制放行，PTY 仍可用（orca 同策略）。
#[allow(dead_code)]
pub fn ensure_shell_ready_wrappers(base: &Path) -> ShellReadyWrappers {
    let root = base.join("shell-ready");
    let zsh_dir = root.join("zsh");
    let bash_dir = root.join("bash");

    for (name, content) in zsh_wrapper_files(&zsh_dir) {
        write_once(&zsh_dir.join(name), &content);
    }
    write_once(&bash_dir.join("rcfile"), &bash_rcfile());

    ShellReadyWrappers {
        zsh_zdotdir: zsh_dir,
        bash_rcfile: bash_dir.join("rcfile"),
    }
}

/// 已存在不覆写；写失败记日志不中断（见 ensure_shell_ready_wrappers 注释）。
#[allow(dead_code)]
fn write_once(path: &Path, content: &str) {
    if path.exists() {
        return;
    }
    if let Some(parent) = path.parent() {
        if let Err(e) = std::fs::create_dir_all(parent) {
            log::warn!(
                "[pty] shell-ready mkdir {} failed: {e}",
                path.display()
            );
            return;
        }
    }
    if let Err(e) = std::fs::write(path, content) {
        log::warn!(
            "[pty] shell-ready write {} failed: {e}",
            path.display()
        );
    }
}

/// 输出流 marker 扫描器（任务 2 挂进 reader 线程，Utf8Tail 之后）。
/// 命中前：扫描剥除 marker（不推前端/ring）；持尾部缓冲防止 marker 恰被 8KB 读块
/// 边界切断（保留可能的前缀字节，最多 marker.len()-1）。命中后直通（每会话至多
/// 一次注入，之后的输出与 marker 无关）。
#[allow(dead_code)]
pub struct ShellReadyScanner {
    /// 尾部缓冲：最近 ≤ marker.len()-1 字节（可能构成 marker 前缀的部分）。
    tail: Vec<u8>,
    /// 是否已命中（命中后 scan 直通）。
    done: bool,
}

impl ShellReadyScanner {
    #[allow(dead_code)]
    pub fn new() -> Self {
        Self {
            tail: Vec::new(),
            done: false,
        }
    }

    /// 扫描一块输出：返回 (剥除 marker 后的输出, 本块是否命中 marker)。
    /// 输出可能为空（整块都是 marker 或 marker 前缀挂起）。
    #[allow(dead_code)]
    pub fn scan(&mut self, chunk: &[u8]) -> (Vec<u8>, bool) {
        if self.done {
            return (chunk.to_vec(), false);
        }
        let mut buf = std::mem::take(&mut self.tail);
        buf.extend_from_slice(chunk);
        // 扫描全部候选起点：命中即从 buf 头剥除 marker、直通后续（清 tail）。
        if let Some(pos) = find_subslice(&buf, WE_TERM_SHELL_READY_MARKER) {
            self.done = true;
            self.tail.clear();
            let mut out = Vec::with_capacity(buf.len());
            out.extend_from_slice(&buf[..pos]);
            out.extend_from_slice(&buf[pos + WE_TERM_SHELL_READY_MARKER.len()..]);
            return (out, true);
        }
        // 未命中：保留可能构成 marker 前缀的最长尾部（≤ marker.len()-1 字节），
        // 其余放行。marker 全 ASCII，切在 ASCII 边界不会破坏 UTF-8 序列。
        let keep = self.keep_len(&buf);
        let split_at = buf.len() - keep;
        let out = buf[..split_at].to_vec();
        self.tail = buf[split_at..].to_vec();
        (out, false)
    }

    /// buf 尾部有多少字节可能是 marker 的前缀（最长 marker.len()-1：整段等于
    /// marker 的话上面已命中；buf 比 marker 短时整段都可能是前缀，须全保留）。
    #[allow(dead_code)]
    fn keep_len(&self, buf: &[u8]) -> usize {
        let m = WE_TERM_SHELL_READY_MARKER;
        for keep in (1..m.len().min(buf.len())).rev() {
            if m.starts_with(&buf[buf.len() - keep..]) {
                return keep;
            }
        }
        // buf 整段是 marker 前缀（buf.len() < marker.len()）：全保留挂起。
        if buf.len() < m.len() && m.starts_with(buf) {
            return buf.len();
        }
        0
    }
}

impl Default for ShellReadyScanner {
    fn default() -> Self {
        Self::new()
    }
}

/// 子序列查找（std 无 bytes::find 的借用版本替代：windows 匹配即可，数据量小）。
#[allow(dead_code)]
fn find_subslice(haystack: &[u8], needle: &[u8]) -> Option<usize> {
    if needle.is_empty() || haystack.len() < needle.len() {
        return None;
    }
    haystack
        .windows(needle.len())
        .position(|w| w == needle)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Read;
    use std::time::{Duration, Instant};

    // ---------- scanner 单测 ----------

    /// 单块完整 marker：剥除命中，前后字节保留。
    #[test]
    fn scanner_hits_marker_in_single_chunk() {
        let mut s = ShellReadyScanner::new();
        let chunk = [
            b"prompt> ".as_slice(),
            WE_TERM_SHELL_READY_MARKER,
            b"\r\n".as_slice(),
        ]
        .concat();
        let (out, hit) = s.scan(&chunk);
        assert!(hit);
        assert_eq!(out, b"prompt> \r\n");
        // 命中后直通
        let (out2, hit2) = s.scan(b"more\x1b]777;we-term-shell-ready\x07");
        assert!(!hit2);
        assert_eq!(out2, b"more\x1b]777;we-term-shell-ready\x07");
    }

    /// marker 跨块切断（每 3 字节一块喂入）：仍命中且输出不含 marker 残段。
    #[test]
    fn scanner_hits_marker_split_across_chunks() {
        let mut s = ShellReadyScanner::new();
        let data = [b"before", WE_TERM_SHELL_READY_MARKER, b"after"].concat();
        let mut hit_at = None;
        let mut collected = Vec::new();
        for chunk in data.chunks(3) {
            let (out, hit) = s.scan(chunk);
            collected.extend_from_slice(&out);
            if hit {
                hit_at = Some(collected.len());
            }
        }
        assert_eq!(collected, b"beforeafter");
        assert!(hit_at.is_some(), "跨块 marker 应命中");
    }

    /// marker 首字节 ESC 前缀挂起：其他 OSC/转义序列不误吞。
    #[test]
    fn scanner_passes_non_marker_escapes() {
        let mut s = ShellReadyScanner::new();
        // \x1b[?2004h（bracketed paste 开）与 OSC 0 title 序列照常放行
        let (out, hit) = s.scan(b"\x1b[?2004h\x1b]0;title\x07ok");
        assert!(!hit);
        assert_eq!(out, b"\x1b[?2004h\x1b]0;title\x07ok");
        // 尾部恰为 marker 前缀（\x1b]777;）时挂起，下一块补全或不补全
        let (out2, _) = s.scan(b"\x1b]777;");
        assert!(out2.is_empty(), "疑似前缀应挂起不放行");
        let (out3, hit3) = s.scan(b"we-term-shell-ready\x07tail");
        assert!(hit3);
        assert_eq!(out3, b"tail");
    }

    // ---------- wrapper 生成单测 ----------

    /// 生成五文件齐全；再次调用不覆写（手改内容保留）。
    #[test]
    fn wrappers_idempotent_no_overwrite() {
        let base = std::env::temp_dir().join("we-term-shell-ready-test-idem");
        let _ = std::fs::remove_dir_all(&base);
        let w = ensure_shell_ready_wrappers(&base);
        for f in [
            w.zsh_zdotdir.join(".zshenv"),
            w.zsh_zdotdir.join(".zprofile"),
            w.zsh_zdotdir.join(".zshrc"),
            w.zsh_zdotdir.join(".zlogin"),
            w.bash_rcfile.clone(),
        ] {
            assert!(f.is_file(), "missing {}", f.display());
        }

        // 手改 .zshrc 后再生成：内容保留（幂等不覆写）。
        let rc = w.zsh_zdotdir.join(".zshrc");
        std::fs::write(&rc, "# user edited").unwrap();
        ensure_shell_ready_wrappers(&base);
        assert_eq!(
            std::fs::read_to_string(&rc).unwrap(),
            "# user edited"
        );
        let _ = std::fs::remove_dir_all(&base);
    }

    /// 模板内容断言：marker printf、widget 链式、HISTFILE 修复、ZDOTDIR discover
    /// 关键行均在（防后续重构悄悄丢行为）。
    #[test]
    fn wrapper_templates_contain_key_blocks() {
        let esc_marker = "\\033]777;we-term-shell-ready\\007";
        let base = std::env::temp_dir().join("we-term-shell-ready-test-tpl");
        let _ = std::fs::remove_dir_all(&base);
        let w = ensure_shell_ready_wrappers(&base);
        let read = |p: &Path| std::fs::read_to_string(p).unwrap();
        let zshrc = read(&w.zsh_zdotdir.join(".zshrc"));
        assert!(zshrc.contains(&format!("printf '{esc_marker}'")));
        assert!(zshrc.contains("zle -N zle-line-init __we_term_prompt_mark"));
        assert!(zshrc.contains("__we_term_prev_line_init_fn"));
        assert!(zshrc.contains("HISTFILE="));
        let zshenv = read(&w.zsh_zdotdir.join(".zshenv"));
        assert!(zshenv.contains("WE_TERM_ORIG_ZDOTDIR"));
        assert!(zshenv.contains("*/shell-ready/zsh)"));
        let rcfile = read(&w.bash_rcfile);
        assert!(rcfile.contains("__we_term_precmd"));
        assert!(rcfile.contains("PROMPT_COMMAND="));
        let _ = std::fs::remove_dir_all(&base);
    }

    // ---------- PTY 集成：真 shell 跑包装文件 ----------

    /// 轮询读 master 输出直到含期望字节或超时。
    fn wait_output(reader: &mut dyn Read, want: &[u8], timeout: Duration) -> bool {
        let deadline = Instant::now() + timeout;
        let mut acc = Vec::new();
        let mut buf = [0u8; 4096];
        while Instant::now() < deadline {
            match reader.read(&mut buf) {
                Ok(0) => break,
                Ok(n) => {
                    acc.extend_from_slice(&buf[..n]);
                    if find_subslice(&acc, want).is_some() {
                        return true;
                    }
                }
                Err(_) => break,
            }
        }
        false
    }

    /// zsh -l 跑 ZDOTDIR 包装：输出含 marker（临时 HOME 隔离用户 rc，防本机
    /// dotfiles 噪声/交互阻塞）。
    #[test]
    fn zsh_wrapper_emits_marker_in_pty() {
        let base = std::env::temp_dir().join("we-term-shell-ready-test-zsh");
        let _ = std::fs::remove_dir_all(&base);
        let home = base.join("home");
        std::fs::create_dir_all(&home).unwrap();
        let w = ensure_shell_ready_wrappers(&base);

        let pty_system = portable_pty::native_pty_system();
        let pair = pty_system
            .openpty(portable_pty::PtySize {
                rows: 24,
                cols: 80,
                pixel_width: 0,
                pixel_height: 0,
            })
            .unwrap();
        let mut cmd = portable_pty::CommandBuilder::new("/bin/zsh");
        cmd.args(["-l"]);
        cmd.env("ZDOTDIR", &w.zsh_zdotdir);
        cmd.env("HOME", &home);
        cmd.env("TERM", "xterm-256color");
        cmd.cwd(&home);

        let _child = pair.slave.spawn_command(cmd).expect("spawn zsh");
        let mut reader = pair.master.try_clone_reader().unwrap();
        let ok = wait_output(
            &mut reader,
            WE_TERM_SHELL_READY_MARKER,
            Duration::from_secs(8),
        );
        // 喂 exit 收尾（测试进程不陪挂）
        if let Ok(mut writer) = pair.master.take_writer() {
            let _ = writer.write_all(b"exit\n");
        }
        drop(pair.master);
        assert!(ok, "zsh 包装 spawn 未发出 shell-ready marker");
        let _ = std::fs::remove_dir_all(&base);
    }

    /// bash --rcfile 跑包装：输出含 marker。
    #[test]
    fn bash_wrapper_emits_marker_in_pty() {
        let base = std::env::temp_dir().join("we-term-shell-ready-test-bash");
        let _ = std::fs::remove_dir_all(&base);
        let home = base.join("home");
        std::fs::create_dir_all(&home).unwrap();
        let w = ensure_shell_ready_wrappers(&base);

        let pty_system = portable_pty::native_pty_system();
        let pair = pty_system
            .openpty(portable_pty::PtySize {
                rows: 24,
                cols: 80,
                pixel_width: 0,
                pixel_height: 0,
            })
            .unwrap();
        let mut cmd = portable_pty::CommandBuilder::new("/bin/bash");
        cmd.arg("--rcfile");
        cmd.arg(&w.bash_rcfile);
        cmd.env("HOME", &home);
        cmd.env("TERM", "xterm-256color");
        cmd.cwd(&home);

        let _child = pair.slave.spawn_command(cmd).expect("spawn bash");
        let mut reader = pair.master.try_clone_reader().unwrap();
        let ok = wait_output(
            &mut reader,
            WE_TERM_SHELL_READY_MARKER,
            Duration::from_secs(8),
        );
        if let Ok(mut writer) = pair.master.take_writer() {
            let _ = writer.write_all(b"exit\n");
        }
        drop(pair.master);
        assert!(ok, "bash 包装 spawn 未发出 shell-ready marker");
        let _ = std::fs::remove_dir_all(&base);
    }
}
