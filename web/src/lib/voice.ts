import { socketURL } from './types';

type RealtimeMessage = { type: string; [key: string]: any };

export class VoiceClient {
  private ws?: WebSocket;
  private audio?: AudioContext;
  private stream?: MediaStream;
  private capture?: AudioWorkletNode;
  private source?: MediaStreamAudioSourceNode;
  private schedule = 0;
  private recording = false;
  private generationDone = false;
  private assistantTranscript = '';
  private activeSources = new Set<AudioBufferSourceNode>();

  constructor(private roomID: string, private onState: (s: string) => void, private onText: (s: string, who: string) => void, private onError: (s: string) => void) {}

  async connect() {
    if (!window.isSecureContext) throw new Error('麦克风需要 HTTPS 地址，或在本机 localhost 打开');
    if (!navigator.mediaDevices?.getUserMedia) throw new Error('当前浏览器无法使用麦克风，请使用新版 Chrome 或 Edge');
    this.onState('connecting');
    try {
      this.audio = new AudioContext();
      await this.audio.resume();
      this.stream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true, autoGainControl: true } });
      await this.audio.audioWorklet.addModule('/pcm-capture.js');
      this.source = this.audio.createMediaStreamSource(this.stream);
      this.capture = new AudioWorkletNode(this.audio, 'pcm-capture');
      this.capture.port.onmessage = (event: MessageEvent<ArrayBuffer>) => {
        if (this.recording && this.ws?.readyState === WebSocket.OPEN) this.ws.send(event.data);
      };
      this.source.connect(this.capture);
      this.capture.connect(this.audio.destination);

      const ws = new WebSocket(socketURL('/ws/voice?room_id=' + encodeURIComponent(this.roomID)));
      this.ws = ws;
      ws.binaryType = 'arraybuffer';
      await new Promise<void>((resolve, reject) => {
        const timeout = setTimeout(() => reject(new Error('语音连接超时，请稍后重试')), 20000);
        ws.onmessage = event => {
          if (event.data instanceof ArrayBuffer) return;
          let message: RealtimeMessage;
          try { message = JSON.parse(event.data); } catch { return; }
          if (message.type === 'hello') {
            clearTimeout(timeout);
            this.recording = true;
            this.onState('ready');
            resolve();
          } else if (message.type === 'error') {
            clearTimeout(timeout);
            const error = new Error(message.message || '语音连接没有成功');
            reject(error);
            this.onError(error.message);
            this.disconnect();
          } else if (message.type === 'stt') {
            if (message.text) this.onText(message.text, 'you');
          } else if (message.type === 'response.audio.delta' || message.type === 'response.output_audio.delta') {
            this.generationDone = false;
            this.onState('speaking');
            this.playPCM(message.delta, message.sample_rate || 24000);
          } else if (message.type === 'response.audio_transcript.delta' || message.type === 'response.output_audio_transcript.delta' || message.type === 'response.output_text.delta') {
            if (message.delta) { this.assistantTranscript += message.delta; this.onText(this.assistantTranscript, 'linx'); }
          } else if (message.type === 'response.audio_transcript.done' || message.type === 'response.output_audio_transcript.done') {
            if (message.transcript) { this.assistantTranscript = message.transcript; this.onText(this.assistantTranscript, 'linx'); }
          } else if (message.type === 'response.created') {
            this.assistantTranscript = '';
          } else if (message.type === 'input_audio_buffer.speech_started' || message.type === 'speech_started') {
            this.clearPlayback();
            this.generationDone = false;
            this.onState('listening');
          } else if (message.type === 'input_audio_buffer.speech_stopped' || message.type === 'speech_stopped') {
            this.onState('thinking');
          } else if (message.type === 'response.audio.done' || message.type === 'response.output_audio.done') {
            this.generationDone = true;
            this.checkDrain();
          } else if (message.type === 'response.done') {
            this.generationDone = true;
            this.checkDrain();
          }
        };
        ws.onerror = () => { clearTimeout(timeout); reject(new Error('语音连接失败，请稍后重试')); };
        ws.onclose = () => {
          clearTimeout(timeout);
          if (this.ws === ws) {
            this.ws = undefined;
            this.recording = false;
            this.releaseAudio();
            this.onState('disconnected');
          }
        };
      });
    } catch (error) {
      this.disconnect();
      if (error instanceof DOMException && error.name === 'NotAllowedError') throw new Error('麦克风权限未开启，请在浏览器地址栏允许使用麦克风');
      throw error;
    }
  }

  private playPCM(encoded: string, sampleRate: number) {
    const audio = this.audio;
    if (!audio || !encoded) return;
    try {
      const binary = atob(encoded);
      const samples = new Float32Array(Math.floor(binary.length / 2));
      for (let i = 0; i < samples.length; i++) {
        const value = binary.charCodeAt(i * 2) | (binary.charCodeAt(i * 2 + 1) << 8);
        samples[i] = (value & 0x8000 ? value - 0x10000 : value) / 32768;
      }
      if (this.schedule - audio.currentTime > 12) {
        this.onError('语音播放积压，请重新连接');
        this.disconnect();
        return;
      }
      const buffer = audio.createBuffer(1, samples.length, sampleRate);
      buffer.getChannelData(0).set(samples);
      const source = audio.createBufferSource();
      source.buffer = buffer;
      source.connect(audio.destination);
      this.schedule = Math.max(audio.currentTime + .025, this.schedule);
      this.activeSources.add(source);
      source.onended = () => { this.activeSources.delete(source); source.disconnect(); this.checkDrain(); };
      source.start(this.schedule);
      this.schedule += buffer.duration;
    } catch {
      this.onError('返回音频解码失败，请重新连接语音');
      this.disconnect();
    }
  }

  private checkDrain() {
    if (!this.generationDone || this.activeSources.size) return;
    const audio = this.audio;
    if (audio && this.schedule > audio.currentTime + .04) {
      setTimeout(() => this.checkDrain(), Math.max(60, (this.schedule - audio.currentTime) * 1000));
      return;
    }
    this.schedule = 0;
    if (this.recording) this.onState('ready');
  }

  stopPlayback() {
    this.clearPlayback();
    this.generationDone = true;
    this.send({ type: 'abort' });
    if (this.recording) this.onState('ready');
  }

  private send(value: unknown) { if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(JSON.stringify(value)); }

  private clearPlayback() {
    for (const source of this.activeSources) { try { source.stop(); } catch {} }
    this.activeSources.clear();
    this.schedule = 0;
  }

  private releaseAudio() {
    this.clearPlayback();
    this.stream?.getTracks().forEach(track => track.stop()); this.stream = undefined;
    this.capture?.disconnect(); this.capture = undefined;
    this.source?.disconnect(); this.source = undefined;
    void this.audio?.close().catch(() => {}); this.audio = undefined;
  }

  disconnect() {
    this.recording = false;
    const ws = this.ws;
    this.ws = undefined;
    if (ws) {
      ws.onclose = null;
      ws.onerror = null;
      ws.onmessage = null;
      ws.close();
    }
    this.releaseAudio();
    this.onState('disconnected');
  }
}
