import type { AppConfigChangedPayload } from './bindings';
import { listen } from '@tauri-apps/api/event';
import { useEffect, useState } from 'react';
import { getAppConfig } from './appConfig';
import { EVENT_APP_CONFIG_CHANGED } from './events';

/**
 * 订阅一个配置项：mount 时读初值，监听 app-config-changed 实时更新。
 *
 * @param key 配置项 key（与 src/shared/config.ts 的 *_KEY 常量对齐）
 * @param decode 把原始字符串（getAppConfig 的 string|null 或事件 payload.value）解析为目标值；
 *   应在 raw 缺失/非法时回落到 defaultValue，保证总返回有效 T。
 *   **应为稳定引用**（模块级函数或 useCallback），否则每次渲染都会重新订阅。
 * @param defaultValue 加载完成前的初值（通常等于 decode(null)）。
 * @returns 当前配置值（加载前为 defaultValue）。
 *
 * 与 useSystemThemeMode 同属「订阅外部信号」类 hook；消费方无需再手写
 * getAppConfig + listen(EVENT_APP_CONFIG_CHANGED) + cleanup 的重复模板。
 */
export function useConfigValue<T>(
  key: string,
  decode: (raw: string | null) => T,
  defaultValue: T,
): T {
  const [value, setValue] = useState<T>(defaultValue);

  useEffect(() => {
    const apply = (raw: string | null) => setValue(decode(raw));
    getAppConfig(key)
      .then(apply)
      .catch((e: unknown) => {
        console.warn(`[useConfigValue:${key}] load failed`, e);
      });
    const unlisten = listen<AppConfigChangedPayload>(EVENT_APP_CONFIG_CHANGED, (e) => {
      if (e.payload.key === key) {
        apply(e.payload.value);
      }
    });
    // listen 被 ACL 拒绝时（窗口未在 capabilities 授权）默认静默；显式 warn 以便尽早发现漏配窗口。
    unlisten.catch((err: unknown) => {
      console.warn(`[useConfigValue:${key}] listen denied (check capability for this window)`, err);
    });
    return () => {
      unlisten
        .then(fn => fn())
        .catch((err: unknown) => {
          // 页面重载（vite HMR 全页刷新 / Cmd+R）后 tauri 注入的 listeners 注册表
          // 已清空，cleanup 仍持旧 eventId 注销 → "listeners[eventId].handlerId
          // undefined"。事件已随页面死掉、Rust 侧 unlisten 亦到达，无泄漏——重载
          // 竞态是该 API 已知形态，降 debug 不刷屏（warn 会盖住真问题）。
          console.debug(`[useConfigValue:${key}] unlisten skipped (page reloaded?):`, err);
        });
    };
  }, [key, decode]);

  return value;
}
