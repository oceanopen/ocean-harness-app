// LocalPtyProvider：本机 PTY 后端（portable-pty）。
//
// spawn 细节（spike 实证，见 docs/embedded_terminal.md）：
//   - shell 显式取 $SHELL 回退 /bin/zsh，加 -i（交互式）。不能用
//     CommandBuilder::new_default_prog()——它在无 tty 的环境（测试/某些启动上下文）
//     会永久挂起。
//   - cwd 不存在时 spawn_command 直接报错，由前端捕获展示「任务目录不存在」。
//
// spawn 即启动 reader 线程（session::spawn_reader_thread）：输出经 UTF-8 边界切分后
// 推 listener Channel；EOF 置 exited + Exit 事件。重复 spawn 语义（§3.4/§5.2）：
//   - 未退出会话：复用，换装新 listener（webview 刷新后旧 Channel 失效）
//   - 已退出会话：移除旧会话重起 shell（前端「重开」按钮路径）

use std::path::PathBuf;
use std::sync::Arc;
use std::sync::atomic::Ordering;

use portable_pty::{CommandBuilder, PtySize, native_pty_system};
use tauri::ipc::Channel;

use super::provider::{PtyProvider, PtyReattached, PtySessionInfo, PtySpawned, SpawnOpts};
use super::session::{PtyEvent, PtySession, SessionIo, spawn_reader_thread};
use super::shell_ready::{SHELL_READY_TIMEOUT_MS, ShellReadyBarrier, ensure_shell_ready_wrappers};
use super::state::PtySessionStore;

/// shell-ready 包装文件根（app_data_dir）。setup 时注入（lib.rs），未注入时
/// startup_command 降级为不注入（裸 spawn），不阻塞终端可用性。
fn app_data_dir() -> Option<&'static PathBuf> {
    APP_DATA_DIR.get()
}

/// setup 注入 app_data_dir（lib.rs 调用，一次性）。测试可先行注入临时目录。
static APP_DATA_DIR: std::sync::OnceLock<PathBuf> = std::sync::OnceLock::new();

pub fn set_app_data_dir(dir: PathBuf) {
    let _ = APP_DATA_DIR.set(dir);
}

/// 本机 PTY 后端。持有全局会话存储；provider 实例本身无状态，
/// 保留 store 字段以便远程 provider 扩展时替换为连接级存储。
pub struct LocalPtyProvider {
    pub store: PtySessionStore,
}

impl LocalPtyProvider {
    pub fn new() -> Self {
        Self {
            store: PtySessionStore::default(),
        }
    }

    /// 会话存储访问（spawn 写入侧的同一实例）。命令查询走此处而非
    /// State<PtySessionStore>（app.manage 的另一实例——曾致 probe 恒 not found）。
    pub fn store(&self) -> &PtySessionStore {
        &self.store
    }

