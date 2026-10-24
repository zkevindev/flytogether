import test from 'node:test';
import assert from 'node:assert/strict';
import { loadVoiceSeatModes, saveVoiceSeatMode } from '../src/lib/voice-readonly.ts';

function storage(initial = {}) {
  const values = new Map(Object.entries(initial));
  return { getItem: key => values.get(key) ?? null, setItem: (key, value) => values.set(key, value) };
}

test('voice read-only preferences persist independently by seat', () => {
  const store = storage();
  saveVoiceSeatMode(0, true, store);
  saveVoiceSeatMode(1, false, store);
  assert.deepEqual(loadVoiceSeatModes(store), { 0: true, 1: false });
  saveVoiceSeatMode(1, true, store);
  assert.deepEqual(loadVoiceSeatModes(store), { 0: true, 1: true });
});

test('damaged preference storage safely defaults to writable mode', () => {
  assert.deepEqual(loadVoiceSeatModes(storage({ 'fly-voice-readonly-seats': '{broken' })), {});
  assert.deepEqual(loadVoiceSeatModes(storage({ 'fly-voice-readonly-seats': 'null' })), {});
});
