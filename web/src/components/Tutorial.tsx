import { useDialog } from '../lib/dialog';
import { useEffect, useRef, useState } from 'react';
import { AirplaneTilt, ArrowLeft, ArrowRight, Check, DiceSix, Flag, Sparkle, X } from '@phosphor-icons/react';

export const tutorialPreference = 'fly-tutorial-v1';
export function Tutorial({ propsEnabled, quick, voiceConfigured, onClose, gentle = false }: { gentle?: boolean; propsEnabled: boolean; quick: boolean; voiceConfigured: boolean; onClose: (completed: boolean) => void }) {
  const dialog = useDialog();
  const [step, setStep] = useState(0);
  const title = useRef<HTMLHeadingElement>(null);
  useEffect(() => { title.current?.focus(); }, [step]);
  const steps = [
    { title: '第一步，轮流掷骰子', text: `轮到自己时，点击“掷骰子”或棋盘中央的骰子。${gentle ? '掷到 5 或 6 都能起飞，只有 6 能再掷一次。' : '掷到 6 才能让机库里的飞机起飞，还能再掷一次。'}`, tip: propsEnabled ? '想直接出发？在道具口袋使用起飞卡，再选一架机库飞机，它会立即到起飞位，不用掷骰。' : `机库飞机暂时不能起飞时别着急，轮流掷骰，等待${gentle ? '五点或六点' : '六点'}就好。`, Icon: DiceSix },
    { title: '先看落点，再决定怎么飞', text: '掷完骰子，点选亮着的飞机或它的名字。棋盘会标出预计落点，并说明跳格、撞机和到达终点的效果。', tip: '点另一架可以比较；点“确认飞行”才会走棋，取消预览不会移动飞机。', Icon: Flag },
    { title: voiceConfigured ? '有灵矽陪你，慢慢就学会' : '照着提示，也能轻松起飞', text: propsEnabled ? '掷骰前每回合可用一张道具。起飞卡帮你出发，护盾能挡撞机；不能使用时，口袋里会告诉你原因。' : voiceConfigured ? '可以问灵矽“现在轮到谁”“为什么这架不能走”，也可以直接说“让小熊号起飞”。' : '按回合提示掷骰、看落点、选飞机。想再看看规则，可以点右上角的“玩法”；不用麦克风也能完整游玩。', tip: quick ? (voiceConfigured ? '本局先送达一架飞机就获胜。可以先问建议，再自己做决定。' : '本局先送达一架飞机就获胜。先看清落点，再决定怎么飞。') : '本局要把自己的四架飞机全部送到终点。点数要恰好，超过终点不能走。', Icon: Sparkle },
  ];
  const current = steps[step];
  return <dialog ref={dialog} className="modal tutorial-modal" aria-label="新手飞行指南" onCancel={e => { e.preventDefault(); onClose(false); }}>
    <div className="modal-head"><span className="tutorial-kicker">一起飞 · 新手飞行指南</span><button className="icon-button" aria-label="跳过新手引导" onClick={() => onClose(false)}><X /></button></div>
    <div className="tutorial-art" aria-hidden="true"><AirplaneTilt weight="fill" /><span /><current.Icon weight="duotone" /></div>
    <p className="tutorial-progress">{step + 1} / {steps.length}</p>
    <h2 ref={title} tabIndex={-1}>{current.title}</h2><p className="tutorial-description">{current.text}</p><p className="tutorial-tip"><Check size={18} />{current.tip}</p>
    <div className="tutorial-dots" aria-hidden="true">{steps.map((_, i) => <i key={i} className={i === step ? 'active' : ''} />)}</div>
    <div className="tutorial-actions">{step > 0 ? <button className="outline-button" onClick={() => setStep(value => value - 1)}><ArrowLeft />上一步</button> : <button className="text-button" onClick={() => onClose(false)}>跳过引导</button>}<button className="primary-button" onClick={() => step === steps.length - 1 ? onClose(true) : setStep(value => value + 1)}>{step === steps.length - 1 ? '开始玩' : '下一步'}<ArrowRight /></button></div>
    <p className="tutorial-reopen">以后点右上角“新手指南”，随时再看。</p>
  </dialog>;
}
