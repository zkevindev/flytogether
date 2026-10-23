import { useId } from 'react';
import type { BoardSpec, Game, Piece } from '../lib/types';
import { palette } from '../lib/types';

const center = 360;
const colors = ['coral', 'gold', 'blue', 'jade'];
const planePath = 'M-3-18 3-18 5-4 16 4 16 9 5 5 4 13 9 17 9 20 0 17-9 20-9 17-4 13-5 5-16 9-16 4-5-4Z';
const polar = (cell: number, radius: number) => {
  const theta = (-135 + cell * 360 / 52) * Math.PI / 180;
  return { x: center + Math.cos(theta) * radius, y: center + Math.sin(theta) * radius };
};

function rotate(point: [number, number], side: number, turns: number): [number, number] {
  let [x, y] = point;
  for (let i = 0; i < turns; i++) [x, y] = [side - 1 - y, x];
  return [x, y];
}

function crossRoute(board: BoardSpec): [number, number][] {
  const side = board.ring_length === 84 ? 23 : 15;
  const c = (side - 1) / 2;
  const quarter: [number, number][] = [];
  for (let x = 1; x <= c - 2; x++) quarter.push([x, c - 1]);
  for (let y = c - 2; y >= 1; y--) quarter.push([c - 1, y]);
  quarter.push([c - 1, 0], [c, 0], [c + 1, 0]);
  return [0, 1, 2, 3].flatMap(turn => quarter.map(point => rotate(point, side, turn)));
}

function crossXY(board: BoardSpec, cell: number) {
  const side = board.ring_length === 84 ? 23 : 15;
  const unit = 600 / (side - 1);
  const [x, y] = crossRoute(board)[cell];
  return { x: 60 + x * unit, y: 60 + y * unit, unit };
}

function circlePosition(board: BoardSpec, player: number, piece: Piece) {
  const quarter = board.ring_length / 4;
  const p = piece.progress;
  const pod = [{ x: 88, y: 88 }, { x: 632, y: 88 }, { x: 632, y: 632 }, { x: 88, y: 632 }][player];
  if (p === -1) return { x: pod.x + ((piece.number - 1) % 2 ? 22 : -22), y: pod.y + (piece.number > 2 ? 22 : -17), unit: 42 };
  if (p === 0) return { ...polar(player * quarter, 306), unit: 42 };
  if (p <= board.ring_length) return { ...polar((player * quarter + p - 1) % board.ring_length, 265), unit: 42 };
  return { ...polar((player + 1) * quarter - 1, 265 - (p - board.ring_length) * (175 / board.home_length)), unit: 42 };
}

function crossPosition(board: BoardSpec, player: number, piece: Piece) {
  const side = board.ring_length === 84 ? 23 : 15;
  const c = (side - 1) / 2;
  const unit = 600 / (side - 1);
  if (piece.progress >= 1 && piece.progress <= board.ring_length) {
    const index = (player * board.ring_length / 4 + piece.progress - 1) % board.ring_length;
    return crossXY(board, index);
  }
  if (piece.progress > board.ring_length) {
    const laneStep = piece.progress - board.ring_length - 1;
    const laneStart = c - board.home_length;
    const [x, y] = rotate([laneStart + laneStep, c], side, player);
    return { x: 60 + x * unit, y: 60 + y * unit, unit };
  }
  const extent = (c - 2) / 2;
  const bases: [number, number][] = [[extent, extent], [side - 1 - extent, extent], [side - 1 - extent, side - 1 - extent], [extent, side - 1 - extent]];
  const base = bases[player];
  if (piece.progress === 0) {
    const start = crossXY(board, player * board.ring_length / 4);
    return { x: start.x - unit * .6, y: start.y + unit * .6, unit };
  }
  const dx = ((piece.number - 1) % 2 ? .34 : -.34) * unit;
  const dy = (piece.number > 2 ? .34 : -.34) * unit;
  return { x: 60 + base[0] * unit + dx, y: 60 + base[1] * unit + dy, unit };
}

