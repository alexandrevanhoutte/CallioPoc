import assert from "node:assert/strict";
import test from "node:test";
import { AudioSocket } from "../web/websocket.js";

class FakeSocket {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSED = 3;
  static instances = [];
  constructor(url) {
    this.url = url;
    this.readyState = 0;
    this.bufferedAmount = 0;
    FakeSocket.instances.push(this);
  }
  open() { this.readyState = 1; this.onopen?.(); }
  send() {}
  close() { this.readyState = 3; this.onclose?.(); }
  receive(data) { this.onmessage?.({ data }); }
}
globalThis.WebSocket = FakeSocket;
globalThis.location = { protocol: "http:", host: "localhost:8080" };

test("live WebSocket accepts PCM16 24 kHz responses and JSON events", async () => {
  const audio = [];
  const events = [];
  const client = new AudioSocket({
    endpoint: "/ws/live",
    onChunk: (chunk) => audio.push(chunk),
    onEvent: (event) => events.push(event),
    onDisconnect: () => {},
  });
  const ready = client.connect();
  const ws = FakeSocket.instances.at(-1);
  assert.equal(ws.url, "ws://localhost:8080/ws/live");
  ws.open();
  await ready;
  ws.receive(new ArrayBuffer(960));
  ws.receive(new ArrayBuffer(320));
  ws.receive(JSON.stringify({ type: "turn_end", text: "" }));
  assert.equal(audio.length, 2);
  assert.deepEqual(events, [{ type: "turn_end", text: "" }]);
  client.close();
});
