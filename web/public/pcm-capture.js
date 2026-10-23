// Resample the microphone stream and emit 20 ms, mono, signed 16-bit LE PCM.
class PCMCapture extends AudioWorkletProcessor {
  constructor() {
    super();
    this.buffer = [];
    this.cursor = 0;
    this.frame = [];
    this.port.onmessage = () => { this.buffer = []; this.cursor = 0; this.frame = []; };
  }
  process(inputs) {
    const input = inputs[0]?.[0];
    if (!input) return true;
    for (const value of input) this.buffer.push(value);
    const ratio = sampleRate / 16000;
    while (this.cursor + ratio < this.buffer.length) {
      const index = Math.floor(this.cursor), fraction = this.cursor - index;
      const sample = this.buffer[index] * (1 - fraction) + this.buffer[index + 1] * fraction;
      this.frame.push(Math.max(-1, Math.min(1, sample)));
      this.cursor += ratio;
      if (this.frame.length === 320) {
        const result = new ArrayBuffer(640), view = new DataView(result);
        this.frame.forEach((value, i) => view.setInt16(i * 2, Math.round(value * (value < 0 ? 32768 : 32767)), true));
        this.port.postMessage(result, [result]); this.frame = [];
      }
    }
    const consumed = Math.floor(this.cursor);
    this.buffer.splice(0, consumed); this.cursor -= consumed;
    return true;
  }
}
registerProcessor('pcm-capture', PCMCapture);
