// 提问卡（T4.1，对标 orca NativeChatQuestionCard，MUI 化 + 裁剪）：
// AskUserQuestion 在场时替换 composer（卡内自带自由输入行，是唯一的输入面）。
//
// 交互规则（保留 orca 实测结论）：
//   - 选项点击只**高亮**不提交——首次点击即自动提交会在用户看到任何反馈前
//     关掉卡片（体感「点了没反应」），提交是显式动作（尾部按钮/回车）。
//   - 序号徽标即 claude 选择器提交的行号（STA-1860，按标签文本作答会静默
//     答成首选项），徽标视觉与提交语义一致。
//   - 多问题：顶部页签步进；未答之问可跳过（尾部按钮变「跳过」），跳过不
//     丢弃其他问已作答（提交在最后一问统一触发）。
//   - 最后一问空答时：回车为 no-op（防反射性回车扔掉整个提问），显式点
//     「跳过」才取消（发 ESC）。

import type { AskAnswerSelection, AskPrompt } from './chatAsk';
import {
  CheckOutlined as CheckIcon,
  CloseOutlined as CloseIcon,
  EditOutlined as EditIcon,
} from '@mui/icons-material';
import { Box, Button, IconButton, InputBase, Typography } from '@mui/material';
import { useState } from 'react';
import { isAskAnswered } from './chatAsk';

interface NativeChatQuestionCardProps {
  prompt: AskPrompt;
  // 提交作答（父层 buildAskAnswerKeys → chatInteractiveSend 步进发送）。
  onAnswer: (selections: AskAnswerSelection[]) => void;
  // 取消（父层发 ESC 中止提问）。
  onCancel: () => void;
}

