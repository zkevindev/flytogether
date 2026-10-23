import { useEffect, useRef, useState } from 'react';
import { ArrowRight, Sparkle } from '@phosphor-icons/react';

const pips: Record<number, number[]> = { 1: [4], 2: [0, 8], 3: [0, 4, 8], 4: [0, 2, 6, 8], 5: [0, 2, 4, 6, 8], 6: [0, 2, 3, 5, 6, 8] };
const rotations = ['', 'rotateX(0deg) rotateY(0deg)', 'rotateX(0deg) rotateY(-90deg)', 'rotateX(-90deg) rotateY(0deg)', 'rotateX(90deg) rotateY(0deg)', 'rotateX(0deg) rotateY(90deg)', 'rotateX(0deg) rotateY(180deg)'];

export function DiceRollModal({ roll, onClose }: { roll: { text: string; values: number[] }; onClose: () => void }) {
  const ref = useRef<HTMLDialogElement>(null);
  const [landed, setLanded] = useState(false);
  const close = useRef(onClose); close.current = onClose;
  useEffect(() => {
    ref.current?.showModal();
    const duration = matchMedia('(prefers-reduced-motion: reduce)').matches ? 0 : 950;
    const land = setTimeout(() => setLanded(true), duration);
    const dismiss = setTimeout(() => close.current(), duration + 1600);
    return () => { clearTimeout(land); clearTimeout(dismiss); };
  }, []);
  const value = Math.max(...roll.values);
  return <dialog ref={ref} className="dice-modal" aria-label="3D 掷骰结果" onCancel={e => { e.preventDefault(); close.current(); }}>
    <div className="dice-modal-kicker"><Sparkle weight="fill" /> {roll.values.length === 2 ? '幸运双骰' : '好运降落中'}</div>
    <h2>{landed ? `${value} 点！${value === 6 ? '再来一次' : '准备出发'}` : '骰子转呀转…'}</h2>
    <div className="dice-stage" aria-hidden="true">{roll.values.map((result, i) => <div key={i} className="cube-perspective"><div className={'dice-cube ' + (!landed ? 'tumbling' : 'landed')} style={{ '--landing': rotations[result] } as React.CSSProperties}>
      {[1, 2, 3, 4, 5, 6].map(face => <div key={face} className={'cube-face face-' + face}>{Array.from({ length: 9 }, (_, n) => <i key={n} className={pips[face].includes(n) ? 'cube-pip visible' : 'cube-pip'} />)}</div>)}
    </div></div>)}</div>
    <p aria-live="polite">{landed ? roll.text : '点数由游戏骰子随机产生'}</p>
    <button className="primary-button" onClick={() => close.current()}>继续飞行 <ArrowRight /></button>
  </dialog>;
}
