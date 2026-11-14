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
