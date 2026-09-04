import { defaultKeymap } from '@codemirror/commands';
import { bracketMatching, foldGutter, foldKeymap } from '@codemirror/language';
import { highlightSelectionMatches, search, searchKeymap } from '@codemirror/search';
import { Compartment, EditorState } from '@codemirror/state';
import {
  EditorView,
  highlightActiveLine,
  highlightActiveLineGutter,
  highlightSpecialChars,
  keymap,
  lineNumbers,
  scrollPastEnd,
} from '@codemirror/view';
import { Box, useTheme } from '@mui/material';
import { useEffect, useRef } from 'react';
import { buildCodeTheme } from './codeViewerTheme';
import { codeMirrorLanguage, codeMirrorLanguageFor } from './viewerKind';

interface CodeViewerProps {
  content: string;
  /// 语言分派依据（挂载时取值；切文件由父层按 path 重挂载，实例内不变）。
  path: string;
  /// 显式语言名（md 代码围栏场景：fence 语言标识优先于 path 推导；与 path 二选一传值）。
  language?: string;
  /// 滚动超底（CM scrollPastEnd，初始即给内容注入编辑器高度级的 padding-bottom）：
  /// 仅对高度受约束、内部滚动的查看器有意义——内容自适应高度的调用方（md 代码块）
  /// 必须传 false，否则块被 padding 撑出大片空白。
  scrollPastEnd?: boolean;
}

/// 只读代码查看器（CM6，扩展集与观感对齐 hello-halo 的 reader-first 配置）：行号/活动行/
/// 代码折叠（▸/▾）/Cmd+F 搜索面板/选词高亮/括号匹配/滚动超底 + 移植主题（MUI palette
/// 双模式）。只读态只挂 EditorState.readOnly（halo 同款）——刻意不加 editable(false)：后者
/// 会连光标/焦点一起禁掉（点击无闪烁光标），readOnly 单独即可防编辑且保留光标与选中，
/// 下期编辑（T5.1 后续）摘掉这一行即得编辑器（届时补 history/indentWithTab 键位）。
/// mount 创建 / unmount destroy（StrictMode 双挂载安全）；content 变更（staleTime 0 重验
/// 回填）dispatch 全量替换 doc；明暗变更走 Compartment 热切（不重建实例不丢滚动）。
/// CM6 viewport 虚拟渲染，2MB 上限文本从容滚动。
export default function CodeViewer({ content, path, language, scrollPastEnd: scrollPastEndEnabled = true }: CodeViewerProps) {
  const theme = useTheme();
  const dark = theme.palette.mode === 'dark';
  const containerRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const themeCompartment = useRef(new Compartment()).current;

  useEffect(() => {
    const view = new EditorView({
      state: EditorState.create({
        doc: content,
        extensions: [
          EditorState.readOnly.of(true),
          // —— reader-first 扩展集（halo getBaseExtensions 的只读子集）——
          lineNumbers(),
          highlightActiveLineGutter(),
          highlightSpecialChars(),
          highlightActiveLine(),
          foldGutter({ closedText: '▸', openText: '▾' }),
          bracketMatching(),
          search({ top: true }),
          highlightSelectionMatches(),
          ...(scrollPastEndEnabled ? [scrollPastEnd()] : []),
          keymap.of([...defaultKeymap, ...searchKeymap, ...foldKeymap]),
          language != null ? codeMirrorLanguageFor(language) : codeMirrorLanguage(path),
          themeCompartment.of(buildCodeTheme(theme.palette, dark)),
        ],
      }),
      parent: containerRef.current ?? undefined,
    });
    viewRef.current = view;
    return () => {
      view.destroy();
      viewRef.current = null;
    };
  // 首挂载取各 prop 初值；后续变更走下方 dispatch/reconfigure，不重建实例（保滚动位置）。
  }, []);

  useEffect(() => {
    const view = viewRef.current;
    if (view == null || view.state.doc.toString() === content) {
      return;
    }
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: content } });
  }, [content]);

  useEffect(() => {
    viewRef.current?.dispatch({
      effects: themeCompartment.reconfigure(buildCodeTheme(theme.palette, dark)),
    });
  }, [theme.palette, dark]);

  return <Box ref={containerRef} sx={{ flex: 1, minHeight: 0, overflow: 'hidden' }} />;
}
