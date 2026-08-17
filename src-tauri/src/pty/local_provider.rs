// LocalPtyProvider：本机 PTY 后端（portable-pty）。
//
// spawn 细节（spike 实证，见 docs/embedded_terminal.md）：
//   - shell 显式取 $SHELL 回退 /bin/zsh，加 -i（交互式）。不能用
//     CommandBuilder::new_default_prog()——它在无 tty 的环境（测试/某些启动上下文）
//     会永久挂起。
//   - cwd 不存在时 spawn_command 直接报错，由前端捕获展示「任务目录不存在」。
//
// reader 线程（输出推前端 + ring buffer）与退出通知在任务 2（输出通道 spike）接入；
// 本文件先落 spawn/write/resize/shutdown 的会话生命周期骨架。

use std::sync::atomic::Ordering;

use portable_pty::{CommandBuilder, PtySize, native_pty_system};

use super::provider::{PtyProvider, PtySessionInfo, SpawnOpts};
use super::session::PtySession;
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
    /// 启动会话（幂等）：同 issueId 已有会话（含已退出待重开的）直接返回现有。
    fn spawn(&self, opts: SpawnOpts) -> Result<String, String> {
        {
            let map = self
                .store
                .0
                .lock()
                .expect("PtySessionStore mutex poisoned");
            if map.contains_key(&opts.issue_id) {
                return Ok(opts.issue_id);
            }
        }

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

        let started_at = chrono::Utc::now().timestamp_millis();
        let session = PtySession::new(
            opts.issue_id.clone(),
            opts.cwd.clone(),
            pair.master,
            writer,
            child,
            started_at,
        );

        {
            let mut map = self
                .store
                .0
                .lock()
                .expect("PtySessionStore mutex poisoned");
            // 二次确认：并发 spawn 同 issueId 的败者 kill 刚起的 shell 让位，不覆盖先入会话。
            if map.contains_key(&opts.issue_id) {
                let _ = session.shutdown();
                return Ok(opts.issue_id);
            }
            map.insert(opts.issue_id.clone(), session);
        }

        // reader 持有 cloned reader；任务 2 在此启动 reader 线程（ring + 推前端）。
        // 当前骨架无消费者，drop 释放 fd。
        drop(reader);
        log::info!(
            "[pty] spawned session issue_id={} pid={} cwd={}",
            opts.issue_id,
            pid,
            opts.cwd
        );
        Ok(opts.issue_id)
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
                exited: s.exited.load(Ordering::SeqCst),
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
    use std::io::Read;
    use std::sync::mpsc;
    use std::time::{Duration, Instant};

    /// 冒烟：临时目录 spawn → 写 echo → reader 线程增量读到回显 → shutdown 干净退出。
    /// reader 逐块发送（不等 EOF——shell 常驻时无 EOF，等齐会假死），与生产流式形态一致。
    #[test]
    fn spawn_write_read_shutdown() {
        let provider = LocalPtyProvider::new();
        let tmp = std::env::temp_dir().join("pty-smoke-test");
        std::fs::create_dir_all(&tmp).unwrap();

        let issue_id = "smoke-test-issue".to_string();
        let id = provider
            .spawn(SpawnOpts {
                issue_id: issue_id.clone(),
                cwd: tmp.to_string_lossy().into_owned(),
                cols: 80,
                rows: 24,
            })
            .expect("spawn failed");
        assert_eq!(id, issue_id);

        // 幂等：重复 spawn 返回同 id，不重复起 shell。
        let again = provider
            .spawn(SpawnOpts {
                issue_id: issue_id.clone(),
                cwd: tmp.to_string_lossy().into_owned(),
                cols: 80,
                rows: 24,
            })
            .unwrap();
        assert_eq!(again, issue_id);

        // 从 store 取 cloned reader（生产路径由 reader 线程持有，测试手动取）。
        let reader = {
            let mut map = provider.store.0.lock().unwrap();
            let session = map.get_mut(&issue_id).unwrap();
            session.master.try_clone_reader().unwrap()
        };

        provider
            .write(&issue_id, b"echo PTY_SMOKE_OK\r\n")
            .unwrap();
        provider.resize(&issue_id, 100, 30).unwrap();

        // reader 线程：读到块立即转发，EOF/错误时结束。
        let (tx, rx) = mpsc::channel::<Vec<u8>>();
        std::thread::spawn(move || {
            let mut buf = [0u8; 4096];
            let mut reader = reader;
            loop {
                match reader.read(&mut buf) {
                    Ok(0) => break,
                    Ok(n) => {
                        if tx.send(buf[..n].to_vec()).is_err() {
                            break; // 接收端已放弃
                        }
                    }
                    Err(_) => break,
                }
            }
        });

        // 主线程：增量累积 + 8s 截止，出现回显即成功（shell 常驻，无需等到 EOF）。
        let deadline = Instant::now() + Duration::from_secs(8);
        let mut out = String::new();
        while Instant::now() < deadline {
            match rx.try_recv() {
                Ok(chunk) => {
                    out.push_str(&String::from_utf8_lossy(&chunk));
                    if out.contains("PTY_SMOKE_OK") {
                        break;
                    }
                }
                Err(_) => std::thread::sleep(Duration::from_millis(100)),
            }
        }
        assert!(
            out.contains("PTY_SMOKE_OK"),
            "未读到 echo 回显，输出: {out}"
        );

        provider.shutdown(&issue_id).unwrap();
        assert!(provider.list().is_empty());
    }
}
