import { AirplaneTilt, DiceFive, Lightning } from '@phosphor-icons/react';
import type { Game } from '../lib/types';

export const propCards = [
  { id: 'launch', name: '起飞卡', effect: '任意点数也能起飞', Icon: AirplaneTilt, color: '#cf574c' },
  { id: 'boost', name: '加速卡', effect: '在飞的飞机多走 2 格', Icon: Lightning, color: '#b07716' },
  { id: 'luck', name: '幸运骰', effect: '掷两次，选较大点数', Icon: DiceFive, color: '#387fc0' },
];

export function PropTray({ game, disabled, onUse }: { game: Game; disabled: boolean; onUse: (id: string) => void }) {
  if (!game.props_enabled || game.phase === 'finished') return null;
  const player = game.players[game.current];
  const active = propCards.find(p => p.id === game.active_prop);
  return <section id="prop-pocket" className="prop-tray" aria-label="本回合道具">
    <div className="prop-heading"><strong>{player.name}的道具口袋</strong><span>{active ? `${active.name}${game.phase === 'move' ? '生效中 · 请选择飞机' : '已就绪 · 请掷骰子'}` : '掷骰前选一张 · 每种限一次'}</span></div>
    <div className="prop-cards">{propCards.map(({ id, name, effect, Icon, color }) => {
      const count = player.props?.[id] || 0;
      const needsPlane = id === 'boost' && !player.pieces.some(p => p.progress >= 0 && p.progress < game.board.finish);
      const noHangar = id === 'launch' && !player.pieces.some(p => p.progress < 0);
      const selected = game.active_prop === id;
      const unavailable = disabled || game.phase !== 'roll' || !!game.active_prop || count === 0 || needsPlane || noHangar;
      const hint = selected ? (game.phase === 'move' ? '效果已生效，请选飞机' : '本次掷骰生效') : count === 0 ? '已经用完' : needsPlane ? '需先有飞机起飞' : noHangar ? '飞机都已起飞' : effect;
      return <button key={id} className={'prop-card ' + (selected ? 'selected' : '')} disabled={unavailable} onClick={() => onUse(id)} aria-label={`使用${name}`} title={hint} style={{ '--prop-color': color } as React.CSSProperties}>
        <span className="prop-symbol"><Icon weight="fill" size={23} /></span><span className="prop-copy"><strong>{name}<small>{selected ? '已就绪' : `×${count}`}</small></strong><span>{hint}</span></span>
      </button>;
    })}</div>
  </section>;
}
