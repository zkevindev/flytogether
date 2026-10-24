import { useState } from 'react';
import { AirplaneTilt, Bomb, DiceFive, Lightning, ShieldCheck, X } from '@phosphor-icons/react';
import type { Game } from '../lib/types';

export const propCards = [
  { id: 'launch', name: '起飞卡', effect: '任意点数也能起飞', Icon: AirplaneTilt, color: '#cf574c' },
  { id: 'boost', name: '加速卡', effect: '在飞的飞机多走 2 格', Icon: Lightning, color: '#b07716' },
  { id: 'luck', name: '幸运骰', effect: '立即掷两次，选较大点数', Icon: DiceFive, color: '#387fc0' },
  { id: 'bomb', name: '炸弹', effect: '送回对手一架航线飞机', Icon: Bomb, color: '#99543c' },
  { id: 'shield', name: '护盾', effect: '抵挡一次炸弹或撞机', Icon: ShieldCheck, color: '#25866f' },
];

export function PropTray({ game, status = 'playing', disabled, onUse }: { game: Game; status?: string; disabled: boolean; onUse: (id: string, target?: string) => void }) {
  const [targeting, setTargeting] = useState('');
  if (!game.props_enabled || game.phase === 'finished') return null;
  const player = game.players[game.current];
  const active = propCards.find(p => p.id === game.active_prop);
  const targets = (id: string) => game.players.flatMap((owner, i) => owner.pieces.filter(p =>
    p.progress > 0 && p.progress <= game.board.ring_length && (id === 'bomb' ? i !== game.current : i === game.current && !p.shield)
  ).map(piece => ({ owner, piece })));
  return <section id="prop-pocket" className="prop-tray" aria-label="本回合道具">
    <div className="prop-heading"><strong>{player.name}的道具口袋</strong><span>{status !== 'playing' ? (status === 'paused' ? '游戏暂停 · 库存与效果保留' : status === 'ended' ? '本局道具记录' : '开始游戏后可使用') : active ? `${active.name}${['bomb', 'shield'].includes(active.id) ? (game.phase === 'move' ? '已生效 · 请选择飞机' : '已生效 · 请掷骰子') : game.phase === 'move' ? '生效中 · 请选择飞机' : '已就绪 · 请掷骰子'}` : '掷骰前选一张 · 每次掷骰限一张'}</span></div>
    <div className="prop-cards">{propCards.map(({ id, name, effect, Icon, color }) => {
      const count = player.props?.[id] || 0;
      const needsPlane = id === 'boost' && !player.pieces.some(p => p.progress >= 0 && p.progress < game.board.finish);
      const noHangar = id === 'launch' && !player.pieces.some(p => p.progress < 0);
      const targeted = id === 'bomb' || id === 'shield', noTarget = targeted && !targets(id).length;
      const selected = game.active_prop === id;
      const unavailable = disabled || game.phase !== 'roll' || !!game.active_prop || count === 0 || needsPlane || noHangar || noTarget;
      const hint = selected && status !== 'playing' ? '已选道具 · 效果保留' : selected ? (game.phase === 'move' ? '效果已生效，请选飞机' : targeted ? '已生效，请继续掷骰' : '本次掷骰生效') : count === 0 ? (game.preset === 'gentle' && id === 'bomb' ? '亲子轻松不提供炸弹' : '已经用完') : needsPlane ? '需先有飞机起飞' : noHangar ? '飞机都已起飞' : noTarget ? '暂无可选择的航线飞机' : effect;
      return <button key={id} className={'prop-card ' + (selected || targeting === id ? 'selected' : '')} disabled={unavailable} onClick={() => targeted ? setTargeting(id) : onUse(id)} aria-label={`使用${name}`} title={hint} style={{ '--prop-color': color } as React.CSSProperties}>
        <span className="prop-symbol"><Icon weight="fill" size={23} /></span><span className="prop-copy"><strong>{name}<small>{selected ? '已使用' : `×${count}`}</small></strong><span>{hint}</span></span>
      </button>;
    })}</div>
    {targeting && <div className="prop-targets"><div><strong>{targeting === 'bomb' ? '选择炸弹目标' : '选择要保护的飞机'}</strong><button className="icon-button" aria-label="取消道具选择" onClick={() => setTargeting('')}><X size={18} /></button></div><p>只可选择公共航线上的飞机。点选后立即生效。</p>{targets(targeting).map(({ owner, piece }) => <button className="prop-target" disabled={disabled || !!game.active_prop || game.phase !== 'roll'} key={piece.id} onClick={() => onUse(targeting, piece.id)}>{owner.name}的{piece.label}{piece.shield ? ' · 将抵消护盾' : ''}</button>)}</div>}
  </section>;
}
