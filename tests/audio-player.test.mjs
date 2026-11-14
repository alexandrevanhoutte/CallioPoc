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
  createBuffer() {
    const samples = new Float32Array(320);
    return { duration: 0.02, getChannelData() { return samples; }, samples };
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
  const player = new AudioPlayer();
  const data = new ArrayBuffer(640);
  const view = new DataView(data);
  view.setInt16(0, 16384, true);

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

test("output queue rejects audio when already 250 ms ahead", async () => {
  const player = new AudioPlayer();
  await player.enable();
  player.nextStart = player.context.currentTime + 1;
  assert.equal(player.play(new ArrayBuffer(640)), false);
  await player.disable();
});
