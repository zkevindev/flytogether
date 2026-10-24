import test from 'node:test';
import assert from 'node:assert/strict';
import { playerMarker, playerMarkers } from '../src/lib/player-markers.ts';

test('four seats have stable non-color markers with unique shapes', () => {
  assert.equal(playerMarkers.length, 4);
  assert.equal(new Set(playerMarkers.map(marker => marker.symbol)).size, 4);
  assert.deepEqual([0, 1, 2, 3].map(index => playerMarker(index).id), ['circle', 'triangle', 'diamond', 'square']);
  assert.equal(playerMarker(4).id, playerMarker(0).id);
  assert.equal(playerMarker(-1).id, playerMarker(3).id);
});
