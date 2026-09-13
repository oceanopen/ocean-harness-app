import { useEffect, useState } from 'react';
import { getAppConfig } from './appConfig';

/**
 * 批量配置就绪闸门（一次性 mount gate）：给定 keys 的持久化值全部读取完成后
 * 返回 true。用于「消费配置的组件 / 副作用编排」挂载前的前置等待——杜绝
 * useConfigValue「初值恒为默认值、真实值异步回填」造成的「先默认值挂载、
 * 到达后纠正」路径（终端场景中每次纠正都是一次打在已绘制提示符上的
 * SIGWINCH 重绘伪影；布局场景中是首帧默认态 → 动画收起的翻转）。
 *
 * 语义要点：
 * - 一次性：true 后不回退。本 hook 只回答「持久化值是否已读到」，不追踪值
 *   变化——后续配置变化仍走 useConfigValue 的事件热更新路径。
 * - 失败放行：任一 key 读取失败也置 true——闸门卡死（组件永远不挂载）比
 *   回落默认值更糟。
 * - keys 需稳定引用（模块级常量）：数组直接进 deps，调用方每次渲染新建数组
 *   会导致 effect 反复重跑。
 */
export function useConfigReady(keys: readonly string[]): boolean {
  const [ready, setReady] = useState(false);

  useEffect(() => {
    let cancelled = false;
    const markReady = () => {
      if (!cancelled) {
        setReady(true);
      }
    };
    // settle 语义：全量兑现即就绪；个别 key 失败不阻塞（失败放行，见头注释）。
    Promise.all(keys.map(key => getAppConfig(key))).then(markReady, markReady);
    return () => {
      cancelled = true;
    };
  }, [keys]);

  return ready;
}