function CrossBoard({ board, game }: { board: BoardSpec; game: Game }) {
  const side = board.ring_length === 84 ? 23 : 15;
  const c = (side - 1) / 2;
  const unit = 600 / (side - 1);
  const route = crossRoute(board);
  const cellSize = unit * .78;
  const baseExtent = c - 2;
  const base = (player: number) => {
    const points: [number, number][] = [[0, 0], [side - 1, 0], [side - 1, side - 1], [0, side - 1]];
    return rotate(points[player], side, 0);
  };
  const basePolygons = [
    `60,60 ${60 + baseExtent * unit},${60 + baseExtent * unit} ${60 + baseExtent * unit},${60 + unit},60,${60 + unit}`,
    `${660},60 ${60 + (side - 1 - baseExtent) * unit},${60 + unit} ${60 + (side - 1 - baseExtent) * unit},${60 + baseExtent * unit} ${660},${60 + baseExtent * unit}`,
    `660,660 ${60 + (side - 1 - baseExtent) * unit},${60 + (side - 1 - baseExtent) * unit} ${60 + (side - 1 - baseExtent) * unit},${660} 660,${660}`,
    `60,660 ${60 + baseExtent * unit},${660} ${60 + baseExtent * unit},${60 + (side - 1 - baseExtent) * unit} 60,${60 + (side - 1 - baseExtent) * unit}`,
  ];
  const homeCells = colors.flatMap((_, player) => Array.from({ length: board.home_length }, (_, i) => {
    const [x, y] = rotate([c - board.home_length + i, c], side, player);
    return { player, x: 60 + x * unit, y: 60 + y * unit };
  }));
  const pieces = game.players.flatMap((player, pi) => player.pieces.map(piece => {
    const p = crossPosition(board, pi, piece);
    const legal = game.current === pi && game.legal_moves.some(m => m.piece_id === piece.id);
    const current = game.current === pi;
    return <g key={piece.id} className={'board-piece ' + (legal ? 'is-legal' : '')}
      style={{ transform: `translate(${p.x}px, ${p.y}px)`, color: palette[player.color] }}
      role="img" aria-label={`${player.name}的${piece.label}${legal ? '，可以移动' : ''}`}>
      {legal && <circle className="piece-halo" r={unit * .47} fill="currentColor" opacity=".18" />}
      <circle r={unit * .35} fill="white" stroke="currentColor" strokeWidth={legal ? 3 : 2} />
      <path d={planePath} fill="currentColor" transform={`rotate(35) scale(${unit * .014})`} />
      {current && <text y={unit * .62} textAnchor="middle" fill="currentColor" fontSize={Math.max(9, unit * .23)} fontWeight="700">{piece.number}</text>}
      {piece.progress === board.finish && <path d={`M${-unit * .14} 0l${unit * .1} ${unit * .1} ${unit * .2} ${-unit * .23}`} fill="none" stroke="white" strokeWidth="3" />}
    </g>;
  }));
  return <g>
    {basePolygons.map((points, i) => <polygon key={'base'+i} points={points} fill={palette[colors[i]]} opacity=".10" />)}
    <rect x={60 + unit} y={60 + unit} width={unit * (side - 3)} height={unit * (side - 3)} rx={unit * .5} fill="none" stroke="#e4eae1" strokeWidth="2" />
    {route.map(([gx, gy], index) => {
      const x = 60 + gx * unit, y = 60 + gy * unit;
      const player = Math.floor(index / (board.ring_length / 4));
      const isStart = index % (board.ring_length / 4) === 0;
      const isFly = colors.some((_, pi) => index === (pi * board.ring_length / 4 + board.fly_from - 1) % board.ring_length);
      return <g key={'route'+index}>
        <rect x={x - cellSize/2} y={y - cellSize/2} width={cellSize} height={cellSize} rx={unit * .15}
          fill={isStart ? palette[colors[player]] : '#fffefb'} stroke={isStart ? palette[colors[player]] : '#dce4da'} strokeWidth="1.5" />
        {isFly ? <path d={`M${x} ${y-unit*.2}l${unit*.07} ${unit*.14} ${unit*.15} ${unit*.06} -${unit*.15} ${unit*.06} -${unit*.07} ${unit*.14} -${unit*.07} -${unit*.14} -${unit*.15} -${unit*.06} ${unit*.15} -${unit*.06}Z`} fill="#c49737" /> : <circle cx={x} cy={y} r={unit*.055} fill={isStart ? 'white' : '#dce4da'} />}
      </g>;
    })}
    {homeCells.map(({ player, x, y }, i) => <rect key={'home'+i} x={x - cellSize/2} y={y - cellSize/2} width={cellSize} height={cellSize} rx={unit*.14} fill={palette[colors[player]]} opacity={i % board.home_length === board.home_length - 1 ? '.58' : '.25'} stroke="white" strokeWidth="1.2" />)}
    {colors.map((color, i) => {
      const a = crossXY(board, i * board.ring_length / 4);
      const [bx, by] = base(i);
      const px = 60 + bx * unit, py = 60 + by * unit;
      return <g key={'seat'+i}>
        <circle cx={px} cy={py} r={unit*1.14} fill={palette[color]} opacity=".12" stroke={palette[color]} strokeWidth="2" />
        <text x={px} y={py + unit*1.65} textAnchor="middle" fontSize={Math.max(10, unit*.35)} fontWeight="700" fill={palette[color]}>{game.players[i]?.name || '空位'}</text>
        <circle cx={a.x} cy={a.y} r={cellSize*.36} fill={palette[color]} stroke="white" strokeWidth="2" />
      </g>;
    })}
    <path d={`M${center} ${center} L${center-unit*1.05} ${center-unit*1.05} L${center-unit*1.05} ${center+unit*1.05} Z`} fill={palette.coral} opacity=".9" />
    <path d={`M${center} ${center} L${center-unit*1.05} ${center-unit*1.05} L${center+unit*1.05} ${center-unit*1.05} Z`} fill={palette.gold} opacity=".9" />
    <path d={`M${center} ${center} L${center+unit*1.05} ${center-unit*1.05} L${center+unit*1.05} ${center+unit*1.05} Z`} fill={palette.blue} opacity=".9" />
    <path d={`M${center} ${center} L${center+unit*1.05} ${center+unit*1.05} L${center-unit*1.05} ${center+unit*1.05} Z`} fill={palette.jade} opacity=".9" />
    <circle cx={center} cy={center} r={unit*.37} fill="#fffef8" />
    <path d={planePath} fill="#417967" transform={`translate(${center} ${center-unit*.12}) rotate(35) scale(${unit*.015})`} />
    {pieces}
  </g>;
}

