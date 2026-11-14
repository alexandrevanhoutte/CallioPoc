// Node's built-in test runner; no browser microphone or dependencies required.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");

function createProcessor(inputRate) {
  const messages = [];
  let Processor;

  class FakeAudioWorkletProcessor {
    constructor() {
      this.port = {
        postMessage(message) {
          messages.push(message);
        },
      };
    }
  }

  const source = fs.readFileSync(
    path.join(__dirname, "../web/audio-processor.js"), "utf8",
  );

  vm.runInNewContext(source, {
    AudioWorkletProcessor: FakeAudioWorkletProcessor,
    Int16Array,
    Math,
    sampleRate: inputRate,
    registerProcessor(name, implementation) {
      assert.equal(name, "pcm16-recorder");
      Processor = implementation;
    },
  });

  return { processor: new Processor(), messages };
}

function feed(processor, sampleRate, amplitude) {
  const frameSize = 128;
  for (let offset = 0; offset < sampleRate; offset += frameSize) {
    const count = Math.min(frameSize, sampleRate - offset);
    const input = new Float32Array(count).fill(amplitude);
    const output = new Float32Array(count);
    assert.equal(processor.process([[input]], [[output]]), true);
    assert.ok(output.every((sample) => sample === 0), "no microphone echo");
  }
}

for (const inputRate of [16000, 44100, 48000]) {
  test(`PCM16 16 kHz chunks from ${inputRate} Hz`, () => {
    const { processor, messages } = createProcessor(inputRate);
    feed(processor, inputRate, 0.25);

    assert.equal(messages.length, 50, "one second makes fifty 20 ms chunks");
    for (const chunk of messages) {
      assert.equal(chunk.type, "audio");
      assert.equal(chunk.buffer.byteLength, 640, "320 samples * 2 bytes");
      assert.equal(new Int16Array(chunk.buffer).length, 320);
      assert.ok(Math.abs(chunk.rms - 0.25) < 0.001);
      assert.ok(
        new Int16Array(chunk.buffer).every((sample) => Math.abs(sample - 8192) <= 1),
      );
    }
  });
}

test("silent microphone still produces chunks with zero level", () => {
  const { processor, messages } = createProcessor(48000);
  feed(processor, 48000, 0);
  assert.equal(messages.length, 50);
  assert.ok(messages.every((chunk) => chunk.rms === 0));
});
