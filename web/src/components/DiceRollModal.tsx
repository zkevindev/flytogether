import { useEffect, useRef, useState } from 'react';
import { ArrowRight, Sparkle } from '@phosphor-icons/react';
import { DiceCanvas } from './DiceCanvas';

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
    <h2>{landed ? `${value} 点！${value === 6 ? '好运降落' : '准备出发'}` : '骰子转呀转…'}</h2>
    <DiceCanvas values={roll.values} />
    <p aria-live="polite">{landed ? roll.text : '点数由游戏骰子随机产生'}</p>
    <button className="primary-button" onClick={() => close.current()}>继续飞行 <ArrowRight /></button>
  </dialog>;
}
