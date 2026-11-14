import assert from "node:assert/strict";
import test from "node:test";
import { AudioPlayer } from "../web/audio-player.js";

const starts = [];
class FakeAudioContext {
  constructor() {
    this.state = "suspended";
    this.currentTime = 1;
    this.destination = {};
  }
  async resume() { this.state = "running"; }
  async close() { this.state = "closed"; }
  createBuffer(channels, length, rate) {
    const samples = new Float32Array(length);
    return { duration: length / rate, getChannelData() { return samples; } };
  }
  createBufferSource() {
    return {
      connect() {}, disconnect() {},
      start(time) { starts.push(time); },
      stop() {},
    };
  }
}

globalThis.AudioContext = FakeAudioContext;

test("PCM16 echo playback schedules chunks and releases sources on stop", async () => {
  starts.length = 0;
  const player = new AudioPlayer();
  const data = new ArrayBuffer(640);
  new DataView(data).setInt16(0, 16384, true);
  assert.equal(player.play(data), false, "disabled by default");
  await player.enable();
  assert.equal(player.play(data), true);
  assert.equal(player.play(data), true);
  assert.equal(starts.length, 2);
  assert.ok(starts[1] > starts[0]);
  assert.equal(player.sources.size, 2);
  await player.disable();
  assert.equal(player.sources.size, 0);
  assert.equal(player.context, null);
});

test("24 kHz model playback accepts variable-sized PCM16 chunks", async () => {
  const player = new AudioPlayer();
  await player.enable();
  assert.equal(player.play(new ArrayBuffer(960), 24000), true);
  assert.equal(player.play(new ArrayBuffer(320), 24000), true);
  assert.equal(player.play(new ArrayBuffer(3), 24000), false);
  await player.disable();
});

test("a fast 3-second Gemini audio burst does not drop any 20 ms chunks", async () => {
  const player = new AudioPlayer();
  await player.enable();
  for (let i = 0; i < 150; i++) {
    assert.equal(player.play(new ArrayBuffer(960), 24000), true, `chunk ${i} was lost`);
  }
  assert.equal(player.sources.size, 150);
  assert.ok(Math.abs(player.nextStart - player.context.currentTime - 3.04) < 0.001);
  await player.disable();
});

test("playback remains bounded for unusually large audio bursts", async () => {
  const player = new AudioPlayer();
  await player.enable();
  let accepted = 0;
  let dropped = 0;
  for (let i = 0; i < 1600; i++) {
    if (player.play(new ArrayBuffer(960), 24000)) accepted++;
    else dropped++;
  }
  assert.ok(accepted > 950 && accepted <= 1000, `accepted: ${accepted}`);
  assert.ok(dropped > 500, `dropped: ${dropped}`);
  await player.disable();
});

test("interruption flushes the old playback queue", async () => {
  const player = new AudioPlayer();
  await player.enable();
  for (let i = 0; i < 100; i++) {
    assert.equal(player.play(new ArrayBuffer(960), 24000), true);
  }
  player.clear();
  assert.equal(player.sources.size, 0);
  assert.equal(player.nextStart, player.context.currentTime);
  assert.equal(player.play(new ArrayBuffer(960), 24000), true);
  await player.disable();
});

test("audio idle callback fires only after the final scheduled source finishes", async () => {
  const player = new AudioPlayer();
  let idle = 0;
  player.onIdle = () => { idle++; };
  await player.enable();
  assert.equal(player.play(new ArrayBuffer(960), 24000), true);
  assert.equal(player.play(new ArrayBuffer(960), 24000), true);
  const sources = Array.from(player.sources);
  sources[0].onended();
  assert.equal(idle, 0);
  sources[1].onended();
  assert.equal(idle, 1);
  await player.disable();
  assert.equal(idle, 1, "cleanup does not trigger another idle callback");
});
