import { useEffect, useRef, useState } from 'react';
import * as THREE from 'three';
import type { Game, Move, Piece } from '../lib/types';
import { colors, palette } from '../lib/types';
import { boardPieces, createBoardLayout, piecePosition, type BoardLayout, type Point } from '../lib/board-layout';
import type { BoardSpec } from '../lib/types';
import { createDie, dieQuaternion } from '../lib/die';
import { disposeObjects, fitBoardCamera } from '../lib/scene';
import { FlatBoard } from './FlatBoard';

const S = 0.011;
const toWorld = (point: Point, height = 0) => new THREE.Vector3((point.x - 360) * S, height, (point.y - 360) * S);

type Handlers = {
  game: Game;
  disabled: boolean;
  rolling: boolean;
  reducedMotion?: boolean;
  preview?: Move;
  onPiece: (piece: Piece, player: number) => void;
  onRoll: () => void;
};

export function FlightScene(props: Handlers) {
  const host = useRef<HTMLDivElement>(null);
  const landingLabel = useRef<HTMLSpanElement>(null);
  const labels = useRef(new Map<string, HTMLSpanElement>());
  const [status, setStatus] = useState<'ready' | 'lost' | 'unavailable'>('ready');
  const [generation, setGeneration] = useState(0);
  const propsRef = useRef(props);
  propsRef.current = props;

  useEffect(() => {
    const el = host.current;
    if (!el) return;
    let renderer: THREE.WebGLRenderer;
    try {
      renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true });
    } catch {
      setStatus('unavailable');
      return;
    }
    setStatus('ready');
    renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
    renderer.outputColorSpace = THREE.SRGBColorSpace;
    renderer.shadowMap.enabled = true;
    renderer.toneMapping = THREE.ACESFilmicToneMapping;
    renderer.toneMappingExposure = 1.05;
    el.appendChild(renderer.domElement);

    const scene = new THREE.Scene();
    scene.background = new THREE.Color('#e7f3f6');
    const camera = new THREE.PerspectiveCamera(38, 1, 0.1, 80);

    scene.add(new THREE.HemisphereLight('#fff8ee', '#7ea497', 1.15));
    const sun = new THREE.DirectionalLight('#fffaf2', 1.45);
    sun.position.set(3.2, 8.4, 2.4);
    sun.castShadow = true;
    sun.shadow.mapSize.set(1024, 1024);
    sun.shadow.camera.near = 1;
    sun.shadow.camera.far = 24;
    sun.shadow.camera.left = -8;
    sun.shadow.camera.right = 8;
    sun.shadow.camera.top = 8;
    sun.shadow.camera.bottom = -8;
    sun.shadow.camera.updateProjectionMatrix();
    scene.add(sun);

    const table = new THREE.Group();
    const tokens = new THREE.Group();
    scene.add(table, tokens);
    // Reuse preview geometry for every selection; never create a renderer per preview.
    const landing = new THREE.Group();
    const landingRing = new THREE.Mesh(new THREE.RingGeometry(.16, .23, 32), new THREE.MeshBasicMaterial({ color: '#fff2a5', side: THREE.DoubleSide, depthTest: false }));
    landingRing.rotation.x = -Math.PI / 2;
    const landingDisc = new THREE.Mesh(new THREE.CircleGeometry(.16, 32), new THREE.MeshBasicMaterial({ color: '#ffc84e', transparent: true, opacity: .55, side: THREE.DoubleSide, depthTest: false }));
    landingDisc.rotation.x = -Math.PI / 2;
    landing.add(landingRing, landingDisc); landing.visible = false;
    const previewGeometry = new THREE.BufferGeometry();
    previewGeometry.setAttribute('position', new THREE.BufferAttribute(new Float32Array(6), 3));
    previewGeometry.setAttribute('lineDistance', new THREE.BufferAttribute(new Float32Array(2), 1));
    const previewLine = new THREE.Line(previewGeometry, new THREE.LineDashedMaterial({ color: '#8b681a', dashSize: .11, gapSize: .07, depthTest: false }));
    previewLine.frustumCulled = false; previewLine.visible = false;
    scene.add(landing, previewLine);
    const box = (w: number, h: number, d: number) => {
      const geometry = new THREE.BoxGeometry(w, h, d);
      return geometry;
    };
    const paint = (color: string, extra?: THREE.MeshStandardMaterialParameters) => {
      const material = new THREE.MeshStandardMaterial({ color, roughness: 0.62, metalness: 0.02, ...extra });
      return material;
    };
    let layoutKey = '';
    const planes = new Map<string, { group: THREE.Group; halo: THREE.Mesh; shield: THREE.Mesh; lift: number; placed: boolean }>();
    const die = createDie(0.48);
    die.position.set(0, 0.36, 0);
    die.userData.kind = 'die';
    scene.add(die);
    let introduced = false;
    let dieValue = 0;
    let tumbling: { from: THREE.Quaternion; to: THREE.Quaternion; start: number; duration: number } | null = null;
    const timer = new THREE.Timer();
    timer.connect(document);
    const raycaster = new THREE.Raycaster();
    const pointer = new THREE.Vector2();

    const rebuild = (game: Game) => {
      const layout = createBoardLayout(game.board);
      disposeObjects(table);
      table.clear();
      disposeObjects(tokens);
      tokens.clear();
      planes.clear();
      const base = new THREE.Mesh(box(8.15, 0.16, 8.15), paint('#f4faf8'));
      base.position.y = -0.1;
      base.receiveShadow = true;
      table.add(base);
      const radius = game.board.layout === 'circle' ? 15 : layout.unit * 0.47;
      const tile = box(radius * 2 * S * 0.9, 0.09, radius * 2 * S * 0.9);
      for (const [index, point] of layout.route.entries()) {
        const mesh = new THREE.Mesh(tile, paint(palette[colors[index % 4]]));
        mesh.position.copy(toWorld(point, 0.05));
        mesh.receiveShadow = true;
        mesh.castShadow = true;
        table.add(mesh);
      }
      layout.home.forEach((lane, player) => {
        for (const point of lane) {
          const mesh = new THREE.Mesh(tile, paint(palette[colors[player]], { roughness: 0.45 }));
          mesh.position.copy(toWorld(point, 0.055));
          mesh.receiveShadow = true;
          table.add(mesh);
        }
      });
      layout.airports.forEach((airport, index) => {
        const player = game.players[index];
        const mesh = new THREE.Mesh(box(airport.size * S, 0.05, airport.size * S), paint(palette[colors[index]], { transparent: !player, opacity: player ? 1 : 0.35 }));
        mesh.position.copy(toWorld({ x: airport.x + airport.size / 2, y: airport.y + airport.size / 2 }, 0.02));
        mesh.receiveShadow = true;
        table.add(mesh);
        const name = sprite(player ? player.name : '等待登机', '#ffffff');
        name.position.copy(toWorld({ x: airport.x + airport.size / 2, y: airport.y + 28 }, 0.28));
        table.add(name);
      });
      layout.pads.forEach((pad, player) => {
        const mesh = new THREE.Mesh(new THREE.CylinderGeometry(layout.unit * 0.34 * S, layout.unit * 0.34 * S, 0.04, 20), paint('#ffffff'));
        mesh.position.copy(toWorld(pad, 0.04));
        table.add(mesh);
      });
      const center = new THREE.Mesh(new THREE.CylinderGeometry(0.62, 0.66, 0.05, 40), paint('#ffffff', { roughness: 0.35 }));
      center.position.y = 0.03;
      center.receiveShadow = true;
      table.add(center);
      colors.forEach((color, player) => {
        const from = layout.route[(player * layout.route.length / 4 + game.board.fly_from - 1) % layout.route.length];
        const to = layout.route[(player * layout.route.length / 4 + game.board.fly_to - 1) % layout.route.length];
        const geometry = new THREE.BufferGeometry().setFromPoints([toWorld(from, 0.08), toWorld(to, 0.08)]);
        const material = new THREE.LineDashedMaterial({ color: palette[color], dashSize: 0.08, gapSize: 0.05, transparent: true, opacity: 0.7 });
        const line = new THREE.Line(geometry, material);
        line.computeLineDistances();
        table.add(line);
      });
    };

    const syncTokens = (game: Game, layout: BoardLayout) => {
      const seen = new Set<string>();
      for (const item of boardPieces(game, layout)) {
        seen.add(item.piece.id);
        let token = planes.get(item.piece.id);
        if (!token) {
          token = makePlane(palette[item.player.color]);
          token.group.userData.kind = 'piece';
          planes.set(item.piece.id, token);
          tokens.add(token.group);
        }
        const target = toWorld(item.point, 0.12);
        const previous = token.group.userData.target as THREE.Vector3 | undefined;
        const next = ahead(layout, game.board, item.playerIndex, item.piece);
        const dx = next.x - item.point.x;
        const dz = next.y - item.point.y;
        token.group.userData.piece = item.piece;
        token.group.userData.playerIndex = item.playerIndex;
        token.group.userData.heading = Math.hypot(dx, dz) < 1 ? item.playerIndex * Math.PI / 2 : Math.atan2(dx, dz);
        token.group.userData.scale = item.stackSize > 1 ? 0.72 : 1;
        if (!token.placed) {
          token.group.position.copy(target);
          token.group.rotation.y = token.group.userData.heading;
          token.placed = true;
        } else if (previous && previous.distanceTo(target) > 0.12 && token.group.position.distanceTo(target) > 0.45) token.lift = 1;
        token.group.userData.target = target;
        token.shield.visible = !!item.piece.shield;
      }
      for (const [id, token] of planes) {
        if (seen.has(id)) continue;
        disposeObjects(token.group);
        tokens.remove(token.group);
        planes.delete(id);
      }
    };

    let previewKey = '';
    const previewStart = new THREE.Vector3(), previewEnd = new THREE.Vector3();
    const resize = () => {
      const width = el.clientWidth || 720;
      const height = el.clientHeight || width;
      renderer.setSize(width, height, false);
      fitBoardCamera(camera, width / Math.max(height, 1));
    };
    const observer = new ResizeObserver(resize);
    observer.observe(el);
    resize();

    const pick = (event: PointerEvent) => {
      const rect = renderer.domElement.getBoundingClientRect();
      pointer.x = ((event.clientX - rect.left) / rect.width) * 2 - 1;
      pointer.y = -((event.clientY - rect.top) / rect.height) * 2 + 1;
      raycaster.setFromCamera(pointer, camera);
      const hits = raycaster.intersectObjects(scene.children, true);
      for (const hit of hits) {
        let object: THREE.Object3D | null = hit.object;
        while (object) {
          if (object.userData.kind === 'piece' && !propsRef.current.disabled) {
            propsRef.current.onPiece(object.userData.piece as Piece, object.userData.playerIndex as number);
            return;
          }
          if (object.userData.kind === 'die' && object.userData.canRoll) {
            propsRef.current.onRoll();
            return;
          }
          object = object.parent;
        }
      }
    };
    const hover = (event: PointerEvent) => {
      const rect = renderer.domElement.getBoundingClientRect();
      pointer.x = ((event.clientX - rect.left) / rect.width) * 2 - 1;
      pointer.y = -((event.clientY - rect.top) / rect.height) * 2 + 1;
      raycaster.setFromCamera(pointer, camera);
      const hits = raycaster.intersectObjects(scene.children, true);
      let active = false;
      for (const hit of hits) {
        let object: THREE.Object3D | null = hit.object;
        while (object) {
          active = (object.userData.kind === 'piece' && object.userData.legal) || (object.userData.kind === 'die' && object.userData.canRoll);
          if (object.userData.kind) break;
          object = object.parent;
        }
        if (active) break;
      }
      renderer.domElement.style.cursor = active ? 'pointer' : 'default';
    };
    renderer.domElement.addEventListener('pointerdown', pick);
    renderer.domElement.addEventListener('pointermove', hover);

    let frame = 0;
    let lost = false;
    let disposed = false;
    let seenGame: Game | undefined;
    const projected = new THREE.Vector3();
    const animate = () => {
      if (disposed || lost || document.hidden) return;
      frame = requestAnimationFrame(animate);
      timer.update();
      const dt = Math.min(timer.getDelta(), 0.05);
      const now = performance.now();
      const current = propsRef.current;
      const reduced = !!current.reducedMotion;
      const key = JSON.stringify([current.game.id, current.game.board.id, current.game.players.map(p => [p.name, p.color])]);
      if (key !== layoutKey) {
        layoutKey = key;
        rebuild(current.game);
      }
      if (seenGame !== current.game) {
        syncTokens(current.game, createBoardLayout(current.game.board));
        seenGame = current.game;
      }
      const legal = new Set(!current.disabled && current.game.phase === 'move' ? current.game.legal_moves.map(move => move.piece_id) : []);
      for (const [id, token] of planes) {
        const target = token.group.userData.target as THREE.Vector3;
        const heading = token.group.userData.heading as number;
        token.group.userData.legal = legal.has(id);
        token.halo.visible = legal.has(id);
        if (legal.has(id)) token.halo.scale.setScalar(reduced ? 1 : 1 + Math.sin(now / 280) * 0.08);
        token.lift = Math.max(0, token.lift - dt * (reduced ? 8 : 1.15));
        const hop = Math.sin(token.lift * Math.PI) * 0.48;
        if (reduced) {
          token.lift = 0;
          token.group.position.copy(target);
          token.group.rotation.y = heading;
        } else {
          token.group.position.lerp(new THREE.Vector3(target.x, target.y + hop, target.z), 1 - Math.pow(0.001, dt));
          token.group.rotation.y = dampAngle(token.group.rotation.y, heading, dt);
        }
        const scale = ((token.group.userData.scale as number) || 1) * 1.28;
        token.group.scale.setScalar(scale);
        const label = labels.current.get(id);
        if (label) {
          projected.copy(token.group.position);
          projected.y += 0.24;
          projected.project(camera);
          label.style.left = `${(projected.x + 1) * 50}%`;
          label.style.top = `${(1 - projected.y) * 50}%`;
          label.dataset.legal = String(legal.has(id));
          label.dataset.selected = String(current.preview?.piece_id === id);
        }
      }
      const move = !current.disabled && current.game.phase === 'move' ? current.preview : undefined;
      const previewPiece = move && current.game.players[current.game.current].pieces.find(piece => piece.id === move.piece_id);
      landing.visible = previewLine.visible = !!previewPiece;
      if (landingLabel.current) landingLabel.current.hidden = !previewPiece;
      if (move && previewPiece) {
        const key = `${current.game.id}:${current.game.version}:${move.piece_id}`;
        if (key !== previewKey) {
          previewKey = key;
          const layout = createBoardLayout(current.game.board);
          const count = current.game.players[current.game.current].pieces.length;
          previewEnd.copy(toWorld(piecePosition(layout, current.game.board, current.game.current, { ...previewPiece, progress: move.to }, count), .24));
          previewStart.copy(toWorld(piecePosition(layout, current.game.board, current.game.current, previewPiece, count), .24));
          landing.position.copy(previewEnd);
          const points = previewGeometry.getAttribute('position') as THREE.BufferAttribute;
          points.setXYZ(0, previewStart.x, previewStart.y, previewStart.z); points.setXYZ(1, previewEnd.x, previewEnd.y, previewEnd.z); points.needsUpdate = true;
          const distances = previewGeometry.getAttribute('lineDistance') as THREE.BufferAttribute;
          distances.setX(1, previewStart.distanceTo(previewEnd)); distances.needsUpdate = true;
        }
        landingRing.scale.setScalar(reduced ? 1 : 1 + Math.sin(now / 220) * .08);
        if (landingLabel.current) {
          projected.copy(previewEnd); projected.y += .3; projected.project(camera);
          landingLabel.current.style.left = `${(projected.x + 1) * 50}%`;
          landingLabel.current.style.top = `${(1 - projected.y) * 50}%`;
        }
      }
      die.userData.canRoll = !current.disabled && current.game.phase === 'roll';
      const nextValue = current.game.dice || 1;
      if (!introduced) {
        die.quaternion.copy(dieQuaternion(nextValue));
        dieValue = nextValue;
        introduced = true;
      } else if (current.rolling && !reduced) {
        die.rotation.x += dt * 9;
        die.rotation.y += dt * 7;
        tumbling = null;
        dieValue = 0;
      } else if (dieValue !== nextValue) {
        tumbling = { from: die.quaternion.clone(), to: dieQuaternion(nextValue), start: now, duration: reduced ? 0 : 480 };
        dieValue = nextValue;
      }
      if (tumbling) {
        const t = tumbling.duration === 0 ? 1 : Math.min(1, (now - tumbling.start) / tumbling.duration);
        const eased = 1 - Math.pow(1 - t, 3);
        die.quaternion.slerpQuaternions(tumbling.from, tumbling.to, eased);
        die.position.y = 0.36 + Math.sin(t * Math.PI) * 0.18;
        if (t >= 1) tumbling = null;
      } else if (!current.rolling) die.position.y += (0.36 - die.position.y) * 0.2;
      renderer.render(scene, camera);
    };
    const contextLost = (event: Event) => {
      event.preventDefault();
      lost = true;
      cancelAnimationFrame(frame);
      setStatus('lost');
    };
    const contextRestored = () => {
      lost = false;
      setStatus('ready');
      resize();
      cancelAnimationFrame(frame);
      animate();
    };
    const visibilityChanged = () => {
      cancelAnimationFrame(frame);
      if (!document.hidden) animate();
    };
    renderer.domElement.addEventListener('webglcontextlost', contextLost);
    renderer.domElement.addEventListener('webglcontextrestored', contextRestored);
    document.addEventListener('visibilitychange', visibilityChanged);
    animate();

    return () => {
      disposed = true;
      cancelAnimationFrame(frame);
      observer.disconnect();
      timer.dispose();
      document.removeEventListener('visibilitychange', visibilityChanged);
      renderer.domElement.removeEventListener('webglcontextlost', contextLost);
      renderer.domElement.removeEventListener('webglcontextrestored', contextRestored);
      renderer.domElement.removeEventListener('pointerdown', pick);
      renderer.domElement.removeEventListener('pointermove', hover);
      disposeObjects(scene);
      sun.shadow.dispose();
      renderer.dispose();
      renderer.forceContextLoss();
      renderer.domElement.remove();
    };
  }, [generation]);

  return <>
    <div ref={host} className={'flight-host' + (status !== 'ready' ? ' scene-hidden' : '')} aria-hidden="true">
      <div className="plane-labels"><span ref={landingLabel} hidden className="landing-marker" aria-hidden="true">预计落点</span>{props.game.players.flatMap(player => player.pieces.map(piece => <span
        className="plane-marker" key={piece.id} ref={node => { if (node) labels.current.set(piece.id, node); else labels.current.delete(piece.id); }}
        style={{ color: palette[player.color] }} title={`${player.name} · ${piece.number}号 ${piece.label}`}>
        <b>{piece.number}</b><small>{piece.label}</small>
      </span>))}</div>
    </div>
    {status !== 'ready' && <div className="scene-recovery" role="status">
      <p>{status === 'lost' ? '3D 画面暂时中断，已切换到平面棋盘，游戏进度已保留。' : '当前浏览器暂时无法显示 3D，已切换到平面棋盘。'}
        <button type="button" onClick={() => setGeneration(value => value + 1)}>恢复 3D 棋盘</button></p>
      <FlatBoard {...props} />
    </div>}
  </>;
}

