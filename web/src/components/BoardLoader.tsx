import { Component, Suspense, lazy } from 'react';
import type { ComponentProps, ReactNode } from 'react';
import type { Board } from './Board';

const LazyBoard = lazy(() => import('./Board').then(module => ({ default: module.Board })));
class BoardBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  render() {
    if (this.state.failed) return <div className="board-loading" role="alert"><p>棋盘暂时无法显示，重新加载后继续。</p><button className="outline-button" onClick={() => location.reload()}>重新加载棋盘</button></div>;
    return this.props.children;
  }
}
export function BoardLoader(props: ComponentProps<typeof Board>) {
  return <BoardBoundary><Suspense fallback={<div className="board-loading" role="status">正在铺开棋盘…</div>}><LazyBoard {...props} /></Suspense></BoardBoundary>;
}
