import type { CommandConfig, CommandGroup } from './types';
import { commands } from './commands';

export function getCommandsByGroup(group: CommandGroup): CommandConfig[] {
  return commands.filter(cmd => cmd.group === group);
}
