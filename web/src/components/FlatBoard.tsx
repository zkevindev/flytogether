import { useId } from 'react';
import type { Game, Piece } from '../lib/types';
import { colors, palette } from '../lib/types';
import { boardPieces, createBoardLayout, hangarPosition } from '../lib/board-layout';

const planePath = 'M-3-18 3-18 5-4 16 4 16 9 5 5 4 13 9 17 9 20 0 17-9 20-9 17-4 13-5 5-16 9-16 4-5-4Z';
const dicePips: Record<number, number[]> = { 0: [4], 1: [4], 2: [0, 8], 3: [0, 4, 8], 4: [0, 2, 6, 8], 5: [0, 2, 4, 6, 8], 6: [0, 2, 3, 5, 6, 8] };
const shortName = (name: string) => [...name].length > 8 ? [...name].slice(0, 7).join('') + '…' : name;

export function FlatBoard({ game, onPiece, disabled, onRoll, rolling = false }: {
  game: Game; onPiece: (piece: Piece, player: number) => void; disabled: boolean; onRoll: () => void; rolling?: boolean;
}) {
  const uid = useId().replaceAll(':', '');
  const layout = createBoardLayout(game.board), { unit, route, home, airports, pads } = layout;
  const canRoll = !disabled && game.phase === 'roll';
  const radius = game.board.layout === 'circle' ? 15 : unit * .47;
  const routePath = route.map(p => `${p.x},${p.y}`).join(' ') + ` ${route[0].x},${route[0].y}`;
  return <svg className={'board-svg board-' + game.board.layout} viewBox="0 0 720 720" role="group" aria-label={`${game.board.name}飞行棋棋盘`}>
    <defs>
      <filter id={uid + 'shadow'} x="-40%" y="-40%" width="180%" height="200%"><feDropShadow dx="0" dy="2" stdDeviation="2" floodColor="#254154" floodOpacity=".2" /></filter>
      <pattern id={uid + 'dots'} width="24" height="24" patternUnits="userSpaceOnUse"><circle cx="12" cy="12" r="1" fill="#d7e6ea" /></pattern>
    </defs>
    <rect x="3" y="3" width="714" height="714" rx="28" fill="#f0f8fc" stroke="#ccdee6" strokeWidth="2" />
    <rect x="12" y="12" width="696" height="696" rx="21" fill={`url(#${uid}dots)`} />
    <polyline points={routePath} fill="none" stroke="#d8e5eb" strokeWidth={unit * 1.05} strokeLinejoin="round" />
    {airports.map((airport, i) => {
      const player = game.players[i], color = palette[colors[i]], current = game.current === i && game.phase !== 'finished';
      const n = player?.pieces.length || (game.mode === 'quick' ? 2 : 4);
      return <g key={'airport' + i} className={'airport ' + (current ? 'current-airport' : '')} opacity={player ? 1 : .38}>
        <rect x={airport.x} y={airport.y} width={airport.size} height={airport.size} rx="18" fill={color} stroke={current ? '#263e52' : 'white'} strokeWidth={current ? 3 : 2} />
        <rect x={airport.x + 9} y={airport.y + 36} width={airport.size - 18} height={airport.size - 45} rx="12" fill="white" fillOpacity=".88" />
        <text x={airport.x + airport.size / 2} y={airport.y + 24} textAnchor="middle" fill="white" fontSize="15" fontWeight="750">{player ? shortName(player.name) : '等待登机'}</text>
        {Array.from({ length: n }, (_, j) => {
          const slot = hangarPosition(airport, j + 1, n);
          return <g key={j}><circle cx={slot.x} cy={slot.y} r={Math.min(27, airport.size * .16)} fill={color} fillOpacity=".12" stroke={color} strokeOpacity=".25" strokeDasharray="3 3" /><text x={slot.x} y={slot.y + 4} textAnchor="middle" fontSize="12" fill={color} opacity=".8">{j + 1}</text></g>;
        })}
      </g>;
    })}
    {colors.map((color, player) => {
      const from = route[(player * route.length / 4 + game.board.fly_from - 1) % route.length];
      const to = route[(player * route.length / 4 + game.board.fly_to - 1) % route.length];
      return <g key={'flight' + player} opacity=".48"><line x1={from.x} y1={from.y} x2={to.x} y2={to.y} stroke={palette[color]} strokeWidth="2" strokeDasharray="5 6" /><text x={(from.x + to.x) / 2 + 8} y={(from.y + to.y) / 2 - 8} fontSize="12" fontWeight="650" fill={palette[color]}>飞行</text></g>;
    })}
    {home.map((lane, player) => <g key={'lane' + player}>
      <polyline points={lane.map(p => `${p.x},${p.y}`).join(' ')} fill="none" stroke={palette[colors[player]]} strokeWidth={radius * 2} strokeOpacity=".2" />
      {lane.map((p, i) => <g key={i}>
        <rect x={p.x - radius} y={p.y - radius} width={radius * 2} height={radius * 2} rx="4" fill={palette[colors[player]]} stroke="white" strokeWidth="2" />
        <path d="M-5-7 5 0-5 7" fill="none" stroke="white" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" transform={`translate(${p.x} ${p.y}) rotate(${player * 90}) scale(${unit / 44})`} />
      </g>)}
    </g>)}
    {route.map((p, index) => {
      const color = palette[colors[index % 4]], next = route[(index + 1) % route.length];
      const angle = Math.atan2(next.y - p.y, next.x - p.x) * 180 / Math.PI;
      const start = index % (route.length / 4) === 0;
      const fly = colors.some((_, player) => index === (player * route.length / 4 + game.board.fly_from - 1) % route.length);
      return <g key={'cell' + index}>
        <rect x={p.x - radius} y={p.y - radius} width={radius * 2} height={radius * 2} rx="5" fill={color} stroke="white" strokeWidth="2" />
        {fly ? <path d="M0-9 3-3 9 0 3 3 0 9-3 3-9 0-3-3Z" fill="white" transform={`translate(${p.x} ${p.y}) scale(${unit / 44})`} /> : start ? <path d="M-6-7 5 0-6 7" fill="none" stroke="white" strokeWidth="3" strokeLinecap="round" transform={`translate(${p.x} ${p.y}) rotate(${angle}) scale(${unit / 44})`} /> : <circle cx={p.x} cy={p.y} r={unit * .18} fill="white" fillOpacity=".9" />}
      </g>;
    })}
    {pads.map((pad, player) => <g key={'pad' + player}>
      <line x1={pad.x} y1={pad.y} x2={route[player * route.length / 4].x} y2={route[player * route.length / 4].y} stroke={palette[colors[player]]} strokeWidth="2" strokeDasharray="4 4" />
      <circle cx={pad.x} cy={pad.y} r={unit * .38} fill="white" stroke={palette[colors[player]]} strokeWidth="2" />
      <path d={planePath} fill={palette[colors[player]]} transform={`translate(${pad.x} ${pad.y}) rotate(${player * 90 + 90}) scale(${unit * .012})`} />
    </g>)}
    {[0, 1, 2, 3].map(player => <path key={'goal' + player} d="M360 360 332 332 332 388Z" fill={palette[colors[player]]} transform={`rotate(${player * 90} 360 360)`} />)}
    <g className={'board-dice ' + (canRoll ? 'can-roll ' : '') + (rolling ? 'is-rolling' : '')} transform="translate(360 360)"
      role="button" tabIndex={canRoll ? 0 : -1} aria-label={canRoll ? '点击棋盘中央掷骰子' : `本次骰子 ${game.dice || '未掷'} 点`} aria-disabled={!canRoll}
      onClick={() => { if (canRoll) onRoll(); }} onKeyDown={e => { if (canRoll && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); onRoll(); } }}>
      <rect x="-32" y="-32" width="64" height="64" rx="18" fill="transparent" />
      <rect className="board-die-face" x="-26" y="-26" width="52" height="52" rx="13" fill="white" stroke={canRoll ? '#2e5e4c' : '#cbd8df'} strokeWidth="2" filter={`url(#${uid}shadow)`} />
      {dicePips[game.dice || 0].map(p => <circle key={p} cx={(p % 3 - 1) * 12} cy={(Math.floor(p / 3) - 1) * 12} r="4" fill="#324e62" />)}
    </g>
    {boardPieces(game, layout).map(({ player, playerIndex, piece, point, radius: r, stackSize }) => {
      const legal = !disabled && game.current === playerIndex && game.legal_moves.some(m => m.piece_id === piece.id);
      const finished = piece.progress === game.board.finish;
      const status = finished ? '已抵达' : piece.progress < 0 ? '在机库' : piece.progress === 0 ? '在起飞位' : `前进到第 ${piece.progress} 格`;
      return <g key={piece.id} className={'board-piece ' + (legal ? 'is-legal' : '')} data-piece-id={piece.id} data-progress={piece.progress}
        style={{ transform: `translate(${point.x}px, ${point.y}px)`, color: palette[player.color] }}
        role="button" tabIndex={legal ? 0 : -1} aria-label={`${player.name}的${piece.label}，${status}${piece.shield ? "，护盾保护中" : ""}${legal ? '，可以移动' : ''}`} aria-disabled={!legal}
        onClick={() => { if (legal) onPiece(piece, playerIndex); }} onKeyDown={e => { if (legal && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); onPiece(piece, playerIndex); } }}>
        <title>{player.name} · {piece.label} · {status}</title>
        {piece.shield && <circle r={r + 5} fill="#6cd5f2" fillOpacity=".2" stroke="#1689b2" strokeWidth="2.5" strokeDasharray="5 3" />}
        {legal && <circle className="piece-halo" r={r + 7} fill="currentColor" opacity=".24" />}
        <circle r={r + 4} fill="transparent" />
        <circle className="piece-disc" r={r} fill={finished || stackSize > 1 ? 'currentColor' : 'white'} stroke="currentColor" strokeWidth={legal ? 3 : 2} filter={`url(#${uid}shadow)`} />
        {stackSize > 1 ? <text y={r * .35} textAnchor="middle" fill="white" fontSize={r * 1.2} fontWeight="750">{piece.number}</text> : finished ? <path d="m-7 0 5 5 10-11" fill="none" stroke="white" strokeWidth="3" transform={`scale(${r / 18})`} /> : <path d={planePath} fill="currentColor" transform={`rotate(${playerIndex * 90 + 35}) scale(${r / 24})`} />}
        {stackSize === 1 && <><circle cx={r * .72} cy={-r * .72} r={Math.max(6, r * .4)} fill="currentColor" stroke="white" strokeWidth="1" /><text x={r * .72} y={-r * .72 + Math.max(2.5, r * .14)} textAnchor="middle" fill="white" fontSize={Math.max(8, r * .48)} fontWeight="750">{piece.number}</text></>}
      </g>;
    })}
  </svg>;
}