    /// 起一个新 shell 会话并入库（不检查已存在——调用方 spawn 决定复用/重起）。
    fn spawn_fresh(
        &self,
        opts: &SpawnOpts,
        listener: Channel<PtyEvent>,
    ) -> Result<PtySpawned, String> {
        let pty_system = native_pty_system();
        let pair = pty_system
            .openpty(PtySize {
                rows: opts.rows,
                cols: opts.cols,
                pixel_width: 0,
                pixel_height: 0,
            })
            .map_err(|e| format!("openpty failed: {e}"))?;

        let (prog, args) = resolve_shell();
        let mut cmd = CommandBuilder::new(&prog);
        for a in &args {
            cmd.arg(a);
        }
        cmd.cwd(&opts.cwd);

        // startup_command 分支（任务 2/5）：shell 为 zsh/bash 且 app_data_dir 已注入
        // → 包装 spawn（marker 精确锚定 + barrier）；其余情形（fish/其他 shell、
        // 或包装文件根不可用）统一降级 fast 注入——首个非空输出块 + 30ms 放行，
        // 排队/超时/退出语义复用 barrier（文档 §3.2「行为降级不阻塞」）。
        let mut barrier: Option<Arc<ShellReadyBarrier>> = None;
        if let Some(startup) = &opts.startup_command {
            let shell_name = shell_basename(&prog);
            let base = app_data_dir();
            let wrapped = matches!(shell_name.as_str(), "zsh" | "bash") && base.is_some();
            if wrapped {
                let base = base.expect("checked above");
                let wrappers = ensure_shell_ready_wrappers(base);
                match shell_name.as_str() {
                    "zsh" => {
                        // -l 登录式（与 Terminal.app 一致，PATH 等环境完整）；
                        // ZDOTDIR 指向 wrapper 目录，原始 ZDOTDIR（非 wrapper 目录
                        // 时）透传给包装 .zshenv 防嵌套丢配置。
                        cmd = CommandBuilder::new(&prog);
                        cmd.arg("-l");
                        cmd.env("ZDOTDIR", &wrappers.zsh_zdotdir);
                        if let Some(orig) = user_zdotdir_for_passthrough() {
                            cmd.env("WE_TERM_ORIG_ZDOTDIR", orig);
                        } else {
                            cmd.env_remove("ZDOTDIR".to_string().as_str());
                        }
                        cmd.cwd(&opts.cwd);
                    }
                    "bash" => {
                        cmd = CommandBuilder::new(&prog);
                        cmd.arg("--rcfile");
                        cmd.arg(&wrappers.bash_rcfile);
                        cmd.cwd(&opts.cwd);
                    }
                    _ => unreachable!(),
                }
                let b = ShellReadyBarrier::new(SHELL_READY_TIMEOUT_MS);
                b.enqueue_startup(startup);
                barrier = Some(b);
                log::info!(
                    "[pty] shell-ready wrapped spawn session_id={} shell={}",
                    opts.session_id,
                    shell_name
                );
            } else {
                // fast 回退：fish 等无 marker 包装的 shell，或 zsh/bash 但
                // app_data_dir 未注入（包装文件无处落盘）。裸 spawn + fast barrier。
                let b = ShellReadyBarrier::new_fast(SHELL_READY_TIMEOUT_MS);
                b.enqueue_startup(startup);
                barrier = Some(b);
                log::warn!(
                    "[pty] shell-ready fast fallback session_id={} shell={}",
                    opts.session_id,
                    shell_name
                );
            }
        }

        // claude 归因链 env 打标（T1.4）：pane 锚点 / spool 通道 / spawn 代际标。
        // 无条件注入（与 chat 模式开关无关）——未装 hook 脚本时 env 无人消费，无害；
        // 模式热开启后已运行会话的下一个 hook 事件即可被归因。
        // 注入点必须在包装分支之后：zsh/bash 分支重建 CommandBuilder，之前的
        // env 会被剥掉；在此处注入则 4 条 spawn 路径（裸/包装/fast）一处全覆盖。
        for (k, v) in claude_attribution_envs(&opts.session_id) {
            cmd.env(k, v);
        }

        let child = pair
            .slave
            .spawn_command(cmd)
            .map_err(|e| format!("spawn shell in {} failed: {e}", opts.cwd))?;
        let pid = child.process_id().unwrap_or(0);
        let reader = pair
            .master
            .try_clone_reader()
            .map_err(|e| format!("clone pty reader failed: {e}"))?;
        let writer = pair
            .master
            .take_writer()
            .map_err(|e| format!("take pty writer failed: {e}"))?;

        let io = Arc::new(SessionIo::new());
        io.set_listener(listener.clone());
        let started_at = chrono::Utc::now().timestamp_millis();
        let mut session = PtySession::new(
            opts.session_id.clone(),
            opts.cwd.clone(),
            pair.master,
            writer,
            child,
            Arc::clone(&io),
            started_at,
        );
        // barrier 接线：writer 共享句柄给 flush 线程；会话挂 barrier；reader 扫描。
        // 顺序关键——install_writer 须在 spawn_reader_thread 前（marker 可能极快）。
        if let Some(b) = &barrier {
            b.install_writer(Arc::clone(&session.writer));
            session.barrier = Some(Arc::clone(b));
        }
        spawn_reader_thread(reader, io, barrier);

        {
            let mut map = self
                .store
                .0
                .lock()
                .expect("PtySessionStore mutex poisoned");
            // 二次确认：并发 spawn 同 issueId 的败者 kill 刚起的 shell 让位，不覆盖先入会话，
            // 但把现有会话的 listener 换成自己的——败者（如 StrictMode 第二遍挂载）才是
            // 存活的前端，不换装则现有会话持续向已销毁的旧 Channel 推流（数据全丢）。
            if let Some(existing) = map.get_mut(&opts.session_id) {
                if !existing.exited() {
                    existing.io.set_listener(listener.clone());
                    let _ = session.shutdown();
                    return Ok(self.snapshot(existing));
                }
            }
            map.insert(opts.session_id.clone(), session);
        }

        log::info!(
            "[pty] spawned session session_id={} pid={} cwd={}",
            opts.session_id,
            pid,
            opts.cwd
        );
        Ok(PtySpawned {
            session_id: opts.session_id.clone(),
            cwd: opts.cwd.clone(),
            pid,
            started_at,
            fresh: true,
            scrollback: String::new(),
        })
    }

