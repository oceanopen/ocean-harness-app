// chat 提问卡数据解析 + 应答按键构造（T4.1，对标 orca native-chat-ask.ts）：
// Rust 侧 PreToolUse(AskUserQuestion) 已把关进状态机（notification.toolInput
// 为 JSON 串），本模块做前端解析与「选项 → PTY 按键组」的纯转换。
//
// 关键事实（orca STA-1860 实测）：claude AskUserQuestion 是方向键选择器，
// 裸回车提交的是**高亮默认项**（首个选项），粘贴标签文本不会移动高亮——
// 按标签文本作答会把非首选项静默答成首选项。因此一律用选项的 1 起始序号
// 作答（数字即选中+提交），多选勾完用右方向键（\x1b[C）跳到 Submit 页签。
//
// 按键组模型（对齐 orca AskAnswerKeyGroup）：{raw} 原样写入；{text} 自由
// 文本由发送层跑 sanitize（buildChatPasteBytes）后写入——本模块保持纯函数
// 无 IO，分组间隔节奏（CHAT_ASK_STEP_MS）由 chatInteractiveSend 掌管。

import type { ClaudeNotification } from '@src/shared/bindings';

/** 单个选项：label 必有，description 可选（claude 载荷两种形态都收）。 */
export interface AskOption {
  label: string;
  description?: string;
}

/** 一问：question 文本 + 选项集 + 单选/多选标记。 */
export interface AskQuestion {
  question: string;
  header?: string;
  multiSelect: boolean;
  options: AskOption[];
}

/** 一次 AskUserQuestion 的完整提问集（≥1 问）。 */
export interface AskPrompt {
  questions: AskQuestion[];
}

/** 一问的作答：选中选项下标（选项序 0 起）+ 自由输入文本。 */
export interface AskAnswerSelection {
  indices: number[];
  other?: string;
}

/**
 * 一组待写按键：raw 原样写入（选项数字/回车/方向键）；text 为自由文本，
 * 由发送层过 sanitize 后写（本模块不做字节转换，保持无 IO）。
 */
export type AskAnswerKeyGroup = { raw: string } | { text: string };

/** 单个审批按钮：label 展示文案；send 为写回 PTY 的字面按键串。 */
export interface ApprovalButton {
  label: string;
  send: string;
}

// 选择器导航键（对齐 orca）：回车提交（同 chatSend CHAT_SUBMIT 值）；
// 右方向键跳下一页签（多问题→下一问，末问→Submit 页签）。
const ASK_SUBMIT = '\r';
const ASK_NEXT_TAB = '\x1B[C';
// 拒绝/中断键：claude TUI 的 ESC——审批对话框即 deny，提问选择器即取消。
// 提问卡取消与审批卡拒绝色判定共用（单源导出）。
export const APPROVAL_DENY = '\x1B';
// 审批建议缺省时的回落按钮（对齐 orca：Allow='1'，Always allow='2'）。
const APPROVAL_FALLBACK_LABELS = ['允许', '总是允许'];

/**
 * 解析 notification.toolInput（JSON 串）→ AskPrompt。宽容解析：
 * JSON 损坏 / 无 questions / questions 全空 → null（卡片层按 null 不渲染，
 * 不 panic）。选项收 string 与 {label,description} 两种形态。
 */
export function parseAskQuestions(toolInputJson: string | null | undefined): AskPrompt | null {
  if (!toolInputJson) {
    return null;
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(toolInputJson);
  } catch {
    return null;
  }
  return parseQuestionsShape(parsed);
}

/** questions 形态解析（orca parseQuestionsShape 同款宽容规则）。 */
function parseQuestionsShape(input: unknown): AskPrompt | null {
  if (!input || typeof input !== 'object') {
    return null;
  }
  const rawQuestions = (input as { questions?: unknown }).questions;
  if (!Array.isArray(rawQuestions) || rawQuestions.length === 0) {
    return null;
  }
  const questions: AskQuestion[] = [];
  for (const raw of rawQuestions) {
    if (!raw || typeof raw !== 'object') {
      continue;
    }
    const question = raw as Record<string, unknown>;
    const text = typeof question.question === 'string' ? question.question : '';
    const options = parseAskOptions(question.options);
    if (text || options.length > 0) {
      questions.push({
        question: text,
        header: typeof question.header === 'string' ? question.header : undefined,
        multiSelect: question.multiSelect === true,
        options,
      });
    }
  }
  return questions.length > 0 ? { questions } : null;
}

/** 选项解析：string → {label}；对象 → {label, description?}；其余丢弃。 */
function parseAskOptions(raw: unknown): AskOption[] {
  if (!Array.isArray(raw)) {
    return [];
  }
  return raw
    .map((option): AskOption | null => {
      if (typeof option === 'string') {
        return { label: option };
      }
      if (
        option
        && typeof option === 'object'
        && typeof (option as { label?: unknown }).label === 'string'
      ) {
        return {
          label: (option as { label: string }).label,
          description:
            typeof (option as { description?: unknown }).description === 'string'
              ? (option as { description: string }).description
              : undefined,
        };
      }
      return null;
    })
    .filter((option): option is AskOption => option !== null);
}

