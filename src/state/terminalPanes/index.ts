// terminalPanes 域对外 API（唯一入口）。消费方只从此处 import。
export { closeNode, layoutFor, loadLayout, removeLayout, saveLayout, setRatioNode, splitNode } from './actions';
export { ensureLayout, useTerminalPanesStore } from './store';
