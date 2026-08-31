// LocalPtyProvider：本机 PTY 后端（portable-pty）。
//
// spawn 细节（spike 实证，见 docs/embedded_terminal.md）：
//   - shell 显式取 $SHELL 回退 /bin/zsh，加 -i（交互式）。不能用
//     CommandBuilder::new_default_prog()——它在无 tty 的环境（测试/某些启动上下文）
//     会永久挂起。
//   - direct_command（claude_orca T5.1，唯一自动执行路径）：无 shell 中转，
//     PTY 直接 exec CLI；解析失败回落普通裸 shell（warn log，用户可手动启动）。
//     （chat 模式退役：shell_ready 注入中间层已删。）
//   - cwd 不存在时 spawn_command 直接报错，由前端捕获展示「任务目录不存在」。
//
// spawn 即启动 reader 线程（session::spawn_reader_thread）：输出经 UTF-8 边界切分后
// 推 listener Channel；EOF 置 exited + Exit 事件。重复 spawn 语义（§3.4/§5.2）：
//   - 未退出会话：复用，换装新 listener（webview 刷新后旧 Channel 失效）
//   - 已退出会话：移除旧会话重起 shell（前端「重开」按钮路径）

use std::sync::Arc;
use std::sync::atomic::Ordering;

use portable_pty::{CommandBuilder, PtySize, native_pty_system};
use tauri::ipc::Channel;

use super::cli_bin;
use super::provider::{PtyProvider, PtyReattached, PtySessionInfo, PtySpawned, SpawnOpts};
use super::session::{PtyEvent, PtySession, SessionIo, spawn_reader_thread};
use super::state::PtySessionStore;

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

        // direct_command 分支（T5.1，唯一自动执行路径）：CLI 直启——整体替换
        // cmd（重建 CommandBuilder），无 shell 中转。解析失败（CLI 不在场/
        // builtin/alias/token 非法）回落普通裸 shell（上方已构造好），用户可
        // 手动启动。
        if let Some(direct) = &opts.direct_command {
            let token = direct.split_whitespace().next().unwrap_or("");
            match cli_bin::resolve_cli_bin(token) {
                Some((bin, login_path)) => {
                    // 首 token（CLI 名）由绝对路径顶替；余参原样透传
                    //（含 flag 形态，如 --model xxx）。
                    cmd = CommandBuilder::new(&bin);
                    for arg in direct.split_whitespace().skip(1) {
                        cmd.arg(arg);
                    }
                    // GUI app env 缺 nvm/volta 目录（claude 常装在那里）：注入
                    // login PATH，node shebang CLI 才能找到 node；brew 自包含
                    // 二进制不依赖，注入无害。
                    cmd.env("PATH", &login_path);
                    cmd.cwd(&opts.cwd);
                    log::info!(
                        "[pty] direct spawn session_id={} bin={}",
                        opts.session_id,
                        bin
                    );
                }
                None => {
                    log::warn!(
                        "[pty] direct spawn '{token}' unresolvable, \
                         fallback to plain shell session_id={}",
                        opts.session_id
                    );
                }
            }
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
        let session = PtySession::new(
            opts.session_id.clone(),
            opts.cwd.clone(),
            pair.master,
            writer,
            child,
            Arc::clone(&io),
            started_at,
        );
        spawn_reader_thread(reader, io);

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
            direct_command: None,
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
                    direct_command: None,
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
                        direct_command: None,
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

    // ---------- direct_command 直启分支（T5.1）----------

    /// direct 冒烟：`env` 直启（外部二进制，打印 env 即退）——ring 出现
    /// env 输出（PATH 等基础变量恒在场），随后会话自然退出（CLI 退出即
    /// pane 退出语义，无 shell 回落）。
    #[test]
    fn direct_spawn_runs_and_exits() {
        let provider = LocalPtyProvider::new();
        let tmp = std::env::temp_dir().join("pty-direct-spawn-test");
        std::fs::create_dir_all(&tmp).unwrap();

        let session_id = "direct-spawn-issue::main".to_string();
        let opts = SpawnOpts {
            session_id: session_id.clone(),
            cwd: tmp.to_string_lossy().into_owned(),
            cols: 80,
            rows: 24,
            direct_command: Some("env".to_string()),
        };
        provider
            .spawn(opts, Channel::new(|_| Ok(())))
            .unwrap();

        // ring：env 打印出基础变量（PATH 恒在场，即直启命令已被执行）。
        let deadline = Instant::now() + Duration::from_secs(10);
        let mut ring = String::new();
        while Instant::now() < deadline {
            ring = provider
                .with_session(&session_id, &mut |s| s.io.snapshot())
                .unwrap();
            if ring.contains("PATH=") {
                break;
            }
            std::thread::sleep(Duration::from_millis(100));
        }
        assert!(
            ring.contains("PATH="),
            "direct spawn 未执行（ring 无 env 输出）: {ring}"
        );

        // env 打印完即退：reader EOF → exited 置位（list 快照可见；会话留 store）。
        let deadline = Instant::now() + Duration::from_secs(10);
        let mut exited = false;
        while Instant::now() < deadline {
            exited = provider
                .list()
                .into_iter()
                .find(|s| s.session_id == session_id)
                .map(|s| s.exited)
                .unwrap_or(false);
            if exited {
                break;
            }
            std::thread::sleep(Duration::from_millis(100));
        }
        assert!(
            exited,
            "direct spawn 的 CLI 退出应置位会话 exited"
        );

        provider.shutdown(&session_id).unwrap();
        let _ = std::fs::remove_dir_all(&tmp);
    }

    /// direct 回落：token 为 builtin（echo——`command -v` 返回名字而非路径，
    /// 不可直启）→ 回落普通裸 shell：命令不执行（ring 无输出标记）且会话
    /// 存活（交互 shell 等输入；direct 成功路径的 CLI 早已退出）。
    #[test]
    fn direct_spawn_fallback_to_plain_shell() {
        let provider = LocalPtyProvider::new();
        let tmp = std::env::temp_dir().join("pty-direct-fallback-test");
        std::fs::create_dir_all(&tmp).unwrap();

        let session_id = "direct-fallback-issue::main".to_string();
        let opts = SpawnOpts {
            session_id: session_id.clone(),
            cwd: tmp.to_string_lossy().into_owned(),
            cols: 80,
            rows: 24,
            direct_command: Some("echo WE_DIRECT_FALLBACK_OK".to_string()),
        };
        provider
            .spawn(opts, Channel::new(|_| Ok(())))
            .unwrap();

        // shell 起动留时间（若命令被注入执行会在 ring 出现标记——回落语义下永无）。
        std::thread::sleep(Duration::from_millis(1500));
        let ring = provider
            .with_session(&session_id, &mut |s| s.io.snapshot())
            .unwrap();
        assert!(
            !ring.contains("WE_DIRECT_FALLBACK_OK"),
            "builtin 回落不应执行命令，ring: {ring}"
        );
        let info = provider
            .list()
            .into_iter()
            .find(|s| s.session_id == session_id)
            .unwrap();
        assert!(!info.exited, "回落 shell 应存活");

        provider.shutdown(&session_id).unwrap();
        let _ = std::fs::remove_dir_all(&tmp);
    }
}
