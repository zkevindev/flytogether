import type { Game } from './types';

export function announcementKey(game: Game) {
  return game.phase === 'finished' ? `${game.id}:winner` : game.phase === 'roll' ? `${game.id}:${game.turn}` : '';
}

export class Narrator {
  private context?: AudioContext;
  private source?: AudioBufferSourceNode;
  private epoch = 0;
  private audio() { return this.context ||= new AudioContext(); }
  unlock() { return this.audio().resume().catch(() => {}); }
  async playPCM(encoded: string, sampleRate: number, ended: () => void): Promise<boolean> {
    this.stop();
    const epoch = this.epoch;
    const audio = this.audio();
    let timer: ReturnType<typeof setTimeout> | undefined;
    await Promise.race([this.unlock(), new Promise<void>(resolve => { timer = setTimeout(resolve, 500); })]);
    clearTimeout(timer);
    if (epoch !== this.epoch || audio.state !== 'running') return false;
    const bytes = atob(encoded);
    if (!bytes.length || bytes.length % 2 || bytes.length > 2 * 1024 * 1024 || sampleRate !== 24000) throw new Error('播报音频无效');
    const buffer = audio.createBuffer(1, bytes.length / 2, sampleRate);
    const samples = buffer.getChannelData(0);
    for (let i = 0; i < samples.length; i++) {
      const value = bytes.charCodeAt(i * 2) | bytes.charCodeAt(i * 2 + 1) << 8;
      samples[i] = (value & 0x8000 ? value - 0x10000 : value) / 32768;
    }
    const source = this.source = audio.createBufferSource();
    source.buffer = buffer; source.connect(audio.destination);
    source.onended = () => { source.disconnect(); if (this.source === source) { this.source = undefined; ended(); } };
    source.start();
    return true;
  }
  stop() {
    this.epoch++;
    if (this.source) { this.source.onended = null; try { this.source.stop(); } catch {} this.source.disconnect(); this.source = undefined; }
  }
  dispose() { this.stop(); void this.context?.close().catch(() => {}); this.context = undefined; }
}
