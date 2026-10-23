import { test } from 'node:test';
import assert from 'node:assert/strict';
import * as THREE from 'three';
import { dieQuaternion } from '../src/lib/die.ts';

const normals = {
  1: new THREE.Vector3(0, 1, 0),
  2: new THREE.Vector3(1, 0, 0),
  3: new THREE.Vector3(0, 0, 1),
  4: new THREE.Vector3(0, 0, -1),
  5: new THREE.Vector3(-1, 0, 0),
  6: new THREE.Vector3(0, -1, 0),
};

test('each die value rotates that face to the top', () => {
  for (const value of [1, 2, 3, 4, 5, 6]) {
    const up = normals[value].clone().applyQuaternion(dieQuaternion(value));
    assert.ok(up.distanceTo(new THREE.Vector3(0, 1, 0)) < 1e-6, `face ${value} is not up`);
  }
});
