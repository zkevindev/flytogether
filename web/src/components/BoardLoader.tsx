import { Component, Suspense, lazy } from 'react';
import type { ReactNode } from 'react';
import type { BoardProps } from './BoardFrame';

const LazyBoard = lazy(() => import('./Board').then(module => ({ default: module.Board })));
const LazyLightBoard = lazy(() => import('./LightBoard').then(module => ({ default: module.LightBoard })));
class BoardBoundary extends Component<{ children: ReactNode; onLight?: () => void }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  render() {
    if (this.state.failed) return <div className="board-loading" role="alert"><p>棋盘暂时无法显示，重新加载后继续。</p>{this.props.onLight && <button className="outline-button" onClick={this.props.onLight}>使用轻量棋盘</button>}<button className="outline-button" onClick={() => location.reload()}>重新加载棋盘</button></div>;
    return this.props.children;
  }
}
export function BoardLoader({ light = false, onLight, ...props }: BoardProps & { light?: boolean; onLight?: () => void }) {
  return <BoardBoundary key={light ? 'flat' : 'auto'} onLight={!light ? onLight : undefined}><Suspense fallback={<div className="board-loading" role="status">正在铺开棋盘…</div>}>{light ? <LazyLightBoard {...props} /> : <LazyBoard {...props} />}</Suspense></BoardBoundary>;
}
