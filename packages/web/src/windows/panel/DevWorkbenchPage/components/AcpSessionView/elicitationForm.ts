// elicitation 表单的纯函数层：requestedSchema（JSON Schema）→ 渲染计划 → 应答 content。
// 只覆盖真适配器实际发出的子集（claude-acp 0.84.0 elicitation.js）：属性扁平 + 基元类型
// + 字符串枚举——单选 string+oneOf、多选 array+items.anyOf、自由文本/数值/布尔；其余
// 形态降级 unknown（UI 只读展示，不参与应答）。越界的组合关键字/嵌套结构不做。

/** 单个表单字段的渲染计划。 */
export interface ElicitationFieldPlan {
  key: string;
  title?: string;
  description?: string;
  /** 渲染形态：radio 单选 / checkbox 多选 / text 文本 / number 数值 / switch 开关 / unknown 只读降级。 */
  widget: 'radio' | 'checkbox' | 'text' | 'number' | 'switch' | 'unknown';
  /** radio/checkbox 的选项目录（const 为应答 wire 值，label/title 为展示文案）。 */
  options?: ElicitationOptionPlan[];
}

export interface ElicitationOptionPlan {
  value: string;
  label?: string;
  description?: string;
}

// JSON Schema 字段面（子集；unknown 兜底其余）。
interface FieldSchema {
  type?: string;
  title?: string;
  description?: string;
  oneOf?: unknown;
  items?: { anyOf?: unknown };
}

// 枚举项（adapter：const=应答值，title=展示文案，description=次级说明）。
interface EnumOptionSchema {
  const?: unknown;
  title?: string;
  description?: string;
}

/**
 * requestedSchema → 渲染计划（properties 插入序即渲染序——AskUserQuestion 的多题顺序、
 * 每题伴随的 Other 自由文本框顺序都靠它）。非对象/无 properties 返回空计划。
 */
export function renderElicitationPlan(schema: unknown): ElicitationFieldPlan[] {
  if (!schema || typeof schema !== 'object') {
    return [];
  }
  const properties = (schema as { properties?: unknown }).properties;
  if (!properties || typeof properties !== 'object') {
    return [];
  }
  return Object.entries(properties as Record<string, unknown>).map(([key, raw]) => planField(key, raw));
}

/** 单字段归类（按 adapter 子集判定，越界即 unknown）。 */
function planField(key: string, raw: unknown): ElicitationFieldPlan {
  const field = (raw ?? {}) as FieldSchema;
  const base = { key, title: field.title, description: field.description };
  if (field.type === 'array') {
    const options = enumOptionsOf(field.items?.anyOf);
    if (options) {
      return { ...base, widget: 'checkbox', options };
    }
    return { ...base, widget: 'unknown' };
  }
  if (field.type === 'string') {
    const options = enumOptionsOf(field.oneOf);
    if (options) {
      return { ...base, widget: 'radio', options };
    }
    return { ...base, widget: 'text' };
  }
  if (field.type === 'number' || field.type === 'integer') {
    return { ...base, widget: 'number' };
  }
  if (field.type === 'boolean') {
    return { ...base, widget: 'switch' };
  }
  return { ...base, widget: 'unknown' };
}

/** oneOf/anyOf 列表 → 选项目录；任一项缺 const 或 const 非字符串（非 adapter 子集）整体降级。 */
function enumOptionsOf(list: unknown): ElicitationOptionPlan[] | undefined {
  if (!Array.isArray(list) || list.length === 0) {
    return undefined;
  }
  const options: ElicitationOptionPlan[] = [];
  for (const item of list) {
    const option = (item ?? {}) as EnumOptionSchema;
    if (typeof option.const !== 'string') {
      return undefined;
    }
    options.push({ value: option.const, label: option.title, description: option.description });
  }
  return options;
}

/**
 * 表单值 → accept content（键值对透传 ACP wire）。空值剔除语义：未选/空串/未填字段
 * 不下发（adapter 把 accept 里的缺字段当跳过；switch 仅 true 下发，false 视为未触及）。
 * number 字段在表单层已归一为 finite number 或 undefined，此处不再收字符串。
 */
export function collectElicitationContent(fields: ElicitationFieldPlan[], values: Record<string, unknown>): Record<string, unknown> {
  const content: Record<string, unknown> = {};
  for (const field of fields) {
    const value = values[field.key];
    switch (field.widget) {
      case 'radio':
      case 'text':
        if (typeof value === 'string' && value.trim() !== '') {
          content[field.key] = value;
        }
        break;
      case 'checkbox': {
        const picks = Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : [];
        if (picks.length > 0) {
          content[field.key] = picks;
        }
        break;
      }
      case 'number':
        if (typeof value === 'number' && Number.isFinite(value)) {
          content[field.key] = value;
        }
        break;
      case 'switch':
        if (value === true) {
          content[field.key] = true;
        }
        break;
      case 'unknown':
        break; // 降级字段不参与应答
    }
  }
  return content;
}