    /// 从现有会话生成快照荷载（scrollback 由调用方按需覆写）。
    fn snapshot(&self, s: &PtySession) -> PtySpawned {
        PtySpawned {
            session_id: s.session_id.clone(),
            cwd: s.cwd.clone(),
            pid: s
                .child
                .try_lock()
                .map(|c| c.process_id().unwrap_or(0))
                .unwrap_or(0),
            started_at: s.started_at,
            fresh: false,
            scrollback: String::new(),
        }
    }
}

impl Default for LocalPtyProvider {
    fn default() -> Self {
        Self::new()
    }
}

/// 解析用户 shell：$SHELL 优先，缺失回退 /bin/zsh（macOS 默认）。
/// 返回 (程序, 参数)——交互式 shell 需要 -i。
fn resolve_shell() -> (String, Vec<String>) {
    let shell = std::env::var("SHELL").unwrap_or_else(|_| "/bin/zsh".to_string());
    (shell, vec!["-i".to_string()])
}

/// shell 程序 basename（小写）：zsh/bash/fish… 包装分支判定用。
fn shell_basename(prog: &str) -> String {
    prog.rsplit('/')
        .next()
        .unwrap_or(prog)
        .to_lowercase()
}

/// 用户原始 ZDOTDIR（app 进程环境）：非空且非 shell-ready wrapper 目录时透传给
/// 包装 .zshenv（防嵌套场景丢用户自定义 ZDOTDIR）；否则 None（包装内回退 $HOME）。
fn user_zdotdir_for_passthrough() -> Option<String> {
    let v = std::env::var("ZDOTDIR").ok()?;
    let normalized = v.trim_end_matches('/');
    if normalized.is_empty() || normalized.ends_with("/shell-ready/zsh") {
        return None;
    }
    Some(v)
}

/// claude 归因链 env 三标（T1.4），打通「pane → claude → hook 脚本」：
///   - WE_TERM_PANE：`issueId::paneId` 锚点（hook 脚本据此命名 spool 文件）
///   - WE_TERM_LAUNCH_TOKEN：每次 spawn 新 uuid——代际标，ingest 围栏据此丢弃
///     同 pane 上一代 claude 的迟到事件（issueId 跨 spawn 不变，无法担此职）
///   - WE_TERM_SPOOL_DIR：spool 通道目标（app_data_dir/claude-spool）；仅当
///     app_data_dir 已注入时给出（生产 setup 恒在场；缺席时 hook 脚本 env
///     guard 直接 exit 0，天然 no-op）
fn claude_attribution_envs(session_id: &str) -> Vec<(&'static str, String)> {
    use crate::claude_runtime::script::{ENV_LAUNCH_TOKEN, ENV_PANE, ENV_SPOOL_DIR};
    let mut envs = vec![
        (ENV_PANE, session_id.to_string()),
        (ENV_LAUNCH_TOKEN, uuid::Uuid::new_v4().to_string()),
    ];
    if let Some(base) = app_data_dir() {
        envs.push((
            ENV_SPOOL_DIR,
            base.join(crate::claude_runtime::script::SPOOL_DIR_NAME)
                .to_string_lossy()
                .into_owned(),
        ));
    }
    envs
}

impl PtyProvider for LocalPtyProvider {
    /// 启动会话（幂等）：未退出复用 + 换装 listener；已退出移除重起（重开语义）。
    fn spawn(&self, opts: SpawnOpts, listener: Channel<PtyEvent>) -> Result<PtySpawned, String> {
        // 目录预检：portable-pty 对不存在的 cwd 不报错而是静默回退到父进程 cwd
        // （实测 shell 会起在家目录），违背「spawn 失败自然暴露」的设计预期。
        // 显式校验让前端走「任务目录不存在」错误态。
        if !std::path::Path::new(&opts.cwd).is_dir() {
            return Err(format!("任务目录不存在：{}", opts.cwd));
        }
        let existing = {
            let map = self
                .store
                .0
                .lock()
                .expect("PtySessionStore mutex poisoned");
            map.get(&opts.session_id).map(|s| !s.exited())
        };
        match existing {
            // 未退出：复用。同一临界区内快照 ring + 换装 listener——StrictMode 双挂载
            // 场景第二遍 spawn 复用会话时，prompt 等早期输出已随第一遍（已死的）
            // listener 丢弃，须回放 ring 才不空白。
            Some(true) => {
                let mut map = self
                    .store
                    .0
                    .lock()
                    .expect("PtySessionStore mutex poisoned");
                if let Some(session) = map.get_mut(&opts.session_id) {
                    let (scrollback, exited) = session.io.reattach(listener);
                    debug_assert!(!exited, "未退出分支不会 exited");
                    let mut spawned = self.snapshot(session);
                    spawned.scrollback = scrollback;
                    return Ok(spawned);
                }
                // 惊人罕见：刚才还在、此刻没了（并发 shutdown）——走全新 spawn。
                self.spawn_fresh(&opts, listener)
            }
            // 已退出：移除旧会话（kill 兜底）后重起。
            Some(false) => {
                {
                    let mut map = self
                        .store
                        .0
                        .lock()
                        .expect("PtySessionStore mutex poisoned");
                    if let Some(session) = map.remove(&opts.session_id) {
                        let _ = session.shutdown();
                    }
                }
                self.spawn_fresh(&opts, listener)
            }
            // 不存在：全新 spawn。
            None => self.spawn_fresh(&opts, listener),
        }
    }

