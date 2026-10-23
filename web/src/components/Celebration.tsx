import { Trophy, Sparkle } from '@phosphor-icons/react';
export function Celebration({ text, won }: { text: string; won: boolean }) {
  const colors = ['#db6655', '#e1b449', '#74b6d6', '#679d7c', '#b78bcc'];
  return <div className={'celebration ' + (won ? 'celebration-win' : '')} role="status" aria-label={won ? '胜利撒花庆祝' : '抵达终点撒花庆祝'}>
    <div className="celebration-message">{won ? <Trophy weight="fill" /> : <Sparkle weight="fill" />}<strong>{won ? '这一局，飞得漂亮！' : '到家啦，撒花！'}</strong><span>{text}</span></div>
    <div className="confetti-rain" aria-hidden="true">{Array.from({ length: won ? 64 : 40 }, (_, i) => <i key={i} style={{ '--x': `${(i * 37 + 7) % 100}%`, '--delay': `${(i * 13 % 16) * 0.07}s`, '--drift': `${(i * 47 % 160) - 80}px`, '--spin': `${(i * 117 % 720) - 360}deg`, background: colors[i % colors.length] } as React.CSSProperties} />)}</div>
  </div>;
}
