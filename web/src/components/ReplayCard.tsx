import { useState } from 'react';
import type { Game } from '../lib/types';
import { copyReplayText, replayCardSVG, replayCardData, saveReplayCard } from '../lib/replay-card';

export function ReplayCard({ game }: { game: Game }) {
  const [status, setStatus] = useState('');
  const data = replayCardData(game);
  if (!data) return null;
  const image = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(replayCardSVG(game))}`;
  return <details className="replay-card">
    <summary>制作家庭回顾卡</summary>
    <p>只放本局真实比分和棋谱里记下的精彩瞬间，不带房间链接或家庭档案。</p>
    <img src={image} alt={`${data.winner}获胜的家庭飞行棋回顾卡`} />
    <div className="replay-card-actions">
      <button className="primary-button" onClick={async () => { try { await saveReplayCard(game); setStatus('图片已保存，可以发给家人啦。'); } catch (error) { setStatus(error instanceof Error ? error.message : '图片没有保存成功'); } }}>保存回顾图片</button>
      <button className="outline-button" onClick={async () => { try { await copyReplayText(game); setStatus('回顾文字已复制。'); } catch { setStatus('复制没有成功，请检查浏览器的剪贴板权限。'); } }}>复制分享文字</button>
    </div>
    <p className="replay-card-status" role="status" aria-live="polite">{status}</p>
  </details>;
}
