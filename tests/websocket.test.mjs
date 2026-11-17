import assert from "node:assert/strict";
import test from "node:test";
import { AudioSocket } from "../web/websocket.js";

class FakeWebSocket {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSING = 2;
  static CLOSED = 3;
  static instances = [];

  constructor(url) {
    this.url = url;
    this.readyState = FakeWebSocket.CONNECTING;
    this.bufferedAmount = 0;
    this.sent = [];
    FakeWebSocket.instances.push(this);
  }

  open() {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.();
  }

  send(data) { this.sent.push(data); }

  receive(data) { this.onmessage?.({ data }); }

  close() {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.();
  }
}

globalThis.WebSocket = FakeWebSocket;
globalThis.location = { protocol: "http:", host: "localhost:8080" };

test("WebSocket connects, sends bounded PCM16 and receives binary echo", async () => {
  const received = [];
  const connection = new AudioSocket({ onChunk: (chunk) => received.push(chunk), onDisconnect() {} });
  const connected = connection.connect();
  const ws = FakeWebSocket.instances.at(-1);
  assert.equal(ws.url, "ws://localhost:8080/ws/audio");
  assert.equal(ws.binaryType, "arraybuffer");
  ws.open();
  await connected;

  const chunk = new Int16Array(320);
  assert.equal(connection.send(chunk), true);
  assert.equal(ws.sent.length, 1);
  ws.receive(chunk.buffer);
  assert.equal(received.length, 1);
  assert.deepEqual(new Int16Array(received[0]), chunk);

  ws.bufferedAmount = 100_000;
  assert.equal(connection.send(chunk), false, "drop when send queue is backed up");
  assert.equal(ws.sent.length, 1);
  assert.equal(connection.send(new Int16Array(1)), false, "reject bad chunk size");
  connection.close();
  assert.equal(connection.send(chunk), false, "cannot send after stop");
});

test("unexpected disconnect triggers cleanup callback", async () => {
  let calls = 0;
  const connection = new AudioSocket({ onChunk() {}, onDisconnect() { calls++; } });
  const connected = connection.connect();
  const ws = FakeWebSocket.instances.at(-1);
  ws.open();
  await connected;
  ws.close();
  assert.equal(calls, 1);
});

test("invalid echoed data triggers error callback", async () => {
  let message = "";
  const connection = new AudioSocket({ onChunk() {}, onDisconnect(error) { message = error.message; } });
  const connected = connection.connect();
  const ws = FakeWebSocket.instances.at(-1);
  ws.open();
  await connected;
  ws.receive(new ArrayBuffer(7));
  assert.match(message, /Unexpected .*audio data/);
  connection.close();
});

test("live WebSocket sends only allowed lifecycle commands", async () => {
  const connection = new AudioSocket({
    endpoint: "/ws/live",
    onChunk() {},
    onDisconnect() {},
  });
  const connected = connection.connect();
  const ws = FakeWebSocket.instances.at(-1);
  ws.open();
  await connected;
  for (const type of ["accept", "decline", "no_answer", "end_call", "playback_finished", "audio_stream_end"]) {
    assert.equal(connection.sendControl(type), true);
    assert.deepEqual(JSON.parse(ws.sent.at(-1)), { type });
  }
  const count = ws.sent.length;
  assert.equal(connection.sendControl("invalid"), false);
  assert.equal(ws.sent.length, count);
  connection.close();
  assert.equal(connection.sendControl("end_call"), false);
  const echo = new AudioSocket({ onChunk() {}, onDisconnect() {} });
  assert.equal(echo.sendControl("accept"), false);
  assert.equal(echo.sendControl("audio_stream_end"), false);
});


test("traffic hooks expose only message metadata without affecting streaming", async () => {
  const traffic = [];
  const connection = new AudioSocket({
    endpoint: "/ws/live", onChunk() {}, onEvent() {}, onDisconnect() {},
    onTraffic: event => traffic.push(event),
  });
  const connected = connection.connect();
  const ws = FakeWebSocket.instances.at(-1);
  ws.open();
  await connected;

  assert.equal(connection.send(new Int16Array(320)), true);
  assert.equal(connection.sendControl("end_call"), true);
  ws.receive(new ArrayBuffer(960));
  ws.receive(JSON.stringify({ type: "transcript", text: "private caller message" }));
  assert.deepEqual(traffic.map(({ direction, kind, type, size }) => ({ direction, kind, type, size })), [
    { direction: "out", kind: "binary", type: undefined, size: 640 },
    { direction: "out", kind: "json", type: "end_call", size: 19 },
    { direction: "in", kind: "binary", type: undefined, size: 960 },
    { direction: "in", kind: "json", type: "transcript", size: new TextEncoder().encode(JSON.stringify({ type: "transcript", text: "private caller message" })).byteLength },
  ]);
  assert.ok(traffic.every(event => typeof event.timestamp === "number"));
  assert.doesNotMatch(JSON.stringify(traffic), /private caller message/);
  connection.close();
});

test("a failing diagnostic callback cannot interrupt the audio socket", async () => {
  const connection = new AudioSocket({
    onChunk() {}, onDisconnect() {}, onTraffic() { throw new Error("inspector failed"); },
  });
  const connected = connection.connect();
  const ws = FakeWebSocket.instances.at(-1);
  ws.open();
  await connected;
  assert.equal(connection.send(new Int16Array(320)), true);
  ws.receive(new ArrayBuffer(640));
  connection.close();
});

test("per-call locale is only added to live URL, without changing control frames", async () => {
  const connection = new AudioSocket({
    endpoint: "/ws/live", locale: "ko-KR", onChunk() {}, onDisconnect() {},
  });
  const connected = connection.connect();
  const ws = FakeWebSocket.instances.at(-1);
  assert.equal(ws.url, "ws://localhost:8080/ws/live?locale=ko-KR");
  ws.open();
  await connected;
  assert.equal(connection.sendControl("end_call"), true);
  assert.deepEqual(JSON.parse(ws.sent.at(-1)), { type: "end_call" });
  connection.close();

  const echo = new AudioSocket({ endpoint: "/ws/audio", locale: "fr-FR", onChunk() {}, onDisconnect() {} });
  const next = echo.connect();
  const echoSocket = FakeWebSocket.instances.at(-1);
  assert.equal(echoSocket.url, "ws://localhost:8080/ws/audio");
  echoSocket.open();
  await next;
  echo.close();
});
