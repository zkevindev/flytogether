import { AirplaneTilt, ArrowsLeftRight, Robot } from '@phosphor-icons/react';

type Props = {
  name: string;
  phase: 'roll' | 'move' | 'finished';
  isRobot: boolean;
  visible: boolean;
};

export function PassDevicePrompt({ name, phase, isRobot, visible }: Props) {
  if (!visible || phase === 'finished') return null;

  if (isRobot) {
    return <div className="pass-device-prompt pass-device-robot" role="status" aria-live="polite">
      <Robot size={22} weight="fill" aria-hidden="true" />
      <span><strong>{name}正在自动行动</strong><small>不用传手机，稍等它走完</small></span>
    </div>;
  }

  return <div className="pass-device-prompt" role="status" aria-live="polite">
    <ArrowsLeftRight size={22} weight="bold" aria-hidden="true" />
    <span><strong>把手机交给{name}</strong><small>{phase === 'move' ? '现在选一架飞机' : '现在掷骰子'}</small></span>
    <AirplaneTilt className="pass-device-plane" size={24} weight="fill" aria-hidden="true" />
  </div>;
}
