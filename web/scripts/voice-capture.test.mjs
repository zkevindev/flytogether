import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';
import vm from 'node:vm';

const registered = new Map();
class WorkletBase {
  constructor() {
    this.port = {
      messages: [],
      postMessage(message) { this.messages.push(message.slice(0)); },
    };
  }
}
const context = vm.createContext({
  AudioWorkletProcessor: WorkletBase,
  DataView,
  ArrayBuffer,
  Math,
  sampleRate: 48000,
  registerProcessor(name, processor) { registered.set(name, processor); },
});
vm.runInContext(await readFile(new URL('../public/pcm-capture.js', import.meta.url), 'utf8'), context);
const Processor = registered.get('pcm-capture');
assert.ok(Processor, 'the shipped microphone worklet should register');

function inputSamples(kind, count = 1923) {
  let seed = 0x12345678;
  const random = () => {
    seed = (1664525 * seed + 1013904223) >>> 0;
    return seed / 0x100000000 * 2 - 1;
  };
  return Float32Array.from({ length: count }, (_, i) => {
    const speech = 0.24 * Math.sin(2 * Math.PI * 440 * i / 48000);
    const noise = kind === 'noise-10db' ? 0.076 * random() : kind === 'noise-0db' ? 0.24 * random() : 0;
    const echo = kind === 'echo-50ms' && i >= 2400
      ? 0.12 * Math.sin(2 * Math.PI * 440 * (i - 2400) / 48000)
      : 0;
    const overload = kind === 'overload' ? 1.7 * speech + 0.6 * random() : 0;
    return speech + noise + echo + overload;
  });
}

function processChunks(processor, samples) {
  for (let offset = 0; offset < samples.length; offset += 128) {
    processor.process([[samples.subarray(offset, Math.min(offset + 128, samples.length))]]);
  }
}

for (const kind of ['clean', 'noise-10db', 'noise-0db', 'echo-50ms', 'overload']) {
  test(`capture worklet emits bounded 20 ms PCM frames for ${kind} input (simulation only)`, () => {
    const processor = new Processor();
    processChunks(processor, inputSamples(kind));
    const chunks = processor.port.messages;
    assert.equal(chunks.length, 2, '48 kHz input is resampled into 16 kHz frames');
    for (const chunk of chunks) {
      assert.equal(chunk.byteLength, 640, 'each frame contains 320 signed 16-bit mono samples');
      const view = new DataView(chunk);
      for (let i = 0; i < 320; i++) {
        const value = view.getInt16(i * 2, true);
        assert.ok(value >= -32768 && value <= 32767);
      }
    }
  });
}

test('capture worklet clears partial PCM when told to reset between sessions', () => {
  const processor = new Processor();
  processChunks(processor, inputSamples('clean', 384));
  const before = processor.port.messages.length;
  processor.port.onmessage();
  processChunks(processor, inputSamples('clean', 1923));
  assert.equal(processor.port.messages.length - before, 2);
});
