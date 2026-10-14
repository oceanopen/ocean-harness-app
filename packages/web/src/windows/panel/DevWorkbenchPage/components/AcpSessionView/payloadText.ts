/**
 * 工具调用载荷文本化（ToolCallItem 载荷块）：pretty 化（字符串直显避免引号噪音，
 * 其余 JSON.stringify，解析失败兜底 String）+ 超长截断。截断按 UTF-16 码元计——DOM
 * 内存随码元线性；上限常量命名对齐 iwFileMaxEntries 惯例。
 */
export const tcPayloadMaxChars = 32768;

// 截断结果：text 可直接渲染（截断点保证不劈开代理对），truncated 标记是否发生截断。
export interface TruncatedPayload {
  text: string;
  truncated: boolean;
}

export function prettyPayloadText(value: unknown): string {
  if (typeof value === 'string') {
    return value;
  }
  try {
    return JSON.stringify(value, null, 2) ?? String(value);
  } catch {
    return String(value);
  }
}

export function truncatePayloadText(text: string): TruncatedPayload {
  if (text.length <= tcPayloadMaxChars) {
    return { text, truncated: false };
  }
  let cut = tcPayloadMaxChars;
  const last = text.charCodeAt(cut - 1);
  if (last >= 0xD800 && last <= 0xDBFF) {
    cut -= 1; // 截断点落在高代理上时回退一位，避免渲染出孤立半对。
  }
  return { text: text.slice(0, cut), truncated: true };
}
