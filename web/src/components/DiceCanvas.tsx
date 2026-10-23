import { useEffect, useRef } from 'react';
import * as THREE from 'three';
import { createDie, dieQuaternion, disposeDie } from '../lib/die';

export function DiceCanvas({ values }: { values: number[] }) {
  const host = useRef<HTMLDivElement>(null);
  const faces = values.join(',');
  useEffect(() => {
    const el = host.current;
    if (!el) return;
    const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;
    const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true });
    renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
    renderer.outputColorSpace = THREE.SRGBColorSpace;
    el.appendChild(renderer.domElement);
    const scene = new THREE.Scene();
    const camera = new THREE.PerspectiveCamera(32, 1, 0.1, 20);
    camera.position.set(0, 1.55, 3.1);
    camera.lookAt(0, 0.2, 0);
    scene.add(new THREE.HemisphereLight('#ffffff', '#b7cfc4', 1.2));
    const sun = new THREE.DirectionalLight('#fff', 1.6);
    sun.position.set(2, 4, 3);
    scene.add(sun);
    const dice = faces.split(',').map((raw, index) => {
      const mesh = createDie(0.86);
      const count = faces.split(',').length;
      mesh.position.x = count === 1 ? 0 : index === 0 ? -0.72 : 0.72;
      const spin = new THREE.Quaternion().setFromEuler(new THREE.Euler(Math.PI * (3 + index), Math.PI * (2.4 + index), 0.4));
      mesh.quaternion.copy(spin);
      mesh.userData.spin = spin;
      scene.add(mesh);
      return { mesh, to: dieQuaternion(Number(raw) || 1) };
    });
    const started = performance.now();
    const duration = reduced ? 0 : 950;
    let frame = 0;
    const resize = () => {
      const width = el.clientWidth || 360;
      const height = el.clientHeight || 220;
      renderer.setSize(width, height, false);
      camera.aspect = width / Math.max(height, 1);
      camera.updateProjectionMatrix();
    };
    const observer = new ResizeObserver(resize);
    observer.observe(el);
    resize();
    const tick = () => {
      frame = requestAnimationFrame(tick);
      const t = duration === 0 ? 1 : Math.min(1, (performance.now() - started) / duration);
      const eased = 1 - Math.pow(1 - t, 3);
      for (const die of dice) {
        die.mesh.quaternion.copy(die.mesh.userData.spin as THREE.Quaternion).slerp(die.to, eased);
        die.mesh.position.y = Math.sin(t * Math.PI) * 0.35;
      }
      renderer.render(scene, camera);
    };
    tick();
    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
      for (const die of dice) disposeDie(die.mesh);
      renderer.dispose();
      if (renderer.domElement.parentElement === el) el.removeChild(renderer.domElement);
    };
  }, [faces]);
  return <div ref={host} className="dice-canvas" />;
}
