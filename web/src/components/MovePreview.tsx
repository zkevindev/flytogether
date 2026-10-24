import { AirplaneTakeoff, ArrowRight, Flag, ShieldCheck, Sparkle, X } from '@phosphor-icons/react';
import type { Coach, Game, Move } from '../lib/types';

export function MovePreview({ game, move, onConfirm, onCancel, disabled, coach, onCoach }: { game: Game; move: Move; onConfirm: () => void; onCancel: () => void; disabled: boolean; coach?: Coach; onCoach?: (choice: string) => void }) {
  const destinations = (ids: string[]) => game.players.flatMap(player => player.pieces.filter(piece => ids.includes(piece.id)).map(piece => `${player.name}的${piece.label}`));
  const captures = destinations(move.captured), shields = destinations(move.blocked || []);
  const position = move.finish ? '终点' : move.to === 0 ? '起飞位' : move.to > game.board.ring_length ? `终点跑道第 ${move.to - game.board.ring_length} 格` : `本方航线第 ${move.to} 格`;
  return <section tabIndex={-1} className="move-preview" aria-label="飞行落点预览" data-piece-id={move.piece_id} data-to={move.to}>
    <div className="preview-title"><span><Flag weight="fill" />{coach ? '灵矽建议 · ' : ''}{move.label}的预计落点</span><button className="icon-button" aria-label="取消落点预览" disabled={disabled && !!coach} onClick={onCancel}><X /></button></div>
    <p className="preview-destination">{move.from < 0 ? '机场' : move.from === 0 ? '起飞位' : `第 ${move.from} 格`}<ArrowRight />{position}</p>
    <p className="preview-summary" aria-live="polite">{move.summary}</p>
    <div className="preview-effects">{move.launch && <span><AirplaneTakeoff />起飞</span>}{move.jump && <span><Sparkle />同色跳跃</span>}{move.fly && <span><AirplaneTakeoff />飞行捷径</span>}{move.finish && <span><Flag />抵达终点</span>}{captures.length > 0 && <span className="capture-effect">送回：{captures.join('、')}</span>}{shields.length > 0 && <span><ShieldCheck />击破护盾：{shields.join('、')}，飞机留在原位</span>}</div>
    {coach && <div className="coach-detail"><p>{coach.risk}</p><small>按当前棋局估算，不含道具和后续变化。只是建议，确认后才移动。</small><div className="coach-choices"><button disabled={disabled} onClick={() => onCoach?.('next')}>另一架呢</button><button disabled={disabled} onClick={() => onCoach?.('safe')}>安全一点</button><button disabled={disabled} onClick={() => onCoach?.('previous')}>刚才那个</button></div><small>也可以直接说“就它”确认，或“取消建议”。</small></div>}
    <div className="preview-actions"><button className="text-button" disabled={disabled && !!coach} onClick={onCancel}>再想想</button><button className="primary-button" disabled={disabled} onClick={onConfirm}>确认飞行<ArrowRight /></button></div>
  </section>;
}
