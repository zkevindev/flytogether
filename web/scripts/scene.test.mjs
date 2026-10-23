import { test } from 'node:test';
import assert from 'node:assert/strict';
import * as THREE from 'three';
import { fitBoardCamera, disposeObjects } from '../src/lib/scene.ts';

for (const aspect of [0.5, 1, 1.5, 2]) test(`entire board and raised planes fit camera at aspect ${aspect}`, () => {
  const camera = new THREE.PerspectiveCamera(38, aspect, .1, 80);
  fitBoardCamera(camera, aspect);
  for (const x of [-4.3, 4.3]) for (const y of [-.2, .95]) for (const z of [-4.3, 4.3]) {
    const point = new THREE.Vector3(x, y, z).project(camera);
    assert.ok(Math.abs(point.x) < .95 && Math.abs(point.y) < .95, `clipped corner ${x},${y},${z}`);
    assert.ok(point.z > -1 && point.z < 1);
  }
});

test('disposal releases shared geometry, materials, and sprite textures exactly once', () => {
  const root = new THREE.Group();
  const geometry = new THREE.BoxGeometry();
  const texture = new THREE.Texture();
  const material = new THREE.MeshStandardMaterial({ map: texture });
  const spriteMaterial = new THREE.SpriteMaterial({ map: texture });
  const counts = new Map();
  for (const resource of [geometry, texture, material, spriteMaterial]) resource.addEventListener('dispose', () => counts.set(resource, (counts.get(resource) || 0) + 1));
  root.add(new THREE.Mesh(geometry, material), new THREE.Mesh(geometry, [material, material]), new THREE.Sprite(spriteMaterial));
  disposeObjects(root);
  for (const resource of [geometry, texture, material, spriteMaterial]) assert.equal(counts.get(resource), 1);
});
