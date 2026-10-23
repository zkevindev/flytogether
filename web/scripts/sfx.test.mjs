import { test } from 'node:test';
import assert from 'node:assert/strict';
import { cuesFor } from '../src/lib/sfx.ts';

test('dice, flight and hits get different cues, and a win replaces the pile', () => {
  assert.deepEqual(cuesFor([{ kind: 'rolled', text: '小朋友掷出了 4 点' }]), ['roll']);
  assert.deepEqual(cuesFor([{ kind: 'rolled', text: '爸爸的幸运骰掷出 2 和 6 点，取 6 点' }]), ['luck']);
  assert.deepEqual(cuesFor([{ kind: 'moved', text: '小朋友的小熊号：从机场起飞，进入起飞位' }]), ['launch']);
  assert.deepEqual(cuesFor([{ kind: 'moved', text: '小朋友的小熊号：前进 3 格，同色跳跃，穿越飞行航线' }]), ['glide']);
  assert.deepEqual(cuesFor([{ kind: 'moved', text: '小朋友的小熊号：前进 3 格，送 1 架飞机回机场' }]), ['bump']);
  assert.deepEqual(cuesFor([
    { kind: 'moved', text: '小朋友的小熊号：前进 2 格，抵达终点' },
    { kind: 'won', text: '小朋友率先抵达，赢下了这次飞行！' },
  ]), ['win']);
  assert.deepEqual(cuesFor([{ kind: 'prop', text: '爸爸使用了护盾：小熊号获得护盾。' }]), ['shield']);
  assert.deepEqual(cuesFor([{ kind: 'prop', text: '爸爸使用了起飞卡，下一次掷骰生效。' }]), ['card']);
});
