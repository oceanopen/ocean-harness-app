// 交互卡路由（T4.1，对标 orca NativeChatInteractiveCard，裁剪）：
// runtime notification 在场时按 toolName 路由——AskUserQuestion → 提问卡
// （替换 composer，卡内自带输入），其余 → 审批卡（composer 上方）。
//
// 作答后本地消隐：notification 要等 claude 的下一个事件才清（答完它仍在场），
// 已答记忆按 **notification 实例** 为界——Rust 每次 Apply 都 emit 新 payload
// 对象，identity 变化即新一轮请求。不能用纯内容键：本仓事件集比 orca 窄
// （无 PostToolUse 注册、普通 PreToolUse 有意 Drop），一条 assistant 消息内
// 连续两次同工具审批之间没有任何事件清 notification，同键会把第二次误判
// 已答 → 卡片消隐 + composer 锁死在 Waiting 态（内容级去重连 toolInput 都
// 不可靠——claude 重试同一命令时同内容）。
//
// 在途应答链防护：换新交互卡（notification 实例变化）时中止旧链余组——多问
// 应答是 1s/组的步进写（见 chatInteractiveSend），claude 快速消化旧答案并
// 抛出新问题时，旧链余组（导航键/回车）若不中止会写进新选择器（回车提交
// 新问的首选项）。

import type { ClaudeNotification } from '@src/shared/bindings';
import type { AskAnswerSelection, AskPrompt } from './chatAsk';
import { useEffect, useMemo, useRef, useState } from 'react';
import { APPROVAL_DENY, askDismissKey, isAskToolName, parseAskQuestions } from './chatAsk';
import NativeChatApprovalCard from './NativeChatApprovalCard';
import NativeChatQuestionCard from './NativeChatQuestionCard';

/** 路由产物：提问卡（带解析后的提问集）或审批卡（原 notification）。 */
type InteractiveCard
  = | { kind: 'question'; prompt: AskPrompt; mountKey: string }
    | { kind: 'approval'; notification: ClaudeNotification };

interface NativeChatInteractiveCardProps {
  // runtime notification（useTranscript 透传；非 waiting 态为 null）。
  notification: ClaudeNotification | null;
  // 提问卡提交（父层 buildAskAnswerKeys → sendInteractiveKeys 步进发送）。
  onAnswer: (prompt: AskPrompt, selections: AskAnswerSelection[]) => void;
  // 写原始按键串（审批选项数字 / ESC 取消）。
  onRawKeys: (raw: string) => void;
  // 中止在途应答链余组（父层接 cancelInteractiveSends）。
  onCancelPendingKeys: () => void;
  // 提问卡在场通告（父层据此用卡替换 composer；离场复位 false）。
  onQuestionActiveChange: (active: boolean) => void;
}

export default function NativeChatInteractiveCard({
  notification,
  onAnswer,
  onRawKeys,
  onCancelPendingKeys,
  onQuestionActiveChange,
}: NativeChatInteractiveCardProps) {
  const card = useMemo<InteractiveCard | null>(() => {
    if (notification == null) {
      return null;
    }
    if (isAskToolName(notification.toolName)) {
      const prompt = parseAskQuestions(notification.toolInput);
      if (prompt != null) {
        // mountKey 用内容键：提问集变化即重挂载（内部作答状态归零）。
        return { kind: 'question', prompt, mountKey: askDismissKey(prompt)! };
      }
      // 提问解析失败（载荷漂移/畸形）：回落审批卡形态。'1'/'2' 在选择器语境
      // 即选首行/次行，ESC 取消——功能语义仍成立，用户不被晾在无出口的等待态。
    }
    return { kind: 'approval', notification };
  }, [notification]);

  // 已答记忆按 notification 实例为界（渲染期 ref 守卫范式，同 useTranscript
  // lastPathRef）：identity 变化（新一轮 emit / 清空）即复位，新一轮审批或
  // 同款提问可再展示；纯内容键的误判场景见模块头。
  const [answered, setAnswered] = useState(false);
  const lastNotificationRef = useRef<ClaudeNotification | null>(null);
  if (lastNotificationRef.current !== notification) {
    lastNotificationRef.current = notification;
    if (answered) {
      setAnswered(false);
    }
  }

  // 换新交互卡（notification 实例变化且前后都在场）：中止旧应答链余组（防写进
  // 新选择器，见模块头）。首次在场（前值为 null）不中止——那是已答卡片的正常
  // 收尾窗口，链余组属用户已确认的作答，须写完。
  const lastCardRef = useRef<InteractiveCard | null>(null);
  useEffect(() => {
    if (card == null) {
      return;
    }
    const prev = lastCardRef.current;
    lastCardRef.current = card;
    if (prev != null && prev !== card) {
      onCancelPendingKeys();
    }
  }, [card, onCancelPendingKeys]);

  // 提问卡在场通告：父层据此替换 composer（卡的输入行是唯一输入面）。
  const showingQuestion = card != null && !answered && card.kind === 'question';
  useEffect(() => {
    onQuestionActiveChange(showingQuestion);
    return () => onQuestionActiveChange(false);
  }, [showingQuestion, onQuestionActiveChange]);

  if (card == null || answered) {
    return null;
  }
  if (card.kind === 'question') {
    return (
      <NativeChatQuestionCard
        key={card.mountKey}
        prompt={card.prompt}
        onAnswer={(selections) => {
          // 立即消隐：应答链余组由发送层掌管，链完成前 claude 已消化答案推进
          // 状态（Waiting 即被下一事件清掉）；万一状态不动，已答记忆防止卡复活
          // （直到 notification 实例变化）。
          setAnswered(true);
          onAnswer(card.prompt, selections);
        }}
        onCancel={() => {
          setAnswered(true);
          onRawKeys(APPROVAL_DENY);
        }}
      />
    );
  }
  return (
    <NativeChatApprovalCard
      notification={card.notification}
      onChoose={(send) => {
        setAnswered(true);
        onRawKeys(send);
      }}
    />
  );
}
