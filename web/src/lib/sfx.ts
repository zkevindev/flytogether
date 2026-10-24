export type Cue = 'roll' | 'luck' | 'hop' | 'launch' | 'glide' | 'bump' | 'shield' | 'six' | 'home' | 'win' | 'card' | 'welcome' | 'boomerang' | 'rescue' | 'escort';

type GameEvent = { kind: string; text: string; prop_id?:string; effects?:{kind:string}[] };

function moveCue(text: string): Cue {
  if (text.includes('终点') || text.includes('抵达')) return 'home';
  if (text.includes('回机场')) return 'bump';
  if (text.includes('护盾')) return 'shield';
  if (text.includes('飞行') || text.includes('跳跃')) return 'glide';
  if (text.includes('起飞')) return 'launch';
  return 'hop';
}

export function cuesFor(events: GameEvent[]): Cue[] {
  if (events.some(event => event.kind === 'won')) return ['win'];
  const cues: Cue[] = [];
  for (const event of events) {
    if (event.kind === 'rolled') cues.push(event.text.includes('幸运') ? 'luck' : 'roll');
    else if (event.kind === 'bonus') cues.push('six');
    else if (event.kind === 'moved') cues.push(moveCue(event.text));
    else if (event.kind === 'prop') {
      if (event.effects?.some(e=>e.kind==='blocked')) cues.push('shield');
      else if (['boomerang','rescue','escort'].includes(event.prop_id || '')) cues.push(event.prop_id as Cue);
      else if (event.text.includes('护盾') && !event.text.includes('送回')) cues.push('shield');
      else if (event.text.includes('炸弹') || event.text.includes('送回')) cues.push('bump');
      else cues.push('card');
    }
  }
  return cues.slice(0, 3);
}

function reducedMotion() {
  return typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;
}

export class Sfx {
  enabled = true;
  private ctx?: AudioContext;
  private master?: GainNode;
  private noise?: AudioBuffer;
  private nodes: AudioNode[] = [];

  play(cues: Cue[]) {
    if (!this.enabled || cues.length === 0 || typeof AudioContext === 'undefined') return;
    const ctx = this.ensure();
    void ctx.resume();
    this.stopNodes();
    let time = ctx.currentTime + 0.02;
    for (const cue of cues) time = this.cue(cue, time);
  }

  stop() { this.stopNodes(); }

  dispose() {
    this.stopNodes();
    void this.ctx?.close();
    this.ctx = undefined;
    this.master = undefined;
    this.noise = undefined;
  }

  private ensure() {
    if (!this.ctx) {
      this.ctx = new AudioContext();
      this.master = this.ctx.createGain();
      this.master.gain.value = 0.8;
      this.master.connect(this.ctx.destination);
    }
    return this.ctx;
  }

  private stopNodes() {
    for (const node of this.nodes) {
      const source = node as AudioScheduledSourceNode;
      try { source.stop(); } catch { /* already stopped */ }
      node.disconnect();
    }
    this.nodes = [];
  }

