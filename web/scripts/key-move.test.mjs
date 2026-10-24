import test from 'node:test';
import assert from 'node:assert/strict';
import { latestKeyMove } from '../src/lib/key-move.ts';

const event = (id, kind, text) => ({ id, kind, text, player_id: 'player-0', piece_id: 'internal-piece-id' });
const game = history => ({ history });

test('launch, capture, arrival, shield and prop events expose their authoritative human text', () => {
  for (const [kind, text] of [
    ['moved', '小朋友的小熊号：从机场起飞，进入起飞位'],
    ['moved', '爸爸的兔子号：前进 3 格，送 1 架飞机回机场'],
    ['moved', '小朋友的小熊号：前进 5 格，抵达终点'],
    ['moved', '爸爸的兔子号：前进 4 格，击破 1 个护盾'],
    ['prop', '小朋友使用了护盾：小熊号获得护盾，抵挡一次攻击或撞机。请继续掷骰子。'],
  ]) {
    assert.equal(latestKeyMove(game([event('key', kind, text)]))?.text, text);
  }
});

test('uses the latest significant event, skips routine steps, and does not invent empty narration', () => {
  const earlier = event('earlier', 'prop', '爸爸使用了加速卡，下一次掷骰生效。');
  const routine = event('routine', 'moved', '小朋友的小熊号：前进 2 格');
  const current = event('current', 'moved', '爸爸的兔子号：前进 4 格，抵达终点');
  const history = [earlier, routine, current];
  assert.equal(latestKeyMove(game(history)), current);
  assert.deepEqual(history, [earlier, routine, current]);
  assert.equal(latestKeyMove(game([routine])), undefined);
  assert.equal(latestKeyMove(game([])), undefined);
});

test('returns no narration for a new match without key events', () => {
  assert.equal(latestKeyMove(game([event('dice', 'rolled', '掷出了 6 点。')])), undefined);
});
