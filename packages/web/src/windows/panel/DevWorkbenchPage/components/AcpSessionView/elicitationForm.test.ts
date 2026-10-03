import { describe, expect, it } from 'vitest';
import { collectElicitationContent, renderElicitationPlan } from './elicitationForm';

// adapter 真实形态样本（claude-acp 0.84.0 elicitation.js 的构造规则）。
const ASK_SINGLE_SELECT = {
  type: 'object',
  properties: {
    question_0: {
      type: 'string',
      title: '方案',
      oneOf: [
        { const: '方案 A', title: '方案 A', description: '改动最小' },
        { const: '方案 B', title: '方案 B' },
      ],
    },
    question_0_custom: { type: 'string', title: 'Other', description: 'Type your own answer' },
  },
};

const ASK_MULTI_SELECT = {
  type: 'object',
  properties: {
    question_1: {
      type: 'array',
      title: '依赖',
      items: { anyOf: [{ const: 'Redis', title: 'Redis' }, { const: 'MySQL', title: 'MySQL' }] },
    },
  },
};

describe('renderElicitationPlan', () => {
  it('单选 string+oneOf 归类 radio 且保留选项文案', () => {
    const plan = renderElicitationPlan(ASK_SINGLE_SELECT);
    expect(plan.map(f => f.key)).toEqual(['question_0', 'question_0_custom']);
    expect(plan[0].widget).toBe('radio');
    expect(plan[0].options).toEqual([
      { value: '方案 A', label: '方案 A', description: '改动最小' },
      { value: '方案 B', label: '方案 B', description: undefined },
    ]);
    expect(plan[1].widget).toBe('text');
  });

  it('多选 array+items.anyOf 归类 checkbox', () => {
    const plan = renderElicitationPlan(ASK_MULTI_SELECT);
    expect(plan[0].widget).toBe('checkbox');
    expect(plan[0].options?.map(o => o.value)).toEqual(['Redis', 'MySQL']);
  });

  it('number/integer/boolean 归类对应控件，越界形态降级 unknown', () => {
    const plan = renderElicitationPlan({
      type: 'object',
      properties: {
        count: { type: 'integer' },
        ratio: { type: 'number' },
        flag: { type: 'boolean' },
        nested: { type: 'object' },
        constOnly: { oneOf: [{ const: 1 }] }, // 非字符串 const 降级
      },
    });
    expect(plan.map(f => f.widget)).toEqual(['number', 'number', 'switch', 'unknown', 'unknown']);
  });

  it('非对象与缺 properties 返回空计划', () => {
    expect(renderElicitationPlan(undefined)).toEqual([]);
    expect(renderElicitationPlan('str')).toEqual([]);
    expect(renderElicitationPlan({ type: 'object' })).toEqual([]);
  });
});

describe('collectElicitationContent', () => {
  const plan = renderElicitationPlan({
    type: 'object',
    properties: {
      pick: { type: 'string', oneOf: [{ const: 'a' }, { const: 'b' }] },
      free: { type: 'string' },
      multi: { type: 'array', items: { anyOf: [{ const: 'x' }, { const: 'y' }] } },
      count: { type: 'number' },
      flag: { type: 'boolean' },
      hidden: { type: 'object' },
    },
  });

  it('非空值按控件类型归一进 content', () => {
    const content = collectElicitationContent(plan, {
      pick: 'a',
      free: ' 自由文本 ',
      multi: ['x', 3, null, 'y'],
      count: 2.5,
      flag: true,
      hidden: { raw: true },
    });
    expect(content).toEqual({ pick: 'a', free: ' 自由文本 ', multi: ['x', 'y'], count: 2.5, flag: true });
  });

  it('空值剔除：未选/空串/空数组/NaN/undefined 均不下发', () => {
    const content = collectElicitationContent(plan, {
      pick: '',
      free: '   ',
      multi: [],
      count: Number.NaN,
      flag: false,
    });
    expect(content).toEqual({});
  });

  it('switch 仅 true 下发（false 视为未触及）', () => {
    expect(collectElicitationContent(plan, { flag: true })).toEqual({ flag: true });
    expect(collectElicitationContent(plan, { flag: false })).toEqual({});
  });
});