function CircleBoard({ board, game }: { board: BoardSpec; game: Game }) {
  const quarter = board.ring_length / 4;
  const pods = [{ x: 88, y: 88 }, { x: 632, y: 88 }, { x: 632, y: 632 }, { x: 88, y: 632 }];
  return <g>
    <circle cx="360" cy="360" r="304" fill="#f8faf5" />
    <circle cx="360" cy="360" r="265" fill="none" stroke="#e7ede5" strokeWidth="47" />
    <circle cx="360" cy="360" r="224" fill="none" stroke="#e3e9df" strokeDasharray="3 9" />
    {colors.map((color, player) => {
      const start = polar((player + 1) * quarter - 1, 230), end = polar((player + 1) * quarter - 1, 55);
      return <g key={color}>
        <line x1={start.x} y1={start.y} x2={end.x} y2={end.y} stroke={palette[color]} strokeWidth="2" strokeDasharray="5 7" opacity=".32" />
        {Array.from({ length: board.home_length }, (_, i) => {
          const p = polar((player + 1) * quarter - 1, 230 - i * 175 / board.home_length);
          return <rect key={i} x={p.x - 13} y={p.y - 13} width="26" height="26" rx="8" fill={palette[color]} opacity={i === board.home_length - 1 ? '.55' : '.16'} transform={`rotate(${(-135 + ((player + 1) * quarter - 1) * 360 / board.ring_length + 90)} ${p.x} ${p.y})`} />;
        })}
      </g>;
    })}
    {Array.from({ length: board.ring_length }, (_, i) => {
      const p = polar(i, 265), color = palette[colors[Math.floor(i / quarter)]];
      const fly = colors.some((_, player) => i === (player * quarter + board.fly_from - 1) % board.ring_length);
      return <g key={i} transform={`translate(${p.x} ${p.y}) rotate(${-135 + i * 360 / board.ring_length + 90})`}>
        <rect x="-13.5" y="-15" width="27" height="30" rx="8" fill={color} opacity=".78" />
        {fly ? <path d="M0-8 3-2 8 0 3 2 0 8-3 2-8 0-3-2Z" fill="white" /> : <circle r="3" fill="white" opacity=".86" />}
      </g>;
    })}
    {colors.map((color, i) => {
      const pod = pods[i], player = game.players[i], pad = polar(i * quarter, 306);
      return <g key={color}>
        <rect x={pod.x - 59} y={pod.y - 57} width="118" height="112" rx="30" fill={palette[color]} opacity={player ? '.11' : '.045'} />
        <rect x={pod.x - 59} y={pod.y - 57} width="118" height="112" rx="30" fill="none" stroke={palette[color]} strokeOpacity={game.current === i ? '.6' : '.14'} strokeWidth={game.current === i ? '2' : '1'} />
        <text x={pod.x} y={pod.y + 77} textAnchor="middle" fill={player ? palette[color] : '#b5beb4'} fontSize="15" fontWeight="650">{player?.name || '预留席位'}</text>
        <circle cx={pad.x} cy={pad.y} r="13" fill="white" stroke={palette[color]} strokeWidth="1.5" strokeDasharray="3 3" />
        <path d="M-4-4 8 0-4 4Z" fill={palette[color]} transform={`translate(${pad.x} ${pad.y}) rotate(${-135 + i * 90 + 180})`} />
      </g>;
    })}
    <g className="board-center">
      <path d="M330 310q8-16 22-8 13-24 31-5 19-4 22 13Z" fill="#e4ece1" />
      <path d={planePath} fill="#417967" transform="translate(360 350) rotate(35) scale(.9)" />
      <text x="360" y="399" textAnchor="middle" fontSize="29" fontWeight="750" fill="#35574a" letterSpacing="5">一起飞</text>
      <text x="360" y="424" textAnchor="middle" fontSize="10" fontWeight="600" fill="#94a08e" letterSpacing="3">A LITTLE CLOSER, TOGETHER</text>
    </g>
  </g>;
}

