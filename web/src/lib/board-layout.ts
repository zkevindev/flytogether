import type { BoardSpec, Game, Piece } from './types';

export type Point = { x: number; y: number };
export type Airport = Point & { size: number };
export type BoardLayout = { unit: number; route: Point[]; home: Point[][]; pads: Point[]; airports: Airport[] };

const rotate = (p: Point, turns: number): Point => {
  let { x, y } = p;
  for (let i = 0; i < turns; i++) [x, y] = [720 - y, x];
  return { x, y };
};
const polar = (index: number, count: number, radius: number): Point => {
  const angle = (-135 + index * 360 / count) * Math.PI / 180;
  return { x: 360 + Math.cos(angle) * radius, y: 360 + Math.sin(angle) * radius };
};

export function createBoardLayout(board: BoardSpec): BoardLayout {
  const quarter = board.ring_length / 4;
  if (board.layout === 'circle') return {
    unit: 36,
    route: Array.from({ length: board.ring_length }, (_, i) => polar(i, board.ring_length, 267)),
    home: Array.from({ length: 4 }, (_, player) => Array.from({ length: board.home_length }, (_, i) => polar(player * quarter - 1, board.ring_length, 267 - (i + 1) * 180 / board.home_length))),
    pads: Array.from({ length: 4 }, (_, i) => polar(i * quarter - .8, board.ring_length, 314)),
    airports: [{ x: 20, y: 20 }, { x: 566, y: 20 }, { x: 566, y: 566 }, { x: 20, y: 566 }].map(p => ({ ...p, size: 134 })),
  };
  // Thirteen (or twenty-one) cells per quarter around four corner airports.
  const c = (quarter + 3) / 2, k = c - 4, unit = 624 / (c * 2);
  const grid = (x: number, y: number) => ({ x: 48 + x * unit, y: 48 + y * unit });
  const segment: Point[] = [];
  for (let x = 0; x < k; x++) segment.push(grid(x, c - 2));
  segment.push(grid(k, c - 3));
  for (let y = c - 4; y >= 3; y--) segment.push(grid(k, y));
  segment.push(grid(k + 1, 2), grid(k + 2, 1), grid(k + 2, 0));
  for (let x = k + 3; x <= c + 1; x++) segment.push(grid(x, 0));
  const size = (k - .8) * unit + 22;
  return {
    unit,
    route: [0, 1, 2, 3].flatMap(player => segment.map(p => rotate(p, player))),
    home: Array.from({ length: 4 }, (_, player) => Array.from({ length: board.home_length }, (_, i) => rotate(grid(1 + i * (c - 1 - 48 / unit) / (board.home_length - 1), c), player))),
    pads: Array.from({ length: 4 }, (_, player) => rotate(grid(0, c - 3.5), player)),
    airports: [{ x: 26, y: 26 }, { x: 694 - size, y: 26 }, { x: 694 - size, y: 694 - size }, { x: 26, y: 694 - size }].map(p => ({ ...p, size })),
  };
}

export function hangarPosition(airport: Airport, number: number, count: number): Point {
  return { x: airport.x + airport.size * ((number - 1) % 2 ? .70 : .30), y: airport.y + airport.size * (count <= 2 ? .64 : number > 2 ? .78 : .43) };
}

export function piecePosition(layout: BoardLayout, board: BoardSpec, player: number, piece: Piece, count: number): Point {
  if (piece.progress < 0) return hangarPosition(layout.airports[player], piece.number, count);
  if (piece.progress === 0) return layout.pads[player];
  if (piece.progress <= board.ring_length) return layout.route[(player * board.ring_length / 4 + piece.progress - 1) % board.ring_length];
  return layout.home[player][Math.min(piece.progress - board.ring_length - 1, board.home_length - 1)];
}

export function boardPieces(game: Game, layout: BoardLayout) {
  const pieces = game.players.flatMap((player, playerIndex) => player.pieces.map(piece => ({
    player, playerIndex, piece, point: piecePosition(layout, game.board, playerIndex, piece, player.pieces.length),
    radius: piece.progress < 0 ? Math.min(24, layout.airports[playerIndex].size * .14) : layout.unit * .36, stackSize: 1,
  })));
  const groups = new Map<string, typeof pieces>();
  for (const p of pieces) {
    const key = `${p.point.x.toFixed(2)},${p.point.y.toFixed(2)}`;
    groups.set(key, [...(groups.get(key) || []), p]);
  }
  for (const group of groups.values()) {
    if (group.length === 1) continue;
    group.forEach((p, i) => {
      const angle = (i * 360 / group.length - 90) * Math.PI / 180;
      p.point = { x: p.point.x + Math.cos(angle) * layout.unit * .27, y: p.point.y + Math.sin(angle) * layout.unit * .27 };
      p.radius = layout.unit * (group.length > 2 ? .18 : .23);
      p.stackSize = group.length;
    });
  }
  return pieces;
}
