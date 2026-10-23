import * as THREE from 'three';

// Fit the board, airports and raised pieces at every viewport aspect ratio.
export function fitBoardCamera(camera: THREE.PerspectiveCamera, aspect: number) {
  camera.aspect = aspect;
  const direction = new THREE.Vector3(0, 1, 0.58).normalize();
  const up = new THREE.Vector3(0, direction.z, -direction.y);
  const vertical = Math.tan(THREE.MathUtils.degToRad(camera.fov / 2));
  let distance = 0;
  for (const x of [-4.3, 4.3]) for (const y of [-0.2, 0.95]) for (const z of [-4.3, 4.3]) {
    const corner = new THREE.Vector3(x, y, z);
    distance = Math.max(distance, corner.dot(direction) + Math.max(Math.abs(x) / (vertical * aspect), Math.abs(corner.dot(up)) / vertical));
  }
  camera.position.copy(direction.multiplyScalar(distance * 1.06));
  camera.lookAt(0, 0, 0);
  camera.updateProjectionMatrix();
  camera.updateMatrixWorld();
}

// Shared geometry/materials are disposed once; sprite maps need explicit disposal.
export function disposeObjects(root: THREE.Object3D) {
  const geometries = new Set<THREE.BufferGeometry>();
  const materials = new Set<THREE.Material>();
  const textures = new Set<THREE.Texture>();
  root.traverse(object => {
    const mesh = object as THREE.Mesh;
    if (mesh.geometry) geometries.add(mesh.geometry);
    for (const material of Array.isArray(mesh.material) ? mesh.material : mesh.material ? [mesh.material] : []) {
      materials.add(material);
      for (const value of Object.values(material)) if (value instanceof THREE.Texture) textures.add(value);
    }
  });
  textures.forEach(texture => texture.dispose());
  materials.forEach(material => material.dispose());
  geometries.forEach(geometry => geometry.dispose());
}
