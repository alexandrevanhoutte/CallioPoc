import assert from "node:assert/strict";
import test from "node:test";
import { VoiceActivityGate } from "../web/voice-activity-gate.js";

const frame = (id = 0) => new Int16Array(320).fill(id);
const quiet = 0.001;
const speech = 0.02;

test("idle silence is filtered without end events or uploading PCM", () => {
  const gate = new VoiceActivityGate();
  let uploaded = 0;
  let ends = 0;
  for (let i = 0; i < 250; i++) {
    const result = gate.push(frame(i), quiet);
    uploaded += result.frames.length;
    ends += Number(result.end);
  }
  assert.equal(uploaded, 0);
  assert.equal(ends, 0);
  assert.equal(gate.processed, 250);
  assert.equal(gate.filtered, 240);
  assert.equal(gate.state, "idle");
});

test("200 ms pre-roll preserves a new utterance onset in order", () => {
  const gate = new VoiceActivityGate();
  for (let i = 0; i < 12; i++) gate.push(frame(i), quiet);
  assert.equal(gate.push(frame(12), speech).frames.length, 0);
  const started = gate.push(frame(13), speech);
  assert.equal(gate.state, "speaking");
  assert.equal(started.end, false);
  assert.deepEqual(started.frames.map(chunk => chunk[0]), [4, 5, 6, 7, 8, 9, 10, 11, 12, 13]);
  assert.equal(gate.filtered, 4);
  assert.deepEqual(gate.push(frame(14), speech).frames.map(x => x[0]), [14]);
});

test("short pauses and quieter speech do not split a turn", () => {
  const gate = new VoiceActivityGate();
  gate.push(frame(1), speech);
  gate.push(frame(2), speech);
  for (let i = 0; i < 30; i++) {
    assert.equal(gate.push(frame(3), quiet).end, false);
  }
  assert.equal(gate.push(frame(4), 0.006).end, false);
  for (let i = 0; i < 34; i++) {
    assert.equal(gate.push(frame(5), quiet).end, false);
  }
  assert.equal(gate.state, "speaking");
  const last = gate.push(frame(6), quiet);
  assert.equal(last.end, true);
  assert.deepEqual(last.frames.map(x => x[0]), [6]);
  assert.equal(gate.state, "idle");
});

test("each speech burst emits exactly one end event, then restarts", () => {
  const gate = new VoiceActivityGate();
  let ends = 0;
  let uploaded = 0;
  for (const rms of [
    speech, speech, ...Array(35).fill(quiet),
    ...Array(100).fill(quiet),
    speech, speech, ...Array(35).fill(quiet),
    ...Array(100).fill(quiet),
  ]) {
    const result = gate.push(frame(), rms);
    uploaded += result.frames.length;
    ends += Number(result.end);
  }
  assert.equal(ends, 2);
  assert.ok(uploaded > 70);
  assert.ok(gate.filtered >= 170);
});

test("reset discards pre-roll and pending activity without end signal", () => {
  const gate = new VoiceActivityGate();
  gate.push(frame(), speech);
  gate.push(frame(), speech);
  for (let i = 0; i < 10; i++) gate.push(frame(), quiet);
  gate.reset();
  assert.equal(gate.state, "idle");
  assert.equal(gate.processed, 0);
  assert.equal(gate.filtered, 0);
  assert.equal(gate.push(frame(), quiet).end, false);
  assert.equal(gate.push(frame(), quiet).frames.length, 0);
});
