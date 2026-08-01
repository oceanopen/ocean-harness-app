import type { CommandConfig, CommandGroup, CommandPaletteContextValue } from './types';
import { commands } from './commands';

// 静态命令注册表：v1 命令在 commands.tsx 模块级一次性声明，无需运行期动态注册，
// 故这里只是基于该数组提供"按上下文过滤"的纯函数查询（对标 plane PowerKCommandRegistry 的
// getVisibleCommands / getCommandsByGroup，但不引入 MobX observable/computedFn）。
// 将来若需多模块动态注册，再升级为 external store + useSyncExternalStore，对外 API 保持不变。

// 按运行期上下文过滤出可见命令（isVisible 缺省视为恒可见）。
export function getVisibleCommands(ctx: CommandPaletteContextValue): CommandConfig[] {
  return commands.filter(cmd => !cmd.isVisible || cmd.isVisible(ctx));
}

// 在可见命令中取指定分组（Dialog 按分组渲染 Command.Group）。
export function getCommandsByGroup(group: CommandGroup, ctx: CommandPaletteContextValue): CommandConfig[] {
  return getVisibleCommands(ctx).filter(cmd => cmd.group === group);
}
