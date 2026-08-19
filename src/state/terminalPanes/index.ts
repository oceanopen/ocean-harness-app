// terminalPanes 域对外 API（唯一入口）。消费方只从此处 import。
export { closeNode, hasPane, isMainPane, layoutFor, leafPaneIds, splitNode } from './actions';
export { useTerminalPanesStore } from './store';
