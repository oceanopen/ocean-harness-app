import type { PtySessionStatus } from './usePtySession';
import { commands } from '@src/shared/bindings';
import { EVENT_CLAUDE_SESSIONS_CHANGED } from '@src/shared/events';
import { listen } from '@tauri-apps/api/event';
import { useEffect, useRef, useState } from 'react';

// claude 运行态探测轮询间隔：退出恢复的兜底时效（Dead 会话 json 保留、watch
// 不触发，进程退出只能靠轮询感知）。交互近无感 + ps 开销可忽略（单次 IPC +
// 若干 ps 子进程）。这里只是兜底，事件监听也会同步处理。
const POLL_MS = 60_000;

// useClaudeRunning：本 PTY 会话是否跑着 claude（terminal_03 §3.2 按钮置灰驱动）。
//
//   running = active && probed
//   probed：后端 pty_claude_running 沿 claude pid 父链（ps -o ppid=）匹配本
//   会话 shell pid 的进程树探测——进程级存活真值（含退出方向；direct 直启
//   下 claude 即 PTY 子进程，命中同一位置；多 pane 同 cwd 也靠进程树区分）。
//
// 探测驱动时机三路：
//   1. 会话 active 即探测一次（配置直启场景 claude 可能已起；reattach 场景同理）
//   2. EVENT_CLAUDE_SESSIONS_CHANGED（sessions 目录 watch 秒级——claude 启动写
//      json 落盘即触发）重查
//   3. 60s 轮询兜底退出恢复（claude 死后父链无命中 → false）
// 会话非 active（connecting/error/exited）恒 false 且不探测——shell 都没起，
// 探测无意义。探测失败静默（尽力而为，不阻塞 UI；失败保持现值）。
export function useClaudeRunning(sessionId: string, status: PtySessionStatus): boolean {
  // 非 active 恒 false：渲染期派生（避免 effect 内同步 setState——react/set-state-in-effect）。
  // active 态真值经探测 effect 异步回填（probe 异步 setRunning 不触发该规则）。
  const active = status === 'active';
  const [probed, setProbed] = useState(false);
  // active 转 false 的渲染期重置（usePtySession lastAttachKeyRef 同款范式，
  // 避免 effect 内同步 setState——react/set-state-in-effect）：probed 是组件级
  // state，会话关闭→重开（exited → active）后残留上一轮的 true，首帧会误报
  // 运行中；探测持续失败（catch 保持现值）的路径下更是悬挂。
  const lastActiveRef = useRef(false);
  if (active !== lastActiveRef.current) {
    if (!active) {
      setProbed(false);
    }
    lastActiveRef.current = active;
  }

  useEffect(() => {
    if (!active) {
      return;
    }
    let disposed = false;
    const probe = () => {
      void commands.ptyClaudeRunning(sessionId).then((running) => {
        if (!disposed) {
          setProbed(running);
        }
      }).catch((e: unknown) => {
        // warn 级：探测持续失败必须可见（如命令未注册——dev 旧二进制），
        // 曾因 debug 静默把环境问题伪装成功能 bug。
        console.warn('[useClaudeRunning] probe failed:', sessionId, e);
      });
    };
    probe();
    const timer = window.setInterval(probe, POLL_MS);
    const unlistenSessions = listen(EVENT_CLAUDE_SESSIONS_CHANGED, () => {
      probe();
    });
    return () => {
      disposed = true;
      window.clearInterval(timer);
      // 页面重载后 listeners 注册表已清空，旧 eventId 注销报 undefined——无泄漏，
      // 重载竞态已知形态，降 debug（同 useConfigValue 处理，注释详见彼处）。
      unlistenSessions.then(fn => fn()).catch((err: unknown) => console.debug('[useClaudeRunning] unlisten skipped (page reloaded?):', err));
    };
  }, [sessionId, active]);

  return active && probed;
}