function ahead(layout: BoardLayout, board: BoardSpec, player: number, piece: Piece): Point {
  if (piece.progress < 0) return layout.pads[player];
  if (piece.progress >= board.finish) return { x: 360, y: 360 };
  return piecePosition(layout, board, player, { ...piece, progress: piece.progress + 1 }, 1);
}

function makePlane(color: string) {
  const material = new THREE.MeshStandardMaterial({ color, roughness: 0.38, metalness: 0.06 });
  const glass = new THREE.MeshStandardMaterial({ color: '#f4fbff', roughness: 0.16, metalness: 0.04 });
  const group = new THREE.Group();
  const body = new THREE.Mesh(new THREE.CapsuleGeometry(0.055, 0.2, 4, 10), material);
  body.rotation.x = Math.PI / 2;
  body.castShadow = true;
  const wing = new THREE.Mesh(new THREE.BoxGeometry(0.4, 0.02, 0.09), material);
  wing.position.set(0, 0.012, 0.02);
  wing.castShadow = true;
  const tailWing = new THREE.Mesh(new THREE.BoxGeometry(0.16, 0.016, 0.05), material);
  tailWing.position.set(0, 0.012, -0.12);
  const fin = new THREE.Mesh(new THREE.BoxGeometry(0.02, 0.09, 0.07), material);
  fin.position.set(0, 0.06, -0.12);
  fin.castShadow = true;
  const canopy = new THREE.Mesh(new THREE.SphereGeometry(0.038, 12, 8), glass);
  canopy.position.set(0, 0.048, 0.035);
  canopy.scale.set(0.85, 0.65, 1.25);
  const halo = new THREE.Mesh(new THREE.RingGeometry(0.15, 0.2, 32), new THREE.MeshBasicMaterial({ color: '#ffe27a', transparent: true, opacity: 0.95, side: THREE.DoubleSide }));
  halo.rotation.x = -Math.PI / 2;
  halo.position.y = -0.02;
  halo.visible = false;
  const shield = new THREE.Mesh(new THREE.TorusGeometry(0.24, 0.014, 8, 32), new THREE.MeshStandardMaterial({ color: '#8fe7f8', emissive: '#14789a', emissiveIntensity: 0.35, roughness: 0.3 }));
  shield.rotation.x = Math.PI / 2;
  shield.visible = false;
  group.add(body, wing, tailWing, fin, canopy, halo, shield);
  return { group, halo, shield, lift: 0, placed: false };
}

function sprite(text: string, color: string) {
  const canvas = document.createElement('canvas');
  canvas.width = 512;
  canvas.height = 96;
  const ctx = canvas.getContext('2d');
  if (ctx) {
    const label = [...text].length > 8 ? [...text].slice(0, 7).join('') + '…' : text;
    ctx.font = '700 54px sans-serif';
    const width = Math.min(480, ctx.measureText(label).width + 48);
    ctx.fillStyle = '#16342ccc';
    ctx.beginPath();
    ctx.roundRect(256 - width / 2, 14, width, 68, 18);
    ctx.fill();
    ctx.fillStyle = color;
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillText(label, 256, 48);
  }
  const texture = new THREE.CanvasTexture(canvas);
  texture.colorSpace = THREE.SRGBColorSpace;
  const material = new THREE.SpriteMaterial({ map: texture, transparent: true });
  const spriteMesh = new THREE.Sprite(material);
  spriteMesh.scale.set(2.05, 0.38, 1);
  return spriteMesh;
}

function dampAngle(current: number, target: number, dt: number) {
  const delta = Math.atan2(Math.sin(target - current), Math.cos(target - current));
  return current + delta * (1 - Math.pow(0.001, dt));
}
