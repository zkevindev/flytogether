import { useDialog } from '../lib/dialog';
import { useEffect, useRef, useState } from 'react';
import { ArrowRight, Sparkle } from '@phosphor-icons/react';
import { DiceCanvas } from './DiceCanvas';

export function DiceRollModal({ roll, onClose, reducedMotion = false, waitForConfirm = false }: { waitForConfirm?: boolean; reducedMotion?: boolean; roll: { text: string; values: number[] }; onClose: () => void }) {
  const ref = useDialog();
  const [landed, setLanded] = useState(false);
  const close = useRef(onClose); close.current = onClose;
  useEffect(() => {
    const duration = reducedMotion ? 0 : 950;
    const land = setTimeout(() => setLanded(true), duration);
    const dismiss = reducedMotion && waitForConfirm ? undefined : setTimeout(() => close.current(), duration + 1600);
    return () => { clearTimeout(land); clearTimeout(dismiss); };
  }, [reducedMotion, waitForConfirm]);
  const value = Math.max(...roll.values);
  const showResult = reducedMotion || landed;
  return <dialog ref={ref} className="dice-modal" aria-label="3D 掷骰结果" onCancel={e => { e.preventDefault(); close.current(); }}>
    <div className="dice-modal-kicker"><Sparkle weight="fill" /> {roll.values.length === 2 ? '幸运双骰' : '好运降落中'}</div>
    <h2>{showResult ? `${value} 点！${value === 6 ? '好运降落' : '准备出发'}` : '骰子转呀转…'}</h2>
    <DiceCanvas values={roll.values} reducedMotion={reducedMotion} />
    <p aria-live="polite">{showResult ? roll.text : '点数由游戏骰子随机产生'}</p>
    <button className="primary-button" onClick={() => close.current()}>继续飞行 <ArrowRight /></button>
  </dialog>;
}