    fn write(&self, id: &str, data: &[u8]) -> Result<(), String> {
        self.with_session(id, &mut |s| s.write_input(data))?
    }

    fn resize(&self, id: &str, cols: u16, rows: u16) -> Result<(), String> {
        self.with_session(id, &mut |s| s.resize(cols, rows))?
    }

    fn shutdown(&self, id: &str) -> Result<(), String> {
        let mut map = self
            .store
            .0
            .lock()
            .expect("PtySessionStore mutex poisoned");
        match map.remove(id) {
            Some(session) => {
                let session_id = session.session_id.clone();
                session.shutdown()?;
                log::info!("[pty] shutdown session session_id={}", session_id);
                Ok(())
            }
            None => Ok(()), // 幂等：不存在视为已关闭
        }
    }

    fn shutdown_issue(&self, issue_id: &str) -> Result<usize, String> {
        // 锁内收集命中 key（避免持锁 kill——shutdown 可能阻塞在 waitpid 路径）。
        let prefix = format!("{issue_id}::");
        let hits: Vec<String> = {
            let map = self
                .store
                .0
                .lock()
                .expect("PtySessionStore mutex poisoned");
            map.keys()
                .filter(|k| k.starts_with(&prefix))
                .cloned()
                .collect()
        };
        // 逐个走 shutdown（锁重入安全：shutdown 自持锁）。
        for key in &hits {
            self.shutdown(key)?;
        }
        log::info!(
            "[pty] shutdown_issue {} closed {} sessions",
            issue_id,
            hits.len()
        );
        Ok(hits.len())
    }

    fn exists(&self, id: &str) -> bool {
        let map = self
            .store
            .0
            .lock()
            .expect("PtySessionStore mutex poisoned");
        map.contains_key(id)
    }

    fn reattach(
        &self,
        id: &str,
        listener: Channel<PtyEvent>,
    ) -> Result<Option<PtyReattached>, String> {
        let mut listener = Some(listener);
        // 不存在返回 Ok(None)（前端转 spawn），与「会话不存在」的 Err 语义区分。
        self.with_session(id, &mut |s| {
            let (scrollback, exited) = s.io.reattach(
                listener
                    .take()
                    .expect("reattach listener consumed once"),
            );
            Some(PtyReattached {
                session_id: s.session_id.clone(),
                exited,
                scrollback,
            })
        })
        .or(Ok(None))
    }

    fn list(&self) -> Vec<PtySessionInfo> {
        let map = self
            .store
            .0
            .lock()
            .expect("PtySessionStore mutex poisoned");
        map.values()
            .map(|s| PtySessionInfo {
                session_id: s.session_id.clone(),
                cwd: s.cwd.clone(),
                pid: s
                    .child
                    .try_lock()
                    .map(|c| c.process_id().unwrap_or(0))
                    .unwrap_or(0),
                exited: s.io.exited.load(Ordering::SeqCst),
                started_at: s.started_at,
            })
            .collect()
    }