  private cue(cue: Cue, time: number) {
    const land = reducedMotion() ? 0.12 : 0.9;
    switch (cue) {
      case 'roll':
        this.rattle(time);
        this.knock(time + land);
        return time + land + 0.16;
      case 'luck':
        this.rattle(time);
        this.knock(time + land);
        this.knock(time + land + 0.12, 1.08);
        return time + land + 0.28;
      case 'hop':
        this.tone(time, 494, 0.09, 'triangle', 0.045);
        this.tone(time + 0.08, 659, 0.11, 'triangle', 0.04);
        return time + 0.22;
      case 'launch':
        this.glideTone(time, 370, 1046, 0.38, 0.055);
        this.noiseBurst(time + 0.02, 0.34, 1200, 0.032);
        return time + 0.48;
      case 'glide':
        this.tone(time, 523, 0.1, 'triangle', 0.04);
        this.tone(time + 0.09, 659, 0.1, 'triangle', 0.04);
        this.tone(time + 0.18, 784, 0.14, 'triangle', 0.045);
        this.noiseBurst(time, 0.22, 900, 0.03);
        return time + 0.36;
      case 'bump':
        this.glideTone(time, 620, 52, 0.62, 0.045, 'sawtooth');
        this.noiseBurst(time + 0.05, 0.45, 650, 0.025);
        this.tone(time + 0.6, 55, 0.18, 'sine', 0.075);
        this.noiseBurst(time + 0.6, 0.18, 180, 0.055);
        return time + 0.9;
      case 'boomerang':
        this.glideTone(time, 300, 1100, .24, .04);
        this.glideTone(time+.25, 1100, 350, .3, .04);
        return time+.6;
      case 'rescue':
        [660,880,660,1046].forEach((f,i)=>this.tone(time+i*.12,f,.16,'sine',.035));
        return time+.65;
      case 'escort':
        [523,659,784].forEach((f,i)=>this.tone(time+i*.09,f,.35,'sine',.03));
        return time+.6;
      case 'shield':
        this.tone(time, 784, 0.28, 'sine', 0.03);
        this.tone(time, 1175, 0.34, 'sine', 0.02);
        return time + 0.36;
      case 'six':
        this.tone(time, 784, 0.1, 'triangle', 0.045);
        this.tone(time + 0.1, 988, 0.14, 'triangle', 0.04);
        return time + 0.28;
      case 'home':
        [523, 659, 784, 1046].forEach((freq, i) => this.tone(time + i * 0.09, freq, 0.16, 'triangle', 0.04));
        return time + 0.5;
      case 'win':
        [523, 659, 784, 1046].forEach((freq, i) => this.tone(time + i * 0.11, freq, 0.2, 'triangle', 0.045));
        this.tone(time + 0.46, 784, 0.28, 'sine', 0.03);
        return time + 0.8;
      case 'card':
        this.tone(time, 440, 0.08, 'triangle', 0.04);
        this.tone(time + 0.07, 554, 0.1, 'triangle', 0.03);
        return time + 0.2;
      case 'welcome':
        [523, 659, 784].forEach((freq, i) => this.tone(time + i * 0.1, freq, 0.16, 'triangle', 0.035));
        return time + 0.4;
    }
  }

  private rattle(time: number) {
    for (let i = 0; i < 6; i++) this.noiseBurst(time + i * 0.045, 0.03, 1400 + i * 180, 0.045);
  }

  private knock(time: number, pitch = 1) {
    this.tone(time, 186 * pitch, 0.07, 'sine', 0.06);
    this.tone(time, 340 * pitch, 0.05, 'triangle', 0.035);
    this.noiseBurst(time, 0.02, 1600, 0.04);
  }

  private tone(time: number, freq: number, duration: number, type: OscillatorType, peak: number) {
    const ctx = this.ensure();
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.type = type;
    osc.frequency.setValueAtTime(freq, time);
    gain.gain.setValueAtTime(0.0001, time);
    gain.gain.exponentialRampToValueAtTime(Math.max(peak, 0.0002), time + 0.015);
    gain.gain.exponentialRampToValueAtTime(0.0001, time + duration);
    osc.connect(gain);
    gain.connect(this.master!);
    osc.start(time);
    osc.stop(time + duration + 0.02);
    this.nodes.push(osc);
  }

  private glideTone(time: number, from: number, to: number, duration: number, peak: number, type: OscillatorType = 'triangle') {
    const ctx = this.ensure();
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.type = type;
    osc.frequency.setValueAtTime(from, time);
    osc.frequency.exponentialRampToValueAtTime(Math.max(to, 1), time + duration);
    gain.gain.setValueAtTime(0.0001, time);
    gain.gain.exponentialRampToValueAtTime(peak, time + 0.03);
    gain.gain.exponentialRampToValueAtTime(0.0001, time + duration);
    osc.connect(gain);
    gain.connect(this.master!);
    osc.start(time);
    osc.stop(time + duration + 0.02);
    this.nodes.push(osc);
  }

  private noiseBurst(time: number, duration: number, band: number, peak: number) {
    const ctx = this.ensure();
    if (!this.noise) {
      this.noise = ctx.createBuffer(1, ctx.sampleRate, ctx.sampleRate);
      const data = this.noise.getChannelData(0);
      for (let i = 0; i < data.length; i++) data[i] = Math.random() * 2 - 1;
    }
    const source = ctx.createBufferSource();
    source.buffer = this.noise;
    const filter = ctx.createBiquadFilter();
    filter.type = 'bandpass';
    filter.frequency.value = band;
    filter.Q.value = 0.8;
    const gain = ctx.createGain();
    gain.gain.setValueAtTime(0.0001, time);
    gain.gain.exponentialRampToValueAtTime(peak, time + 0.008);
    gain.gain.exponentialRampToValueAtTime(0.0001, time + duration);
    source.connect(filter);
    filter.connect(gain);
    gain.connect(this.master!);
    source.start(time);
    source.stop(time + duration + 0.02);
    this.nodes.push(source);
  }
}
