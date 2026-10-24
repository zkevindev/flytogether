import test from 'node:test';
import assert from 'node:assert/strict';
import { replayCardData, replayCardSVG } from '../src/lib/replay-card.ts';

function game(overrides = {}) {
  return {
    id: 'private-game-id', demo: false, winner: 'private-player-1', mode: 'quick',
    board: { name: '家庭十字棋盘' },
    players: [
      { id: 'private-player-1', name: '小朋友', color: 'coral', ai: false, stats: { rolls: 9, takeoffs: 2, arrivals: 1, captures: 1, props_used: 3 } },
      { id: 'private-player-2', name: '爸爸', color: 'blue', ai: false, stats: { rolls: 8, takeoffs: 1, arrivals: 0, captures: 0, props_used: 2 } },
    ],
    history: [
      { id: 'private-event-1', kind: 'started', text: '欢迎登机' },
      { id: 'private-event-2', kind: 'rolled', text: '掷出了六点' },
      { id: 'private-event-3', kind: 'moved', text: '小朋友的小熊号：从机场起飞' },
      { id: 'private-event-4', kind: 'prop', text: '爸爸使用了炸弹：对手飞机回机场' },
      { id: 'private-event-5', kind: 'moved', text: '小朋友的小熊号：抵达终点' },
      { id: 'private-event-6', kind: 'user_text', text: 'API_KEY=private-secret' },
    ],
    ...overrides,
  };
}

test('real winner stats and only recorded highlights go into share text and image', () => {
  const value = game();
  const data = replayCardData(value);
  assert.equal(data.winner, '小朋友');
  assert.equal(data.board, '家庭十字棋盘');
  assert.equal(data.mode, '轻快一局');
  assert.equal(data.rows[0].rolls, 9);
  assert.equal(data.rows[0].props, 3);
  assert.deepEqual(data.highlights, [
    '小朋友的小熊号：从机场起飞',
    '爸爸使用了炸弹：对手飞机回机场',
    '小朋友的小熊号：抵达终点',
  ]);
  const svg = replayCardSVG(value);
  assert.match(svg, /小朋友/);
  assert.doesNotMatch(svg, /private-game-id|private-player|private-event|private-secret|API_KEY/);
});

test('completed mode is named accurately and markup characters are escaped', () => {
  const value = game({ mode: 'classic', players: [{ ...game().players[0], name: '<家人&孩子>' }] });
  const data = replayCardData(value);
  assert.equal(data.mode, '完整飞行');
  assert.equal(data.rows[0].name, '<家人&孩子>');
  assert.match(replayCardSVG(value), /&lt;家人&amp;孩子&gt;/);
});

test('cooperative win credits the whole team and explains the rescue objective', () => {
  const value = game({ mode: 'coop', winner: 'team' });
  const data = replayCardData(value);
  assert.equal(data.winner, '全队');
  assert.equal(data.mode, '星河救援 · 合作挑战');
  assert.match(data.text, /全队共同完成了星河救援/);
  assert.match(replayCardSVG(value), /共同完成救援/);
  assert.match(replayCardSVG(value), /救援队员/);
});

test('no winner and demo matches do not produce share cards', () => {
  assert.equal(replayCardData(game({ winner: undefined })), undefined);
  assert.equal(replayCardSVG(game({ demo: true })), '');
});