export function Board({ game, onPiece, disabled }: { game: Game; onPiece: (piece: Piece, player: number) => void; disabled: boolean }) {
  const uid = useId().replaceAll(':', '');
  const board = game.board;
  const pieces = game.players.flatMap((player, pi) => player.pieces.map(piece => {
    const p = board.layout === 'circle' ? circlePosition(board, pi, piece) : crossPosition(board, pi, piece);
    const legal = !disabled && game.current === pi && game.legal_moves.some(m => m.piece_id === piece.id);
    const scale = board.layout === 'circle' ? 1 : p.unit * .014;
    return <g key={piece.id} className={'board-piece ' + (legal ? 'is-legal' : '')}
      style={{ transform: `translate(${p.x}px, ${p.y}px)`, color: palette[player.color] }} role="button" tabIndex={0}
      aria-label={`${player.name}的${piece.label}${legal ? '，可以移动' : ''}`} onClick={() => onPiece(piece, pi)}
      onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onPiece(piece, pi); } }}>
      <title>{player.name} · {piece.label}</title>
      {legal && <circle className="piece-halo" r={board.layout === 'circle' ? 25 : p.unit * .47} fill="currentColor" opacity=".14" />}
      <circle r={board.layout === 'circle' ? 18 : p.unit * .35} fill="white" stroke="currentColor" strokeWidth={legal ? '3' : '2'} filter={`url(#${uid}shadow)`} />
      <path d={planePath} fill="currentColor" transform={`rotate(35) scale(${board.layout === 'circle' ? '.55' : scale})`} />
      <circle cx={board.layout === 'circle' ? 15 : p.unit * .29} cy={board.layout === 'circle' ? -15 : -p.unit * .29} r={board.layout === 'circle' ? 8 : p.unit * .16} fill="currentColor" />
      <text x={board.layout === 'circle' ? 15 : p.unit * .29} y={board.layout === 'circle' ? -12 : -p.unit * .24} textAnchor="middle" fill="white" fontSize={board.layout === 'circle' ? 9 : p.unit * .18} fontWeight="700">{piece.number}</text>
      {piece.progress === board.finish && <path d="m-7 0 5 5 10-11" fill="none" stroke="white" strokeWidth="3" />}
    </g>;
  }));
  return <svg className="board-svg" viewBox="0 0 720 720" role="img" aria-label={`${board.name}飞行棋棋盘`}>
    <defs>
      <filter id={uid + 'shadow'} x="-70%" y="-70%" width="240%" height="240%"><feDropShadow dx="0" dy="4" stdDeviation="4" floodColor="#203c31" floodOpacity=".12" /></filter>
      <symbol id={uid + 'plane'} viewBox="-24 -24 48 48"><path d={planePath} /></symbol>
    </defs>
    {board.layout === 'circle' ? <CircleBoard board={board} game={game} /> : <CrossBoard board={board} game={game} />}
    {pieces}
  </svg>;
}
