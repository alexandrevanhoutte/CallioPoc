// Each WebSocket binary message carries one 640-byte (20 ms) PCM16 chunk.
const MAX_BUFFERED_BYTES = 32 * 1024;

export class AudioSocket {
  constructor({ onChunk, onDisconnect }) {
    this.onChunk = onChunk;
    this.onDisconnect = onDisconnect;
    this.socket = null;
  }

  connect() {
    if (this.socket) throw new Error("WebSocket is already connected.");
    const scheme = location.protocol === "https:" ? "wss:" : "ws:";
    const socket = new WebSocket(`${scheme}//${location.host}/ws/audio`);
    this.socket = socket;
    socket.binaryType = "arraybuffer";

    return new Promise((resolve, reject) => {
      let opened = false;
      const timeout = setTimeout(() => {
        if (socket.readyState !== WebSocket.OPEN) {
          socket.close();
          reject(new Error("WebSocket connection timed out."));
        }
      }, 5000);

      socket.onopen = () => {
        opened = true;
        clearTimeout(timeout);
        resolve();
      };
      socket.onerror = () => {
        // Browsers intentionally do not expose specific WebSocket errors.
        if (socket.readyState !== WebSocket.OPEN) {
          clearTimeout(timeout);
          reject(new Error("Could not connect to the Go WebSocket server."));
        }
      };
      socket.onclose = () => {
        clearTimeout(timeout);
        if (this.socket !== socket) return;
        this.socket = null;
        if (!opened) {
          reject(new Error("WebSocket closed before connecting."));
          return;
        }
        this.onDisconnect(new Error("WebSocket connection closed."));
      };
      socket.onmessage = ({ data }) => {
        if (!(data instanceof ArrayBuffer) || data.byteLength !== 640) {
          this.onDisconnect(new Error("Unexpected audio data from server."));
          return;
        }
        this.onChunk(data);
      };
    });
  }

  send(chunk) {
    const socket = this.socket;
    if (!socket || socket.readyState !== WebSocket.OPEN) return false;
    if (!(chunk instanceof Int16Array) || chunk.byteLength !== 640) return false;
    if (socket.bufferedAmount > MAX_BUFFERED_BYTES) return false; // Drop rather than accumulate latency.
    socket.send(chunk);
    return true;
  }

  close() {
    const socket = this.socket;
    this.socket = null;
    if (socket && socket.readyState !== WebSocket.CLOSED) {
      socket.onclose = null;
      socket.onmessage = null;
      socket.onerror = null;
      socket.close(1000, "Stopped");
    }
  }
}
