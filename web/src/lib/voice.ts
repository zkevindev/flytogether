import { socketURL } from './types';
import type { CompanionPreferences, Room } from './types';

type RealtimeMessage = { type: string; [key: string]: any };

export class VoiceClient {
  private epoch = 0;
  private cancelConnect?: () => void;
  private drainTimer?: ReturnType<typeof setTimeout>;
  private pumpTimer?: ReturnType<typeof setTimeout>;
  private pendingPCM: { samples: Float32Array; rate: number; offset: number }[] = [];
  private pendingSeconds = 0;
  private responseID = '';
  private mutedResponses = new Set<string>();
  private mutedUntilInput = false;
  private ws?: WebSocket;
  private audio?: AudioContext;
  private stream?: MediaStream;
  private capture?: AudioWorkletNode;
  private source?: MediaStreamAudioSourceNode;
  private schedule = 0;
  private recording = false;
  private generationDone = false;
  private assistantTranscript = '';
  private waitingForWake = false;
  private expectHost = false;
  private dropHost = false;
  private hostLive = false;
  private hostResponses = new Set<string>();
  private activeSources = new Set<AudioBufferSourceNode>();

  constructor(private roomID: string, private onState: (s: string) => void, private onText: (s: string, who: string) => void, private onError: (s: string) => void, private companion: CompanionPreferences = {style:'warm',frequency:'normal'}, private onRoom?: (room: Room) => void, private socketPath = '/ws/voice', private readOnly = false) {}

  setCompanion(value: CompanionPreferences) {
    this.companion = value;
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(JSON.stringify({type:'companion_preferences',...value}));
  }

