import * as THREE from 'three';

const pips: Record<number, number[]> = {
  1: [4],
  2: [0, 8],
  3: [0, 4, 8],
  4: [0, 2, 6, 8],
  5: [0, 2, 4, 6, 8],
  6: [0, 2, 3, 5, 6, 8],
};

const faceNormal = [
  new THREE.Vector3(0, 1, 0),
  new THREE.Vector3(0, 1, 0),
  new THREE.Vector3(1, 0, 0),
  new THREE.Vector3(0, 0, 1),
  new THREE.Vector3(0, 0, -1),
  new THREE.Vector3(-1, 0, 0),
  new THREE.Vector3(0, -1, 0),
];

export function dieQuaternion(value: number) {
  const normal = faceNormal[value] || faceNormal[1];
  return new THREE.Quaternion().setFromUnitVectors(normal, new THREE.Vector3(0, 1, 0));
}

function pipTexture(face: number) {
  const canvas = document.createElement('canvas');
  canvas.width = 128;
  canvas.height = 128;
  const ctx = canvas.getContext('2d');
  if (!ctx) return new THREE.CanvasTexture(canvas);
  ctx.fillStyle = '#fffaf4';
  ctx.fillRect(0, 0, 128, 128);
  ctx.fillStyle = face === 1 ? '#d45b52' : '#24362f';
  for (const pip of pips[face] || []) {
    const x = 32 + (pip % 3) * 32;
    const y = 32 + Math.floor(pip / 3) * 32;
    ctx.beginPath();
    ctx.arc(x, y, 9, 0, Math.PI * 2);
    ctx.fill();
  }
  const texture = new THREE.CanvasTexture(canvas);
  texture.colorSpace = THREE.SRGBColorSpace;
  texture.anisotropy = 4;
  return texture;
}

export function createDie(size = 0.62) {
  const order = [2, 5, 1, 6, 3, 4];
  const materials = order.map(face => new THREE.MeshStandardMaterial({
    map: pipTexture(face),
    color: '#fffaf4',
    roughness: 0.38,
    metalness: 0.02,
  }));
  const mesh = new THREE.Mesh(new THREE.BoxGeometry(size, size, size), materials);
  mesh.castShadow = true;
  mesh.name = 'die';
  return mesh;
}

export function disposeDie(mesh: THREE.Mesh) {
  mesh.geometry.dispose();
  const materials = Array.isArray(mesh.material) ? mesh.material : [mesh.material];
  for (const material of materials) {
    const mapped = material as THREE.MeshStandardMaterial;
    mapped.map?.dispose();
    mapped.dispose();
  }
}
