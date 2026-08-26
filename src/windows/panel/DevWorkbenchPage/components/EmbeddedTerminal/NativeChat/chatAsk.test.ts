// chatAsk 单测（T4.1）：宽容解析 / 序号按键构造（STA-1860 语义）/ 审批按钮。

import type { ClaudeNotification } from '@src/shared/bindings';
import { describe, expect, it } from 'vitest';
import {
  approvalButtons,
  askDismissKey,
  buildAskAnswerKeys,
  isAskToolName,
  parseAskQuestions,
} from './chatAsk';

const ENTER = '\r';
const NEXT_TAB = '\x1B[C';
const ESC = '\x1B';

function notification(overrides: Partial<ClaudeNotification> = {}): ClaudeNotification {
  return {
    message: 'question',
    toolName: 'AskUserQuestion',
    permissionSuggestions: null,
    ...overrides,
  };
}

describe('parseAskQuestions', () => {
  it('标准形态：questions + 选项两种形态（string / {label,description}）', () => {
    const prompt = parseAskQuestions(
      JSON.stringify({
        questions: [
          {
            question: '用哪个库？',
            header: '技术选型',
            options: [
              'React',
              { label: 'Vue', description: '渐进式框架' },
              { label: 'Svelte' },
            ],
          },
        ],
      }),
    );
    expect(prompt).not.toBeNull();
    expect(prompt!.questions).toHaveLength(1);
    const q = prompt!.questions[0]!;
    expect(q.question).toBe('用哪个库？');
    expect(q.header).toBe('技术选型');
    expect(q.multiSelect).toBe(false);
    expect(q.options).toEqual([
      { label: 'React', description: undefined },
      { label: 'Vue', description: '渐进式框架' },
      { label: 'Svelte', description: undefined },
    ]);
  });

  it('多问 + multiSelect 标记透传', () => {
    const prompt = parseAskQuestions(
      JSON.stringify({
        questions: [
          { question: '语言？', options: ['TS', 'JS'] },
          { question: '特性？', options: ['a', 'b', 'c'], multiSelect: true },
        ],
      }),
    );
    expect(prompt!.questions).toHaveLength(2);
    expect(prompt!.questions[1]!.multiSelect).toBe(true);
  });

  it('宽容丢弃：非对象 question / 无 label 选项 / 空 question 且无选项的行', () => {
    const prompt = parseAskQuestions(
      JSON.stringify({
        questions: [
          null,
          42,
          { question: '有效', options: ['ok', { description: '无 label 被丢' }, 7] },
          { question: '', options: [] },
        ],
      }),
    );
    expect(prompt!.questions).toHaveLength(1);
    expect(prompt!.questions[0]!.question).toBe('有效');
    expect(prompt!.questions[0]!.options).toEqual([{ label: 'ok', description: undefined }]);
  });

  it('损坏 JSON / 无 questions / 全空 questions / null 输入 → null', () => {
    expect(parseAskQuestions('{broken')).toBeNull();
    expect(parseAskQuestions('{"other":1}')).toBeNull();
    expect(parseAskQuestions('{"questions":[]}')).toBeNull();
    expect(parseAskQuestions('{"questions":[{"question":"","options":[]}]}')).toBeNull();
    expect(parseAskQuestions(null)).toBeNull();
    expect(parseAskQuestions(undefined)).toBeNull();
  });
});

describe('askDismissKey', () => {
  it('同提问集同键，不同提问集不同键，null → null', () => {
    const a = parseAskQuestions('{"questions":[{"question":"Q1","options":["a"]}]}');
    const b = parseAskQuestions('{"questions":[{"question":"Q1","options":["a"]}]}');
    const c = parseAskQuestions('{"questions":[{"question":"Q2","options":["a"]}]}');
    expect(askDismissKey(a)).toBe(askDismissKey(b));
    expect(askDismissKey(a)).not.toBe(askDismissKey(c));
    expect(askDismissKey(null)).toBeNull();
  });
});

describe('isAskToolName', () => {
  it('宽容拼写变体；普通工具名 / 空值 → false', () => {
    expect(isAskToolName('AskUserQuestion')).toBe(true);
    expect(isAskToolName('ask_user_question')).toBe(true);
    expect(isAskToolName('askUserQuestion')).toBe(true);
    expect(isAskToolName('Bash')).toBe(false);
    expect(isAskToolName('AskUserQuestionExtra')).toBe(false);
    expect(isAskToolName(null)).toBe(false);
    expect(isAskToolName(undefined)).toBe(false);
  });
});

