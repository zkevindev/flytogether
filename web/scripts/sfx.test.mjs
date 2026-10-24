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


test('shield absorption never sounds like a crash, while bomb returns do', () => {
 assert.deepEqual(cuesFor([{kind:'prop',text:'爸爸使用炸弹，小熊号的护盾被炸弹抵消，飞机留在原地'}]),['shield']);
 assert.deepEqual(cuesFor([{kind:'prop',text:'爸爸使用炸弹，小熊号被送回机场'}]),['bump']);
 assert.deepEqual(cuesFor([{kind:'moved',text:'兔子号抵达终点'}]),['home']);
});

test('new props use authoritative effect type for absorption and separate sounds',()=>{
 assert.deepEqual(cuesFor([{kind:'prop',text:'起飞卡，小熊号到起飞位',prop_id:'launch',effects:[{kind:'launch'}]}]),['launch']);
 assert.deepEqual(cuesFor([{kind:'prop',text:'护盾解除',prop_id:'rescue',effects:[{kind:'rescue'}]}]),['rescue']);
 assert.deepEqual(cuesFor([{kind:'prop',text:'后退',prop_id:'boomerang',effects:[{kind:'boomerang'}]}]),['boomerang']);
 assert.deepEqual(cuesFor([{kind:'prop',text:'攻击',prop_id:'boomerang',effects:[{kind:'blocked'}]}]),['shield']);
 assert.deepEqual(cuesFor([{kind:'prop',text:'获得护盾',prop_id:'escort',effects:[{kind:'escort'}]}]),['escort']);
});
