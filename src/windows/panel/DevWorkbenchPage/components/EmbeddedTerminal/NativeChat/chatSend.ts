// chat 发送字节纯函数（terminal_chat T3.1）：把 composer 文本转成回写 PTY 的字节
// 序列。对标 orca native-chat-send.ts 的字节规则（无 IO，纯函数便于单测）：
//   - 清行：Ctrl+U（\x15）——agent TUI 把 Ctrl+U 当 kill-to-start-of-line，防止上一
//     条未提交输入粘到本次提示词
//   - 正文：单行直接 sanitize；多行用 bracketed paste（\x1b[200~…\x1b[201~）包裹，
//     否则换行会被 TUI 当 Enter 提前提交
//   - 提交：\r 作为「单独、略延迟」的一次 write——正文与回车同写会被当粘贴正文吞掉
//
// 注意：回车（\r）而非 \n 才是 xterm/agent composer 认定的 Enter。

// Ctrl+U：清空 TUI 未提交输入行。
export const CHAT_CLEAR_INPUT = '\x15';
// 回车提交字节（单独延迟 write，勿与正文同写）。
export const CHAT_SUBMIT = '\r';
// bracketed paste 帧头/帧尾。
const BRACKETED_PASTE_START = '\x1B[200~';
const BRACKETED_PASTE_END = '\x1B[201~';
// 正文写入与回车之间的间隔：让 TUI 先消化粘贴正文，再收回车提交。
export const CHAT_SUBMIT_DELAY_MS = 500;

// 内嵌 ESC（如从 scrollback 粘贴来的 \x1b[201~）会提前关闭 bracketed paste 帧、
// 剩余字节被当按键执行。把 ESC 替换为可打印替代符（U+241B）中和掉一切成帧转义。
export function sanitizeBracketedPasteText(text: string): string {
  return text.split('\x1B').join('␛');
}

// 把 \r?\n 归一化为 \r（对齐 xterm 原生粘贴：剪贴板换行转成 CR）。
function normalizeLineEndings(text: string): string {
  return text.replace(/\r?\n/g, '\r');
}

// 构造正文字节（不含提交回车）：单行 → sanitize；多行 → bracketed paste 包裹。
export function buildChatPasteBytes(text: string): string {
  if (/[\r\n]/.test(text)) {
    return `${BRACKETED_PASTE_START}${sanitizeBracketedPasteText(normalizeLineEndings(text))}${BRACKETED_PASTE_END}`;
  }
  return sanitizeBracketedPasteText(text);
}