/**
 * 提问卡去重键：同一提问集（JSON 序列化）同一键。作答后 notification 仍
 * 在场（要等下一个事件才清），卡片层用此键本地记忆「已答」，避免答完还挂着。
 */
export function askDismissKey(prompt: AskPrompt | null): string | null {
  return prompt ? `question:${JSON.stringify(prompt.questions)}` : null;
}

/**
 * toolName 归一判型（Rust is_ask_user_question 同口径）：剥非字母数字并小写
 * 后等于 askuserquestion，宽容拼写变体。卡片层据此路由提问卡 / 审批卡。
 */
export function isAskToolName(toolName: string | null | undefined): boolean {
  if (!toolName) {
    return false;
  }
  return toolName.replace(/[^a-z0-9]/gi, '').toLowerCase() === 'askuserquestion';
}

/**
 * 一问是否有可提交的作答（选了选项或填了自由文本）——提问卡 tab 勾标与
 * 提交闸门共用的唯一判定（单源，勿在视图层重写）。
 */
export function isAskAnswered(selection: AskAnswerSelection | undefined): boolean {
  return (selection?.indices.length ?? 0) > 0 || (selection?.other ?? '').trim().length > 0;
}

/**
 * 一问的作答标签（选中 label + 自由文本，选项序）——经 Type-something 行
 * 提交时拼成一个字符串。
 */
function answerLabels(question: AskQuestion, selection: AskAnswerSelection | undefined): string[] {
  const labels = (selection?.indices ?? [])
    .map(i => question.options[i]?.label ?? '')
    .filter(label => label.length > 0);
  const other = (selection?.other ?? '').trim();
  return other ? [...labels, other] : labels;
}

/**
 * 构造回答 AskUserQuestion 的有序按键组（对齐 orca buildAskAnswerKeys）：
 *   - 单选选中 → 选项序号（数字即选中+提交；多问题下自动跳下一问）
 *   - 自由文本 → Type-something 行号（选项数+1）+ 文本 + 回车
 *   - 多选 → 逐个序号勾选（不自动跳题），再右方向键进 Submit 页签
 *   - 多问题 / 单独多选收尾停在 Submit 页签，末尾补一个回车
 *   - 多问题中的未答之问 → 右方向键跳过
 * 组间由发送层按步进间隔写入（导航键与回车同批会被提前提交，见模块头）。
 */
export function buildAskAnswerKeys(
  prompt: AskPrompt,
  selections: AskAnswerSelection[],
): AskAnswerKeyGroup[] {
  const questions = prompt.questions;
  const multiQuestion = questions.length > 1;
  const groups: AskAnswerKeyGroup[] = [];

  questions.forEach((question, questionIndex) => {
    const selection = selections[questionIndex];
    const other = (selection?.other ?? '').trim();
    // Type-something 行号：选项之后追加的自由输入行。
    const typeSomething = String(question.options.length + 1);

    if (question.multiSelect) {
      for (const index of selection?.indices ?? []) {
        groups.push({ raw: String(index + 1) });
      }
      if (other) {
        groups.push({ raw: typeSomething }, { text: other }, { raw: ASK_SUBMIT });
      }
      // 多选不自动跳题：显式右方向键进下一页签（末问即 Submit）。
      groups.push({ raw: ASK_NEXT_TAB });
    } else if (other) {
      // 单选只能带一个值：含自由文本的作答整体经 Type-something 行提交。
      groups.push(
        { raw: typeSomething },
        { text: answerLabels(question, selection).join(', ') },
        { raw: ASK_SUBMIT },
      );
    } else if ((selection?.indices.length ?? 0) > 0) {
      groups.push({ raw: String(selection!.indices[0]! + 1) });
    } else if (multiQuestion) {
      // 多问题中的未答之问：右方向键跳过。
      groups.push({ raw: ASK_NEXT_TAB });
    }
  });

  // 多问题 / 单独多选最终停在 Submit 页签：补一个回车确认提交。
  const endsOnSubmitTab
    = multiQuestion || (questions.length === 1 && questions[0]!.multiSelect === true);
  if (endsOnSubmitTab && groups.length > 0) {
    groups.push({ raw: ASK_SUBMIT });
  }
  return groups;
}

/**
 * 审批卡按钮集（T4.1「动态建议+回落」）：
 *   - permission_suggestions 在场 → 按序号生成按钮（建议即 claude 对话框
 *     的选项文案，序号 i → send String(i+1)），末尾追加拒绝（ESC）；
 *   - 缺省 → 回落「允许 '1' / 总是允许 '2' / 拒绝 ESC」。
 * suggestions 非字符串数组（Rust 已宽容为 null）按缺省处理。
 */
export function approvalButtons(notification: ClaudeNotification): ApprovalButton[] {
  const suggestions = notification.permissionSuggestions ?? [];
  const allows
    = suggestions.length > 0
      ? suggestions.map((label, i) => ({ label, send: String(i + 1) }))
      : APPROVAL_FALLBACK_LABELS.map((label, i) => ({ label, send: String(i + 1) }));
  return [...allows, { label: '拒绝', send: APPROVAL_DENY }];
}