export default function NativeChatQuestionCard({ prompt, onAnswer, onCancel }: NativeChatQuestionCardProps) {
  const [index, setIndex] = useState(0);
  // 选项身份用下标：label 只是展示文本不保证唯一，claude 选择器按行号提交。
  const [selections, setSelections] = useState<number[][]>(() => prompt.questions.map(() => []));
  const [others, setOthers] = useState<string[]>(() => prompt.questions.map(() => ''));

  const total = prompt.questions.length;
  const isLast = index === total - 1;
  const question = prompt.questions[index]!;

  // 一问的已答判据（chatAsk.isAskAnswered 单源）：tab 勾标 + 提交闸门共用。
  const answeredAt = (qi: number): boolean =>
    isAskAnswered({ indices: selections[qi] ?? [], other: others[qi] ?? '' });

  const pickOption = (optionIndex: number): void => {
    setSelections((prev) => {
      const next = prev.map(s => [...s]);
      const current = next[index] ?? [];
      if (question.multiSelect) {
        next[index] = current.includes(optionIndex)
          ? current.filter(picked => picked !== optionIndex)
          : [...current, optionIndex].sort((a, b) => a - b);
      } else {
        next[index] = current.includes(optionIndex) ? [] : [optionIndex];
      }
      return next;
    });
  };

  const submitAll = (): void => {
    onAnswer(prompt.questions.map((_, i) => ({
      indices: [...(selections[i] ?? [])],
      other: (others[i] ?? '').trim(),
    })));
  };

  // 任一问有答（末问提交闸门 + 尾部按钮文案共用：有答「提交」，全空「跳过」——
  // 全空点按钮是显式取消（发 ESC），勿显示「提交」误导）。
  const anyAnswered = prompt.questions.some((_, i) => answeredAt(i));

  // 尾部按钮 / 回车统一动作：非末问推进页签；末问有答提交、无答显式点击才取消。
  const confirm = (fromKeyboard: boolean): void => {
    if (!isLast) {
      setIndex(i => Math.min(i + 1, total - 1));
      return;
    }
    if (anyAnswered) {
      submitAll();
    } else if (!fromKeyboard) {
      onCancel();
    }
  };

  const currentAnswered = answeredAt(index);
  const trailingLabel = isLast
    ? (anyAnswered ? '提交' : '跳过')
    : (currentAnswered ? '下一问' : '跳过');

  return (
    <Box sx={{ flexShrink: 0, px: 1, pt: 1, pb: 0.5 }}>
      {/* 多问题页签：已答之问带勾标，可回跳修改（提交前作答可变） */}
      {total > 1 && (
        <Box sx={{ display: 'flex', gap: 0.5, overflowX: 'auto', pb: 1 }}>
          {prompt.questions.map((qq, i) => (
            <Button
              // 行位即身份：作答数组按下标存储，两问同 header 也是不同行。
              // eslint-disable-next-line react/no-array-index-key
              key={i}
              size="small"
              variant={i === index ? 'contained' : 'text'}
              startIcon={answeredAt(i) ? <CheckIcon /> : undefined}
              onClick={() => setIndex(i)}
              sx={{ flexShrink: 0, minWidth: 0, textTransform: 'none' }}
            >
              <Box component="span" sx={{ maxWidth: '10rem', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {qq.header || `第 ${i + 1} 问`}
              </Box>
            </Button>
          ))}
        </Box>
      )}

      <Box sx={{ border: 1, borderColor: 'divider', borderRadius: 1, overflow: 'hidden' }}>
        {/* 问题头：文本 + 取消 */}
        <Box sx={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 1, px: 1.5, py: 1 }}>
          <Typography variant="body2" sx={{ fontWeight: 600, wordBreak: 'break-word' }}>{question.question}</Typography>
          <IconButton size="small" onClick={onCancel} aria-label="取消提问" sx={{ flexShrink: 0 }}>
            <CloseIcon fontSize="small" />
          </IconButton>
        </Box>

        {/* 选项行（长列表内滚） */}
        <Box sx={{ maxHeight: '50vh', overflowY: 'auto', borderTop: 1, borderColor: 'divider' }}>
          {question.options.map((option, i) => {
            const selected = (selections[index] ?? []).includes(i);
            return (
              <Box
                // 行号即身份：claude 选择器按行号提交（STA-1860），label 不保证
                // 唯一，key 须含行位。
                // eslint-disable-next-line react/no-array-index-key
                key={`${i}:${option.label}`}
                component="button"
                type="button"
                aria-pressed={selected}
                onClick={() => pickOption(i)}
                sx={{
                  'display': 'flex',
                  'alignItems': 'flex-start',
                  'gap': 1.25,
                  'width': '100%',
                  'px': 1.5,
                  'py': 1,
                  'border': 0,
                  'borderTop': i > 0 ? '1px solid' : 0,
                  'borderColor': 'divider',
                  'cursor': 'pointer',
                  'textAlign': 'left',
                  'bgcolor': selected ? 'action.selected' : 'transparent',
                  '&:hover': { bgcolor: 'action.hover' },
                }}
              >
                <Box
                  sx={{
                    width: 24,
                    height: 24,
                    flexShrink: 0,
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    borderRadius: 0.5,
                    fontSize: 12,
                    bgcolor: selected ? 'primary.main' : 'action.hover',
                    color: selected ? 'primary.contrastText' : 'text.secondary',
                  }}
                >
                  {selected ? <CheckIcon sx={{ fontSize: 14 }} /> : i + 1}
                </Box>
                <Box sx={{ minWidth: 0 }}>
                  <Typography variant="body2" sx={{ wordBreak: 'break-word' }}>{option.label}</Typography>
                  {option.description != null && (
                    <Typography variant="caption" color="text.secondary" sx={{ display: 'block', wordBreak: 'break-word' }}>
                      {option.description}
                    </Typography>
                  )}
                </Box>
              </Box>
            );
          })}

          {/* 自由输入行（常驻）：Type-something 行的等价物，卡在场时的唯一文本输入 */}
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, px: 1.5, py: 1, borderTop: '1px solid', borderColor: 'divider' }}>
            <Box
              sx={{
                width: 24,
                height: 24,
                flexShrink: 0,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                borderRadius: 0.5,
                bgcolor: 'action.hover',
                color: 'text.secondary',
              }}
            >
              <EditIcon sx={{ fontSize: 14 }} />
            </Box>
            <InputBase
              value={others[index] ?? ''}
              onChange={e => setOthers(prev => prev.map((v, i) => (i === index ? e.target.value : v)))}
              onKeyDown={(e) => {
                // IME 组合输入（中文选字回车）不当提交（同 composer）。
                if (e.nativeEvent.isComposing) {
                  return;
                }
                if (e.key === 'Enter') {
                  e.preventDefault();
                  confirm(true);
                }
              }}
              placeholder="输入自定义答案"
              sx={{ flex: 1, minWidth: 0, fontSize: 14 }}
            />
            <Button
              size="small"
              variant={currentAnswered ? 'contained' : 'text'}
              onClick={() => confirm(false)}
            >
              {trailingLabel}
            </Button>
          </Box>
        </Box>
      </Box>

      {total > 1 && (
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', textAlign: 'right', mt: 0.5 }}>
          {index + 1}/{total}
        </Typography>
      )}
    </Box>
  );
}