describe('buildAskAnswerKeys', () => {
  it('单问单选：仅选项序号（数字即选中+提交，无回车无导航）', () => {
    const prompt = parseAskQuestions(
      '{"questions":[{"question":"Q1","options":["React","Vue","Svelte"]}]}',
    )!;
    expect(buildAskAnswerKeys(prompt, [{ indices: [1] }])).toEqual([{ raw: '2' }]);
  });

  it('单问多选：逐序号勾选 + 右方向键进 Submit + 回车', () => {
    const prompt = parseAskQuestions(
      '{"questions":[{"question":"Q1","options":["a","b","c"],"multiSelect":true}]}',
    )!;
    expect(buildAskAnswerKeys(prompt, [{ indices: [0, 2] }])).toEqual([
      { raw: '1' },
      { raw: '3' },
      { raw: NEXT_TAB },
      { raw: ENTER },
    ]);
  });

  it('单问单选 + 自由文本：Type-something 行号 + label 与文本拼串 + 回车', () => {
    const prompt = parseAskQuestions(
      '{"questions":[{"question":"Q1","options":["a","b"]}]}',
    )!;
    const keys = buildAskAnswerKeys(prompt, [{ indices: [0], other: '补充说明' }]);
    expect(keys).toEqual([{ raw: '3' }, { text: 'a, 补充说明' }, { raw: ENTER }]);
  });

  it('单问纯自由文本（未选选项）：Type-something 行号 + 文本 + 回车', () => {
    const prompt = parseAskQuestions('{"questions":[{"question":"Q1","options":["a"]}]}')!;
    expect(buildAskAnswerKeys(prompt, [{ indices: [], other: '自由作答' }])).toEqual([
      { raw: '2' },
      { text: '自由作答' },
      { raw: ENTER },
    ]);
  });

  it('多问单选：各问序号自动跳题，末尾 Submit 回车', () => {
    const prompt = parseAskQuestions(
      '{"questions":[{"question":"Q1","options":["a","b"]},{"question":"Q2","options":["x","y"]}]}',
    )!;
    expect(buildAskAnswerKeys(prompt, [{ indices: [1] }, { indices: [0] }])).toEqual([
      { raw: '2' },
      { raw: '1' },
      { raw: ENTER },
    ]);
  });

  it('多问含未答之问：右方向键跳过（不答成默认首项）', () => {
    const prompt = parseAskQuestions(
      '{"questions":[{"question":"Q1","options":["a"]},{"question":"Q2","options":["x"]}]}',
    )!;
    expect(buildAskAnswerKeys(prompt, [{ indices: [] }, { indices: [0] }])).toEqual([
      { raw: NEXT_TAB },
      { raw: '1' },
      { raw: ENTER },
    ]);
  });

  it('多问含多选：多选勾选后显式右方向键，其余按序号，末尾回车', () => {
    const prompt = parseAskQuestions(
      JSON.stringify({
        questions: [
          { question: 'Q1', options: ['a', 'b'], multiSelect: true },
          { question: 'Q2', options: ['x', 'y'] },
        ],
      }),
    )!;
    expect(buildAskAnswerKeys(prompt, [{ indices: [0, 1] }, { indices: [1] }])).toEqual([
      { raw: '1' },
      { raw: '2' },
      { raw: NEXT_TAB },
      { raw: '2' },
      { raw: ENTER },
    ]);
  });

  it('多选含自由文本：勾选 + Type-something 行号 + 文本 + 回车 + Submit 回车', () => {
    const prompt = parseAskQuestions(
      '{"questions":[{"question":"Q1","options":["a","b"],"multiSelect":true}]}',
    )!;
    expect(buildAskAnswerKeys(prompt, [{ indices: [1], other: '备注' }])).toEqual([
      { raw: '2' },
      { raw: '3' },
      { text: '备注' },
      { raw: ENTER },
      { raw: NEXT_TAB },
      { raw: ENTER },
    ]);
  });

  it('全未答 → 空组（卡片 confirm 闸门拦下，防御）', () => {
    const prompt = parseAskQuestions('{"questions":[{"question":"Q1","options":["a"]}]}')!;
    expect(buildAskAnswerKeys(prompt, [{ indices: [] }])).toEqual([]);
  });
});

describe('approvalButtons', () => {
  it('无 suggestions → 回落「允许 1 / 总是允许 2 / 拒绝 ESC」', () => {
    expect(approvalButtons(notification())).toEqual([
      { label: '允许', send: '1' },
      { label: '总是允许', send: '2' },
      { label: '拒绝', send: ESC },
    ]);
  });

  it('有 suggestions → 按序号生成允许按钮 + 拒绝 ESC', () => {
    const buttons = approvalButtons(
      notification({ permissionSuggestions: ['Allow once', 'Always allow in docs/'] }),
    );
    expect(buttons).toEqual([
      { label: 'Allow once', send: '1' },
      { label: 'Always allow in docs/', send: '2' },
      { label: '拒绝', send: ESC },
    ]);
  });

  it('空数组 suggestions 按缺省回落（不生成只有拒绝的卡）', () => {
    expect(approvalButtons(notification({ permissionSuggestions: [] }))).toEqual(
      approvalButtons(notification()),
    );
  });
});
