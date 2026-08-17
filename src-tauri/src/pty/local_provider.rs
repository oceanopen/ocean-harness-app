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

use std::sync::Arc;
use std::sync::atomic::Ordering;

use portable_pty::{CommandBuilder, PtySize, native_pty_system};
use tauri::ipc::Channel;

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
        io.set_listener(listener);
        let started_at = chrono::Utc::now().timestamp_millis();
        let session = PtySession::new(
            opts.issue_id.clone(),
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
            // 二次确认：并发 spawn 同 issueId 的败者 kill 刚起的 shell 让位，不覆盖先入会话。
            if let Some(existing) = map.get(&opts.issue_id) {
                if !existing.exited() {
                    let _ = session.shutdown();
                    return Ok(self.snapshot(existing));
                }
            }
            map.insert(opts.issue_id.clone(), session);
        }

        log::info!(
            "[pty] spawned session issue_id={} pid={} cwd={}",
            opts.issue_id,
            pid,
            opts.cwd
        );
        Ok(PtySpawned {
            issue_id: opts.issue_id.clone(),
            cwd: opts.cwd.clone(),
            pid,
            started_at,
            fresh: true,
        })
    }

    /// 从现有会话生成快照荷载。
    fn snapshot(&self, s: &PtySession) -> PtySpawned {
        PtySpawned {
            issue_id: s.issue_id.clone(),
            cwd: s.cwd.clone(),
            pid: s
                .child
                .try_lock()
                .map(|c| c.process_id().unwrap_or(0))
                .unwrap_or(0),
            started_at: s.started_at,
            fresh: false,
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
        let existing = {
            let map = self
                .store
                .0
                .lock()
                .expect("PtySessionStore mutex poisoned");
            map.get(&opts.issue_id).map(|s| !s.exited())
        };
        match existing {
            // 未退出：复用，换装 listener（webview 刷新后重挂路径）。
            Some(true) => {
                let mut map = self
                    .store
                    .0
                    .lock()
                    .expect("PtySessionStore mutex poisoned");
                if let Some(session) = map.get_mut(&opts.issue_id) {
                    session.io.set_listener(listener);
                    return Ok(self.snapshot(session));
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
                    if let Some(session) = map.remove(&opts.issue_id) {
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
                let issue_id = session.issue_id.clone();
                session.shutdown()?;
                log::info!("[pty] shutdown session issue_id={}", issue_id);
                Ok(())
            }
            None => Ok(()), // 幂等：不存在视为已关闭
        }
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
                issue_id: s.issue_id.clone(),
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
                issue_id: s.issue_id.clone(),
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

        let issue_id = "smoke-test-issue".to_string();
        let opts = SpawnOpts {
            issue_id: issue_id.clone(),
            cwd: tmp.to_string_lossy().into_owned(),
            cols: 80,
            rows: 24,
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
        assert_eq!(again.issue_id, issue_id);
        assert_eq!(provider.list().len(), 1);

        provider
            .write(&issue_id, b"echo PTY_SMOKE_OK\r\n")
            .unwrap();
        provider.resize(&issue_id, 100, 30).unwrap();

        // 不再 clone 第二个 reader（与 reader 线程竞争同一 fd 会各读走一半字节）。
        // 输出断言改走 ring：reader 线程把全部输出入 ring，轮询 snapshot 直到含回显。
        let deadline = Instant::now() + Duration::from_secs(8);
        let mut out = String::new();
        while Instant::now() < deadline {
            out = provider
                .with_session(&issue_id, &mut |s| s.io.snapshot())
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

        provider.shutdown(&issue_id).unwrap();
        assert!(provider.list().is_empty());
    }

    /// reattach 全链路：spawn 产出输出后 exists=true；reattach 返回 scrollback 含
    /// 之前输出且 exited=false；不存在的会话 reattach 返回 None、exists=false。
    #[test]
    fn reattach_via_provider() {
        let provider = LocalPtyProvider::new();
        let tmp = std::env::temp_dir().join("pty-reattach-test");
        std::fs::create_dir_all(&tmp).unwrap();

        let issue_id = "reattach-test-issue".to_string();
        provider
            .spawn(
                SpawnOpts {
                    issue_id: issue_id.clone(),
                    cwd: tmp.to_string_lossy().into_owned(),
                    cols: 80,
                    rows: 24,
                },
                Channel::new(|_| Ok(())),
            )
            .unwrap();

        assert!(provider.exists(&issue_id));
        assert!(!provider.exists("no-such-issue"));

        provider
            .write(&issue_id, b"echo REATTACH_MARK\r\n")
            .unwrap();
        let deadline = Instant::now() + Duration::from_secs(8);
        loop {
            let ring = provider
                .with_session(&issue_id, &mut |s| s.io.snapshot())
                .unwrap();
            if ring.contains("REATTACH_MARK") || Instant::now() > deadline {
                break;
            }
            std::thread::sleep(Duration::from_millis(100));
        }

        // reattach：scrollback 含历史输出 + 换装 listener（裸 Channel，send 丢弃）。
        let reattached = provider
            .reattach(&issue_id, Channel::new(|_| Ok(())))
            .unwrap()
            .expect("会话存在应返回 Some");
        assert_eq!(reattached.issue_id, issue_id);
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

        provider.shutdown(&issue_id).unwrap();
    }
}
