import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createBoardLayout, hangarPosition, boardPieces } from '../src/lib/board-layout.ts';

const boards = [
  { id: 'cross', layout: 'cross', ring_length: 52, home_length: 6, finish: 58 },
  { id: 'long', layout: 'cross', ring_length: 84, home_length: 8, finish: 92 },
  { id: 'circle', layout: 'circle', ring_length: 52, home_length: 6, finish: 58 },
];
for (const board of boards) {
  test(`${board.id}: continuous unique route, visible home lanes and non-overlapping hangars`, () => {
    const l = createBoardLayout(board);
    assert.equal(l.route.length, board.ring_length);
    assert.equal(new Set(l.route.map(p => `${p.x.toFixed(2)},${p.y.toFixed(2)}`)).size, board.ring_length);
    for (let i = 0; i < l.route.length; i++) {
      const a = l.route[i], b = l.route[(i + 1) % l.route.length];
      assert.ok(Math.hypot(a.x - b.x, a.y - b.y) <= l.unit * 1.5, 'disconnected route');
    }
    for (let player = 0; player < 4; player++) {
      assert.equal(l.home[player].length, board.home_length);
      const airport = l.airports[player];
      const tileRadius = board.layout === 'circle' ? 15 : l.unit * .47;
      for (const p of l.route) {
        const overlaps = p.x + tileRadius > airport.x && p.x - tileRadius < airport.x + airport.size && p.y + tileRadius > airport.y && p.y - tileRadius < airport.y + airport.size;
        assert.equal(overlaps, false, 'airport overlaps public route');
      }
      const slots = Array.from({ length: 4 }, (_, i) => hangarPosition(airport, i + 1, 4));
      for (const p of [...l.route, ...l.home[player], ...slots, l.pads[player]]) assert.ok(p.x > 0 && p.x < 720 && p.y > 0 && p.y < 720);
      for (let i = 0; i < slots.length; i++) for (let j = 0; j < i; j++) assert.ok(Math.hypot(slots[i].x - slots[j].x, slots[i].y - slots[j].y) > Math.min(24, airport.size * .14) * 2);
      const last = l.home[player].at(-1), first = l.home[player][0];
      assert.ok(Math.hypot(last.x - 360, last.y - 360) < Math.hypot(first.x - 360, first.y - 360));
    }
  });
  test(`${board.id}: friendly stacks keep distinct clickable markers`, () => {
    const game = { board, players: [{ color: 'coral', pieces: Array.from({ length: 4 }, (_, i) => ({ id: `p${i}`, number: i + 1, progress: 10 })) }] };
    const p = boardPieces(game, createBoardLayout(board));
    assert.equal(p.length, 4);
    for (let i = 0; i < p.length; i++) for (let j = 0; j < i; j++) assert.ok(Math.hypot(p[i].point.x - p[j].point.x, p[i].point.y - p[j].point.y) > p[i].radius + p[j].radius);
  });
}