  async connect() {
    if (!window.isSecureContext) throw new Error('麦克风需要 HTTPS 地址，或在本机 localhost 打开');
    if (!navigator.mediaDevices?.getUserMedia) throw new Error('当前浏览器无法使用麦克风，请使用新版 Chrome 或 Edge');
    this.disconnect();
    const epoch = ++this.epoch;
    this.waitingForWake = false; this.mutedUntilInput = false; this.mutedResponses.clear();
    this.expectHost = false; this.dropHost = false; this.hostLive = false; this.hostResponses.clear();
    this.onState('connecting');
    try {
      const audio = this.audio = new AudioContext();
      // Start an unlock attempt during a click, but don't block the microphone
      // permission prompt when an automatic connection has no user gesture.
      void audio.resume().catch(() => {});
      const stream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true, autoGainControl: true } });
      if (epoch !== this.epoch) { stream.getTracks().forEach(track => track.stop()); return; }
      this.stream = stream;
      await new Promise<void>((resolve, reject) => {
        const timeout = setTimeout(() => reject(new Error('浏览器需要一次点击来启用声音，请点击下方的语音按钮')), 2500);
        this.cancelConnect = () => { clearTimeout(timeout); reject(new DOMException('语音连接已取消', 'AbortError')); };
        audio.resume().then(resolve, reject).finally(() => clearTimeout(timeout));
      });
      this.cancelConnect = undefined;
      if (epoch !== this.epoch) return;
      await audio.audioWorklet.addModule('/pcm-capture.js');
      if (epoch !== this.epoch) return;
      audio.onstatechange = () => {
        if (epoch !== this.epoch || this.audio !== audio) return;
        if (audio.state === 'running') this.pumpPlayback();
      };
      this.source = audio.createMediaStreamSource(stream);
      this.capture = new AudioWorkletNode(audio, 'pcm-capture');
      this.capture.port.onmessage = (event: MessageEvent<ArrayBuffer>) => {
        if (epoch === this.epoch && this.recording && this.ws?.readyState === WebSocket.OPEN) this.ws.send(event.data);
      };
      this.source.connect(this.capture);
      this.capture.connect(this.audio.destination);

      const ws = new WebSocket(socketURL(this.socketPath + '?room_id=' + encodeURIComponent(this.roomID) + '&style=' + this.companion.style + '&frequency=' + this.companion.frequency + (this.readOnly && this.socketPath === '/ws/voice' ? '&mode=ask' : '')));
      this.ws = ws;
      ws.binaryType = 'arraybuffer';
      await new Promise<void>((resolve, reject) => {
        let ready = false;
        const timeout = setTimeout(() => reject(new Error('语音连接超时，请稍后重试')), 20000);
        this.cancelConnect = () => { clearTimeout(timeout); reject(new DOMException('语音连接已取消', 'AbortError')); };
        ws.onmessage = event => {
          if (epoch !== this.epoch || this.ws !== ws) return;
          if (event.data instanceof ArrayBuffer) return;
          let message: RealtimeMessage;
          try { message = JSON.parse(event.data); } catch { return; }
          if (message.type === 'hello') {
            ready = true; this.cancelConnect = undefined;
            clearTimeout(timeout);
            this.recording = true;
            this.onState('ready');
            resolve();
          } else if (message.type === 'wake_state') {
            const wasWaiting = this.waitingForWake;
            this.waitingForWake = !!message.wake_enabled && !message.awake;
            if (this.waitingForWake && !this.hostLive) {
              this.clearPlayback(); this.generationDone = true; this.onState('standby');
            } else if (!this.waitingForWake && wasWaiting) this.onState('thinking');
          } else if (message.type === 'game_action') {
            if (message.room?.id !== this.roomID || !message.room?.state?.id) return;
            // Committed actions remain real after audio is interrupted. Let the
            // shared snapshot reducer deduplicate this against the game socket.
            this.onRoom?.(message.room);
            if (!this.mutedUntilInput && typeof message.text === 'string') this.onText(message.text, 'linx');
          } else if (message.type === 'host_cue') {
            this.expectHost = true;
            if (typeof message.text === 'string' && message.text) this.onText(message.text, 'linx');
          } else if (message.type === 'host_handoff') {
            this.expectHost = false; this.hostLive = false; this.hostResponses.clear();
            this.clearPlayback(); this.generationDone = true;
          } else if (message.type === 'host_cancel') {
            this.dropHost = this.expectHost && !this.hostLive;
            this.expectHost = false; this.hostLive = false;
            this.clearPlayback(); this.generationDone = true;
            if (this.responseID) this.mutedResponses.add(this.responseID);
            if (this.recording) this.onState(this.waitingForWake ? 'standby' : 'ready');
          } else if (message.type === 'error') {
            clearTimeout(timeout);
            const error = new Error(message.message || '语音连接没有成功');
            reject(error);
            this.onError(error.message);
            this.disconnect();
          } else if (message.type === 'stt') {
            if (this.waitingForWake) return;
            this.mutedUntilInput = false;
            if (message.text) this.onText(message.text, 'you');
          } else if (message.type === 'response.audio.delta' || message.type === 'response.output_audio.delta') {
            if (this.ignoreResponse(message)) return;
            this.generationDone = false;
            this.onState('speaking');
            this.playPCM(message.delta, message.sample_rate || 24000);
          } else if (message.type === 'response.audio_transcript.delta' || message.type === 'response.output_audio_transcript.delta' || message.type === 'response.output_text.delta') {
            if (this.ignoreResponse(message)) return;
            if (message.delta) { this.assistantTranscript += message.delta; this.onText(this.assistantTranscript, 'linx'); }
          } else if (message.type === 'response.audio_transcript.done' || message.type === 'response.output_audio_transcript.done') {
            if (this.ignoreResponse(message)) return;
            if (message.transcript) { this.assistantTranscript = message.transcript; this.onText(this.assistantTranscript, 'linx'); }
          } else if (message.type === 'response.created') {
            this.responseID = message.response?.id || '';
            if (this.dropHost && this.responseID) { this.mutedResponses.add(this.responseID); this.dropHost = false; this.expectHost = false; }
            if (this.expectHost && this.responseID) {
              this.hostResponses.add(this.responseID); this.hostLive = true; this.expectHost = false;
              if (this.mutedUntilInput) this.mutedResponses.add(this.responseID);
            } else if (this.mutedUntilInput && this.responseID) this.mutedResponses.add(this.responseID);
            this.assistantTranscript = '';
          } else if (message.type === 'input_audio_buffer.speech_started' || message.type === 'speech_started') {
            this.muteResponse();
            this.clearPlayback();
            this.generationDone = false;
            this.onState('listening');
          } else if (message.type === 'input_audio_buffer.speech_stopped' || message.type === 'speech_stopped') {
            this.onState('thinking');
          } else if (message.type === 'response.audio.done' || message.type === 'response.output_audio.done') {
            if (this.ignoreResponse(message)) return;
            this.generationDone = true;
            this.checkDrain();
          } else if (message.type === 'response.done') {
            const id = message.response?.id || message.response_id || this.responseID;
            const host = this.hostResponses.has(id);
            this.hostResponses.delete(id);
            if (host && (this.mutedResponses.has(id) || this.mutedUntilInput)) { this.hostLive = false; return; }
            if (!host && this.ignoreResponse(message)) return;
            this.generationDone = true;
            this.checkDrain();
          }
        };
        ws.onerror = () => {
          clearTimeout(timeout);
          if (epoch !== this.epoch) return;
          const error = new Error('语音连接失败，请点击重新开启语音');
          if (ready) { this.onError(error.message); this.disconnect(); } else reject(error);
        };
        ws.onclose = () => {
          clearTimeout(timeout); this.cancelConnect = undefined;
          if (epoch !== this.epoch) return;
          if (!ready) reject(new Error('语音连接提前断开，请重新开启语音'));
          else this.onError('语音连接已断开，棋局已保留，请重新开启语音');
          if (this.ws === ws) {
            this.ws = undefined;
            this.recording = false;
            this.releaseAudio();
            this.onState('disconnected');
          }
        };
      });
    } catch (error) {
      if (epoch !== this.epoch) return;
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
      // Fast generation is normal: hold PCM in a bounded queue, rather than
      // scheduling the whole reply into the browser's audio timeline.
      const duration = samples.length / sampleRate;
      if (!Number.isFinite(duration) || ![16000, 24000, 48000].includes(sampleRate)) throw new Error('Invalid PCM rate');
      if (this.pendingSeconds + Math.max(0, this.schedule - audio.currentTime) + duration > 90 || this.pendingPCM.length >= 4096) {
        this.stopPlayback();
        this.onError('本次播报过长，已停止。语音仍连接，可以直接说下一句');
        return;
      }
      if (!samples.length) return;
      this.pendingPCM.push({ samples, rate: sampleRate, offset: 0 });
      this.pendingSeconds += duration;
      this.pumpPlayback();
    } catch {
      this.onError('返回音频解码失败，请重新连接语音');
      this.disconnect();
    }
  }

  private pumpPlayback() {
    clearTimeout(this.pumpTimer); this.pumpTimer = undefined;
    const audio = this.audio;
    if (!audio || audio.state !== 'running') return;
    // Keep less than half a second scheduled, splitting large provider chunks.
    // onended drives normal playback; the timer also covers timer throttling.
    while (this.pendingPCM.length && this.schedule < audio.currentTime + .35) {
      const chunk = this.pendingPCM[0];
      const length = Math.min(chunk.samples.length - chunk.offset, Math.max(1, Math.floor(chunk.rate * .1)));
      const buffer = audio.createBuffer(1, length, chunk.rate);
      buffer.getChannelData(0).set(chunk.samples.subarray(chunk.offset, chunk.offset + length));
      chunk.offset += length;
      this.pendingSeconds = Math.max(0, this.pendingSeconds - buffer.duration);
      if (chunk.offset === chunk.samples.length) this.pendingPCM.shift();
      const source = audio.createBufferSource();
      source.buffer = buffer;
      source.connect(audio.destination);
      this.schedule = Math.max(audio.currentTime + .025, this.schedule);
      this.activeSources.add(source);
      source.onended = () => { this.activeSources.delete(source); source.disconnect(); this.pumpPlayback(); this.checkDrain(); };
      source.start(this.schedule);
      this.schedule += buffer.duration;
    }
    if (this.pendingPCM.length) this.pumpTimer = setTimeout(() => this.pumpPlayback(), 50);
  }

  private checkDrain() {
    if (!this.generationDone || this.activeSources.size || this.pendingPCM.length) return;
    const audio = this.audio;
    if (audio && this.schedule > audio.currentTime + .04) {
      clearTimeout(this.drainTimer);
      this.drainTimer = setTimeout(() => this.checkDrain(), Math.max(60, (this.schedule - audio.currentTime) * 1000));
      return;
    }
    this.schedule = 0;
    this.hostLive = false;
    if (this.recording) this.onState(this.waitingForWake ? 'standby' : 'ready');
  }

  stopPlayback() {
    this.muteResponse();
    this.clearPlayback();
    this.generationDone = true;
    this.send({ type: 'abort' });
    if (this.recording) this.onState(this.waitingForWake ? 'standby' : 'ready');
  }

  standby() {
    this.muteResponse();
    this.expectHost = false; this.dropHost = false; this.hostLive = false; this.hostResponses.clear();
    this.waitingForWake = true;
    this.clearPlayback();
    this.generationDone = true;
    this.send({ type: 'sleep' });
    if (this.recording) this.onState('standby');
  }

  private muteResponse() {
    this.mutedUntilInput = true;
    this.hostLive = false; this.expectHost = false;
    if (this.responseID) this.mutedResponses.add(this.responseID);
    if (this.mutedResponses.size > 64) this.mutedResponses.delete(this.mutedResponses.values().next().value!);
  }

  private ignoreResponse(message: RealtimeMessage) {
    const id = message.response_id || message.response?.id || this.responseID;
    if (this.mutedResponses.has(id) || this.mutedUntilInput) return true;
    if (this.hostResponses.has(id)) return false;
    return this.waitingForWake;
  }

  private send(value: unknown) { if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(JSON.stringify(value)); }

  private clearPlayback() {
    clearTimeout(this.drainTimer); this.drainTimer = undefined;
    clearTimeout(this.pumpTimer); this.pumpTimer = undefined;
    this.pendingPCM = []; this.pendingSeconds = 0;
    for (const source of this.activeSources) { source.onended = null; try { source.stop(); } catch {} source.disconnect(); }
    this.activeSources.clear();
    this.schedule = 0;
  }

  private releaseAudio() {
    this.clearPlayback();
    this.stream?.getTracks().forEach(track => track.stop()); this.stream = undefined;
    if (this.capture) { this.capture.port.onmessage = null; this.capture.disconnect(); } this.capture = undefined;
    this.source?.disconnect(); this.source = undefined;
    if (this.audio) this.audio.onstatechange = null;
    void this.audio?.close().catch(() => {}); this.audio = undefined;
  }

  disconnect() {
    this.epoch++;
    this.cancelConnect?.(); this.cancelConnect = undefined;
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
