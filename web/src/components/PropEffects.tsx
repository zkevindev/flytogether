import type { CSSProperties } from 'react';
import type { Game, GameEvent } from '../lib/types';
import { createBoardLayout, piecePosition } from '../lib/board-layout';
import { propCards } from './PropTray';
import './prop-effects.css';

// Only fresh authoritative events enter this layer; it never sends a game command.
export function PropEffects({event,game,light,reducedMotion}:{event:GameEvent;game:Game;light:boolean;reducedMotion:boolean}) {
  const card=propCards.find(c=>c.id===event.prop_id);
  const blocked=event.effects?.some(e=>e.kind==='blocked');
  const kind=blocked?'blocked':event.prop_id || (event.text.includes('护盾')?'shield':'bomb');
  const title=blocked?'护盾抵挡攻击':card?.name || '被撞回机场';
  const glyph:Record<string,string>={launch:'✈',boost:'ϟ',luck:'⚄',bomb:'✹',shield:'⬡',boomerang:'↶',rescue:'✚',escort:'⬡ ⬡',blocked:'⬡'};
  const layout=createBoardLayout(game.board);
  const targets=(event.effects || []).flatMap(effect=>game.players.flatMap((owner,i)=>owner.pieces.filter(p=>p.id===effect.piece_id).map(piece=>({effect,label:owner.name+'的'+piece.label,from:piecePosition(layout,game.board,i,{...piece,progress:effect.from},owner.pieces.length),to:piecePosition(layout,game.board,i,{...piece,progress:effect.to},owner.pieces.length)}))));
  return <div className={'prop-fx fx-'+kind+(reducedMotion?' is-still':'')} data-prop-fx={event.prop_id || 'collision'} data-event-id={event.id} aria-label={title}>
    <div className="prop-fx-arena" aria-hidden="true"><div className="fx-ring"/><div className="fx-ring fx-ring-two"/><span className="fx-glyph">{glyph[kind]}</span>{Array.from({length:12},(_,i)=><i key={i} className="fx-spark" style={{'--angle':`${i*30}deg`,'--delay':`${i%3*45}ms`} as CSSProperties}/>)}</div>
    {light && targets.length>0 && <svg className="prop-fx-paths" viewBox="0 0 720 720" aria-hidden="true">{targets.map(({effect,from,to})=><g key={effect.piece_id}><line x1={from.x} y1={from.y} x2={to.x} y2={to.y} className="fx-route"/><circle cx={to.x} cy={to.y} r="30" className="fx-target-ring"/><text x={to.x} y={to.y+7} textAnchor="middle" fontSize="24" fill="currentColor">{glyph[kind]}</text></g>)}</svg>}
    <div className="prop-fx-caption" role="status"><strong>{title}{blocked?' · 飞机留在原地':''}</strong><span>{event.text}</span></div>
  </div>;
}
