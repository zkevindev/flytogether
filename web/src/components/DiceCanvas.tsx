import type { CSSProperties } from 'react';

const pips: Record<number, number[]> = { 1: [4], 2: [0, 8], 3: [0, 4, 8], 4: [0, 2, 6, 8], 5: [0, 2, 4, 6, 8], 6: [0, 2, 3, 5, 6, 8] };
const rotations = ['', 'rotateX(0deg) rotateY(0deg)', 'rotateX(0deg) rotateY(-90deg)', 'rotateX(-90deg) rotateY(0deg)', 'rotateX(90deg) rotateY(0deg)', 'rotateX(0deg) rotateY(90deg)', 'rotateX(0deg) rotateY(180deg)'];

// CSS cubes preserve the 3D throw without allocating a WebGL context per popup.
export function DiceCanvas({ values, reducedMotion = false }: { values: number[]; reducedMotion?: boolean }) {
  return <div className="dice-stage" aria-hidden="true">{values.map((value, index) => <div key={index} className="cube-perspective">
    <div className={'dice-cube ' + (reducedMotion ? 'settled' : 'tumbling')} data-value={value} style={{ '--landing': rotations[value] || rotations[1] } as CSSProperties}>
      {[1, 2, 3, 4, 5, 6].map(face => <div key={face} className={'cube-face face-' + face}>
        {Array.from({ length: 9 }, (_, pip) => <i key={pip} className={'cube-pip' + (pips[face].includes(pip) ? ' visible' : '')} />)}
      </div>)}
    </div>
  </div>)}</div>;
}