    fn with_session<T>(
        &self,
        id: &str,
        f: &mut dyn FnMut(&mut PtySession) -> T,
    ) -> Result<T, String> {
        let mut map = self
            .store
            .0
            .lock()
            .expect("PtySessionStore mutex poisoned");
        map.get_mut(id)
            .map(f)
            .ok_or_else(|| format!("pty session {id} not found"))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::{Duration, Instant};

    /// Channel 不可在测试外构造（on_message 绑 webview eval），测试用裸 on_message 构造
    ///（send 走 no-op 丢弃）；输出断言统一走 ring snapshot，不与 reader 线程抢 fd。
    /// 冒烟：spawn → 幂等复用（fresh=false）→ 写 echo → resize → ring 见回显 → shutdown 清空。
    #[test]
    fn spawn_write_read_shutdown() {
        let provider = LocalPtyProvider::new();
        let tmp = std::env::temp_dir().join("pty-smoke-test");
        std::fs::create_dir_all(&tmp).unwrap();

        let session_id = "smoke-test-issue".to_string();
        let opts = SpawnOpts {
            session_id: session_id.clone(),
            cwd: tmp.to_string_lossy().into_owned(),
            cols: 80,
            rows: 24,
            startup_command: None,
        };

        // 无 webview 环境 Channel 以裸 id 构造（send 会失败，但换装/复用逻辑可验证）。
        let spawned = provider
            .spawn(opts.clone(), Channel::new(|_| Ok(())))
            .unwrap();
        assert!(spawned.fresh, "首次 spawn 应是新会话");

        // 幂等：未退出会话重复 spawn → 复用（fresh=false）。
        let again = provider
            .spawn(opts.clone(), Channel::new(|_| Ok(())))
            .unwrap();
        assert!(!again.fresh, "重复 spawn 应复用现有会话");
        assert_eq!(again.session_id, session_id);
        assert_eq!(provider.list().len(), 1);

        provider
            .write(&session_id, b"echo PTY_SMOKE_OK\r\n")
            .unwrap();
        provider.resize(&session_id, 100, 30).unwrap();

        // 不再 clone 第二个 reader（与 reader 线程竞争同一 fd 会各读走一半字节）。
        // 输出断言改走 ring：reader 线程把全部输出入 ring，轮询 snapshot 直到含回显。
        let deadline = Instant::now() + Duration::from_secs(8);
        let mut out = String::new();
        while Instant::now() < deadline {
            out = provider
                .with_session(&session_id, &mut |s| s.io.snapshot())
                .unwrap();
            if out.contains("PTY_SMOKE_OK") {
                break;
            }
            std::thread::sleep(Duration::from_millis(100));
        }
        assert!(
            out.contains("PTY_SMOKE_OK"),
            "未读到 echo 回显，ring: {out}"
        );

        provider.shutdown(&session_id).unwrap();
        assert!(provider.list().is_empty());
    }

    /// reattach 全链路：spawn 产出输出后 exists=true；reattach 返回 scrollback 含
    /// 之前输出且 exited=false；不存在的会话 reattach 返回 None、exists=false。
    #[test]
    fn reattach_via_provider() {
        let provider = LocalPtyProvider::new();
        let tmp = std::env::temp_dir().join("pty-reattach-test");
        std::fs::create_dir_all(&tmp).unwrap();

        let session_id = "reattach-test-issue".to_string();
        provider
            .spawn(
                SpawnOpts {
                    session_id: session_id.clone(),
                    cwd: tmp.to_string_lossy().into_owned(),
                    cols: 80,
                    rows: 24,
                    startup_command: None,
                },
                Channel::new(|_| Ok(())),
            )
            .unwrap();

        assert!(provider.exists(&session_id));
        assert!(!provider.exists("no-such-issue"));

        provider
            .write(&session_id, b"echo REATTACH_MARK\r\n")
            .unwrap();
        let deadline = Instant::now() + Duration::from_secs(8);
        loop {
            let ring = provider
                .with_session(&session_id, &mut |s| s.io.snapshot())
                .unwrap();
            if ring.contains("REATTACH_MARK") || Instant::now() > deadline {
                break;
            }
            std::thread::sleep(Duration::from_millis(100));
        }

        // reattach：scrollback 含历史输出 + 换装 listener（裸 Channel，send 丢弃）。
        let reattached = provider
            .reattach(&session_id, Channel::new(|_| Ok(())))
            .unwrap()
            .expect("会话存在应返回 Some");
        assert_eq!(reattached.session_id, session_id);
        assert!(!reattached.exited);
        assert!(
            reattached.scrollback.contains("REATTACH_MARK"),
            "scrollback: {}",
            reattached.scrollback
        );

        // 不存在的会话：reattach None（前端转 spawn 路径）。
        assert!(
            provider
                .reattach("no-such-issue", Channel::new(|_| Ok(())))
                .unwrap()
                .is_none()
        );

        provider.shutdown(&session_id).unwrap();
    }

    /// shell-ready 全链路（任务 2）：startup_command 包装 spawn → marker 被 barrier
    /// 剥除（ring 不含 marker 字节）→ 注入命令在提示符就绪后执行（ring 出现回显）→
    /// marker 前的用户输入被排队、顺序放行（注入命令先执行）。zsh 主路径。
    #[test]
    fn spawn_with_startup_command_injects_after_marker() {
        let provider = LocalPtyProvider::new();
        let base = std::env::temp_dir().join("we-term-shell-ready-e2e");
        let _ = std::fs::remove_dir_all(&base);
        std::fs::create_dir_all(&base).unwrap();
        set_app_data_dir(base.clone());

        let session_id = "shell-ready-e2e-issue".to_string();
        let opts = SpawnOpts {
            session_id: session_id.clone(),
            cwd: base.to_string_lossy().into_owned(),
            cols: 80,
            rows: 24,
            startup_command: Some("echo WE_TERM_INJECT_OK".to_string()),
        };
        let spawned = provider
            .spawn(opts, Channel::new(|_| Ok(())))
            .unwrap();
        assert!(spawned.fresh);

        // marker 前的并发写：应被 barrier 排队（不直达 shell）。
        provider
            .write(&session_id, b"echo USER_QUEUED\r")
            .unwrap();

        // 轮询 ring：注入命令回显 + 执行输出出现；marker 字节绝不出现。
        let deadline = Instant::now() + Duration::from_secs(10);
        let mut ring = String::new();
        while Instant::now() < deadline {
            ring = provider
                .with_session(&session_id, &mut |s| s.io.snapshot())
                .unwrap();
            if ring.contains("WE_TERM_INJECT_OK") && ring.contains("USER_QUEUED") {
                break;
            }
            std::thread::sleep(Duration::from_millis(100));
        }
        assert!(
            ring.contains("WE_TERM_INJECT_OK"),
            "注入命令未执行，ring: {ring}"
        );
        assert!(
            ring.contains("USER_QUEUED"),
            "marker 前的用户输入未放行，ring: {ring}"
        );
        // 精确匹配完整 marker 字节序列（子串 "we-term-shell-ready" 会误中 cwd 路径
        // ——测试目录名本身含该词）。
        let marker_str =
            String::from_utf8_lossy(crate::pty::shell_ready::WE_TERM_SHELL_READY_MARKER);
        assert!(
            !ring.contains(marker_str.as_ref()),
            "marker 字节泄漏到前端，ring 含完整 OSC 777 序列"
        );

        provider.shutdown(&session_id).unwrap();
        let _ = std::fs::remove_dir_all(&base);
    }

    /// shutdown_issue 前缀全关（T0.1）：store 放 `a::main`、`a::p1`、`a::p2`、`b::main`，
    /// shutdown_issue("a") 后仅剩 `b::main`；不存在 key 幂等 Ok。
    #[test]
    fn shutdown_issue_closes_all_panes() {
        let provider = LocalPtyProvider::new();
        let tmp = std::env::temp_dir().join("pty-shutdown-issue-test");
        std::fs::create_dir_all(&tmp).unwrap();

        let keys = ["a::main", "a::p1", "a::p2", "b::main"];
        for key in keys {
            provider
                .spawn(
                    SpawnOpts {
                        session_id: key.to_string(),
                        cwd: tmp.to_string_lossy().into_owned(),
                        cols: 80,
                        rows: 24,
                        startup_command: None,
                    },
                    Channel::new(|_| Ok(())),
                )
                .unwrap();
        }
        assert_eq!(provider.list().len(), 4);

        let closed = provider.shutdown_issue("a").unwrap();
        assert_eq!(closed, 3, "应关掉 a 与两个 pane");
        let remaining: Vec<String> = provider
            .list()
            .into_iter()
            .map(|s| s.session_id)
            .collect();
        assert_eq!(remaining, vec!["b::main".to_string()]);

        // 不存在 key：幂等 Ok + 关闭 0。
        assert_eq!(provider.shutdown_issue("no-such").unwrap(), 0);
        provider.shutdown("b::main").unwrap();
    }

    // ---------- claude 归因链 env 打标（T1.4）----------

    fn attr_env_get(envs: &[(&'static str, String)], k: &str) -> String {
        envs.iter()
            .find(|(key, _)| *key == k)
            .unwrap_or_else(|| panic!("missing env {k}"))
            .1
            .clone()
    }

    /// env 构造：PANE 恒为 pane 锚点；token 每次调用新 uuid（36 位连字符形态，
    /// 无 JSON 转义字符——hook 脚本直接拼进载荷首字段）；SPOOL_DIR 目录名与
    /// claude_runtime 常量同源。OnceLock 全局一次，SPOOL_DIR 断言不绑定具体
    /// base（并行测试可能已抢先 set_app_data_dir）。
    #[test]
    fn claude_attribution_envs_token_unique_per_spawn() {
        set_app_data_dir(std::env::temp_dir().join("we-term-attr-env-test"));

        let (a, b) = (
            claude_attribution_envs("iss1::main"),
            claude_attribution_envs("iss1::main"),
        );
        // PANE：pane 锚点，跨 spawn 恒定
        assert_eq!(attr_env_get(&a, "WE_TERM_PANE"), "iss1::main");
        assert_eq!(
            attr_env_get(&a, "WE_TERM_PANE"),
            attr_env_get(&b, "WE_TERM_PANE")
        );
        // TOKEN：per-spawn 唯一（代际围栏语义），uuid 连字符形态
        let (ta, tb) = (
            attr_env_get(&a, "WE_TERM_LAUNCH_TOKEN"),
            attr_env_get(&b, "WE_TERM_LAUNCH_TOKEN"),
        );
        assert_ne!(ta, tb, "token 必须 per-spawn 唯一");
        for t in [&ta, &tb] {
            assert_eq!(t.len(), 36, "uuid 形态：{t}");
            assert_eq!(t.matches('-').count(), 4, "uuid 形态：{t}");
            assert!(t.chars().all(|c| c == '-' || c.is_ascii_hexdigit()));
        }
        // SPOOL_DIR：app_data_dir 在场即给出，目录名与常量同源
        assert!(attr_env_get(&a, "WE_TERM_SPOOL_DIR")
            .ends_with(crate::claude_runtime::script::SPOOL_DIR_NAME));
    }

    /// PTY 透传：带 startup_command spawn（zsh/bash 走包装分支——该分支重建
    /// CommandBuilder，注入点若在重建之前 env 会被剥）后 echo 三标，ring 断言
    /// pane 锚点、uuid 形态 token、claude-spool 路径全部透传在场。
    #[test]
    fn spawn_injects_claude_attribution_envs_through_wrapper() {
        let provider = LocalPtyProvider::new();
        let base = std::env::temp_dir().join("we-term-attr-pty-test");
        let _ = std::fs::remove_dir_all(&base);
        std::fs::create_dir_all(&base).unwrap();
        set_app_data_dir(base.clone());

        let session_id = "attr-pty-issue::main".to_string();
        let opts = SpawnOpts {
            session_id: session_id.clone(),
            cwd: base.to_string_lossy().into_owned(),
            cols: 80,
            rows: 24,
            startup_command: Some(
                "echo WE_ATTR_BEGIN $WE_TERM_PANE $WE_TERM_LAUNCH_TOKEN \
                 $WE_TERM_SPOOL_DIR WE_ATTR_END"
                    .to_string(),
            ),
        };
        provider.spawn(opts, Channel::new(|_| Ok(()))).unwrap();

        // anchor 提前定义：break 守卫与断言共用（展开值锚点；回显行是字面
        // $WE_TERM_PANE 不含 session_id 值，不会误匹配）。
        let anchor = format!("WE_ATTR_BEGIN {session_id} ");
        let deadline = Instant::now() + Duration::from_secs(10);
        let mut ring = String::new();
        while Instant::now() < deadline {
            ring = provider
                .with_session(&session_id, &mut |s| s.io.snapshot())
                .unwrap();
            // 完整落行守卫：anchor 在场且其后 ≥36 字节（token 定长）才 break——
            // 防执行输出被 reader 分块切断时提前退出导致 anchor 误报或越界 panic。
            if let Some(pos) = ring.find(&anchor) {
                if ring.len() >= pos + anchor.len() + 36 {
                    break;
                }
            }
            std::thread::sleep(Duration::from_millis(100));
        }

        // pane 锚点为展开值。anchor 落地由循环守卫保证，此处重定位（循环内
        // pos 不出作用域）；循环若超时退出，此 panic 即失败报错出口。
        let pos = ring
            .find(&anchor)
            .unwrap_or_else(|| panic!("WE_TERM_PANE 未透传，ring: {ring}"));
        // token：anchor 后紧跟 36 位 uuid（echo 单行输出，中间无 \r 插入）
        let token = &ring[pos + anchor.len()..pos + anchor.len() + 36];
        assert_eq!(token.matches('-').count(), 4, "token 非 uuid 形态: {token}");
        assert!(token.chars().all(|c| c == '-' || c.is_ascii_hexdigit()));
        // spool 路径 + 完整落行（防截断误判）
        let tail = &ring[pos..];
        assert!(
            tail.contains(crate::claude_runtime::script::SPOOL_DIR_NAME),
            "WE_TERM_SPOOL_DIR 未透传: {tail}"
        );
        assert!(tail.contains("WE_ATTR_END"), "输出截断: {tail}");

        provider.shutdown(&session_id).unwrap();
        let _ = std::fs::remove_dir_all(&base);
    }

    /// 真 claude 端到端（T1.4，兼 T1.3 冒烟遗留验证）：手动
    /// `cargo test claude_e2e -- --ignored --nocapture`。受控工作区装 hooks →
    /// spawn PTY（env 三标随 spawn 注入）→ 注入 `claude -p` → 读 spool 断言：
    /// SessionStart→Stop 事件链在场、全部载荷行携带同一非空 launch_token
    /// （即 env → claude hook → spool 全链归因打通）。依赖真 claude CLI 与
    /// API 配额；勿与 dev app 同时跑（其 watcher 消费 + SessionStart 截断
    /// spool 文件会干扰断言）；勿 `--include-ignored` 并行——APP_DATA_DIR
    /// OnceLock 先到先得，被抢注则 spawn 的 spool 目录与本测试读取目录分家。
    #[test]
    #[ignore = "依赖真 claude CLI + API 配额，手动跑"]
    fn claude_e2e_spool_carries_launch_token() {
        use crate::claude_runtime::script::{SPOOL_DIR_NAME, spool_file_name};
        use crate::claude_runtime::types::HookPayload;

        let base = std::env::temp_dir().join("we-term-claude-e2e");
        let _ = std::fs::remove_dir_all(&base);
        let ws = base.join("ws");
        std::fs::create_dir_all(&ws).unwrap();
        set_app_data_dir(base.clone());
        std::fs::create_dir_all(base.join(SPOOL_DIR_NAME)).unwrap();

        // 装 hooks：脚本落 base/claude-hooks/，settings 写 ws/.claude/。
        crate::claude_runtime::installer::install(&ws, &base).unwrap();

        let provider = LocalPtyProvider::new();
        let session_id = "claude-e2e-issue::main".to_string();
        let opts = SpawnOpts {
            session_id: session_id.clone(),
            cwd: ws.to_string_lossy().into_owned(),
            cols: 80,
            rows: 24,
            startup_command: Some("claude -p 'reply with just: ok'".to_string()),
        };
        provider.spawn(opts, Channel::new(|_| Ok(()))).unwrap();

        // 轮询 spool：SessionStart 与 Stop 都到场（claude -p 跑完自动退出）。
        let spool_file = base
            .join(SPOOL_DIR_NAME)
            .join(spool_file_name(&session_id));
        let deadline = Instant::now() + Duration::from_secs(180);
        let mut payloads: Vec<HookPayload> = Vec::new();
        while Instant::now() < deadline {
            if let Ok(content) = std::fs::read_to_string(&spool_file) {
                payloads = content
                    .lines()
                    .filter(|l| !l.trim().is_empty())
                    .filter_map(|l| serde_json::from_str(l).ok())
                    .collect();
                let has = |n: &str| payloads.iter().any(|p| p.hook_event_name == n);
                if has("SessionStart") && has("Stop") {
                    break;
                }
            }
            std::thread::sleep(Duration::from_millis(500));
        }
        provider.shutdown(&session_id).unwrap();

        let names: Vec<&str> = payloads.iter().map(|p| p.hook_event_name.as_str()).collect();
        assert!(
            names.contains(&"SessionStart"),
            "事件链缺 SessionStart，实收: {names:?}（spool: {}）",
            spool_file.display()
        );
        assert!(names.contains(&"Stop"), "事件链缺 Stop，实收: {names:?}");
        // 全部行携带同一非空 launch_token——空值即 env 注入断链，不同值即跨代污染。
        let tokens: Vec<&str> = payloads
            .iter()
            .map(|p| p.launch_token.as_deref().unwrap_or(""))
            .collect();
        assert!(
            tokens.iter().all(|t| !t.is_empty()),
            "存在无 launch_token 的载荷行（env 断链）"
        );
        assert!(
            tokens.windows(2).all(|w| w[0] == w[1]),
            "载荷 token 不同代: {tokens:?}"
        );
        let _ = std::fs::remove_dir_all(&base);
    }
}
