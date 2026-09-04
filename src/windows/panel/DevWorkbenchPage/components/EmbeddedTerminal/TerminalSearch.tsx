import type { ISearchOptions, SearchAddon } from '@xterm/addon-search';
import {
  ArrowDownward as ArrowDownwardIcon,
  ArrowUpward as ArrowUpwardIcon,
  CloseOutlined as CloseOutlinedIcon,
} from '@mui/icons-material';
import { Box, IconButton, InputBase, Typography, useTheme } from '@mui/material';
import { useEffect, useRef, useState } from 'react';
import { PANEL_TOOLBAR_HEIGHT } from '../PanelToolbar';

interface TerminalSearchProps {
  // TerminalView mount effect 建立的 SearchAddon 实例（本地 ref 桥直读）
  searchAddon: SearchAddon;
  // 搜索条背景色（终端背景色派生，overlay 与终端视觉衔接）
  background: string;
  onClose: () => void;
}

// TerminalSearch：终端搜索条 overlay（terminal_03 §3.1，官方 @xterm/addon-search 方案）。
// 终端区右上角浮层：输入即 incremental findNext + 大小写切换 + 上/下导航
// （Enter/Shift+Enter 键盘等价）+ 结果计数 + 关闭。
//
// 状态全部局部 state（用户交互型数据流 → state，terminal_03 §5.3）；输入不建
// ref 转发层。onDidChangeResults 订阅在 effect 建立随组件卸载断开；装饰清理
// 与焦点归还由父层 onClose 路径承担（clearDecorations + terminal.focus）。
export default function TerminalSearch({ searchAddon, background, onClose }: TerminalSearchProps) {
  const theme = useTheme();
  const [term, setTerm] = useState('');
  const [caseSensitive, setCaseSensitive] = useState(false);
  // 结果计数（onDidChangeResults 事件回填；初始 0/0）
  const [resultIndex, setResultIndex] = useState(0);
  const [resultCount, setResultCount] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  // 结果计数订阅：findNext/findPrevious/clearDecorations 触发回填。
  // resultIndex === -1 表示匹配数超 highlightLimit（装饰停更，计数语义取「>m」）。
  useEffect(() => {
    const dispose = searchAddon.onDidChangeResults(({ resultIndex: index, resultCount: count }) => {
      setResultIndex(index);
      setResultCount(count);
    });
    return () => {
      dispose.dispose();
    };
  }, [searchAddon]);

  // 搜索 options 统一构造：decorations 必传——addon 源码实证无 decorations 时
  // fireResultsChanged 直接 return，onDidChangeResults（计数）永不触发，且无
  // 匹配高亮。背景色须 #RRGGBB（typings 约束），明暗模式各取色。
  const searchOptions = (): ISearchOptions => ({
    caseSensitive,
    decorations: {
      matchBackground: theme.palette.mode === 'dark' ? '#3a3d41' : '#ffe082',
      activeMatchBackground: theme.palette.mode === 'dark' ? '#5a5d61' : '#ffca28',
      // overview ruler（滚动条侧边的匹配标记）本项目未启用，透明色占位满足必填
      matchOverviewRuler: '#00000000',
      activeMatchColorOverviewRuler: '#00000000',
    },
  });

  // 清空词条：清装饰 + 计数归零（装饰清后 addon 不再发事件，state 须手动复位）
  const clearResults = () => {
    searchAddon.clearDecorations();
    setResultIndex(0);
    setResultCount(0);
  };

  const runFind = (direction: 'next' | 'previous', incremental: boolean) => {
    if (term === '') {
      clearResults();
      return;
    }
    const options = {
      ...searchOptions(),
      // incremental：输入过程中尽量扩选已命中区间，少跳动
      incremental,
    };
    if (direction === 'next') {
      searchAddon.findNext(term, options);
    } else {
      searchAddon.findPrevious(term, options);
    }
  };

  // 输入即搜（incremental）；大小写切换后按当前词条重搜定位首个命中
  const handleTermChange = (value: string) => {
    setTerm(value);
    if (value === '') {
      clearResults();
      return;
    }
    searchAddon.findNext(value, { ...searchOptions(), incremental: true });
  };

  const toggleCaseSensitive = () => {
    const next = !caseSensitive;
    setCaseSensitive(next);
    if (term !== '') {
      searchAddon.findNext(term, searchOptions());
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      runFind(e.shiftKey ? 'previous' : 'next', false);
    } else if (e.key === 'Escape') {
      e.preventDefault();
      onClose();
    }
  };

  return (
    <Box
      sx={{
        position: 'absolute',
        // 工具栏下方 4px 间隙：高度走共享常量（原 top:32 是与旧工具栏高 28 的双写）
        top: PANEL_TOOLBAR_HEIGHT + 4,
        right: 8,
        // xterm 容器是静态定位不建堆叠上下文，其内部层（helpers z-5、
        // accessibility z-10、webgl canvas z-0..2）直接参与外层 z-index 竞争；
        // 搜索条须压过全部内部层（上限 accessibility 的 10），取 MUI 语义
        // 层级 mobileStepper(1000)。z:2 时对 5/10 直接被压、对平局 webgl
        // canvas 因 DOM 顺序在后仍被盖 → 点击落 canvas、xterm mousedown
        // 无条件 preventDefault → 输入框无法聚焦、按钮 click 失效。
        zIndex: theme.zIndex.mobileStepper,
        display: 'flex',
        alignItems: 'center',
        gap: 0.5,
        px: 0.5,
        py: 0.25,
        borderRadius: 1,
        border: '1px solid',
        borderColor: 'divider',
        // 终端背景色派生：明暗模式各取半透明叠层，浮层不遮死底下的输出
        bgcolor: background,
        boxShadow: theme.shadows[4],
      }}
      onKeyDown={handleKeyDown}
    >
      <InputBase
        inputRef={inputRef}
        value={term}
        onChange={e => handleTermChange(e.target.value)}
        placeholder="搜索"
        autoFocus
        sx={{
          width: 160,
          fontSize: 12,
          fontFamily: 'monospace',
          px: 0.5,
          color: theme.palette.mode === 'dark' ? '#d4d4d4' : '#333333',
        }}
        inputProps={{ 'aria-label': '终端内搜索' }}
      />
      {/* 大小写切换（toggle 态以 primary 色区分，aria-pressed 语义） */}
      <IconButton
        size="small"
        onClick={toggleCaseSensitive}
        aria-label="区分大小写"
        aria-pressed={caseSensitive}
        sx={{ color: caseSensitive ? 'primary.main' : 'text.secondary' }}
      >
        <Typography variant="caption" sx={{ fontWeight: 'bold', lineHeight: 1 }}>Aa</Typography>
      </IconButton>
      <IconButton
        size="small"
        onClick={() => runFind('previous', false)}
        aria-label="上一个匹配"
        sx={{ color: 'text.secondary' }}
      >
        <ArrowUpwardIcon fontSize="small" />
      </IconButton>
      <IconButton
        size="small"
        onClick={() => runFind('next', false)}
        aria-label="下一个匹配"
        sx={{ color: 'text.secondary' }}
      >
        <ArrowDownwardIcon fontSize="small" />
      </IconButton>
      {/* 计数：超 highlightLimit（resultIndex=-1）显示「>m」 */}
      <Typography variant="caption" sx={{ color: 'text.secondary', minWidth: 32, textAlign: 'center', lineHeight: 1 }}>
        {resultIndex === -1 ? `>${resultCount}` : resultCount > 0 ? `${resultIndex + 1}/${resultCount}` : '0/0'}
      </Typography>
      <IconButton size="small" onClick={onClose} aria-label="关闭搜索" sx={{ color: 'text.secondary' }}>
        <CloseOutlinedIcon fontSize="small" />
      </IconButton>
    </Box>
  );
}
