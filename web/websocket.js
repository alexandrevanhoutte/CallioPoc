// Upload: 640-byte PCM16/16kHz chunks. Live replies: PCM16/24kHz binary.
// JSON text messages carry provider-independent control events.
const MAX_BUFFERED_BYTES = 32 * 1024;

export class AudioSocket {
  constructor({ endpoint = "/ws/audio", onChunk, onEvent = () => {}, onDisconnect }) {
    this.endpoint = endpoint;
    this.onChunk = onChunk;
    this.onEvent = onEvent;
    this.onDisconnect = onDisconnect;
    this.socket = null;
  }

  connect() {
    if (this.socket) throw new Error("WebSocket is already connected.");
    const scheme = location.protocol === "https:" ? "wss:" : "ws:";
    const socket = new WebSocket(`${scheme}//${location.host}${this.endpoint}`);
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
        if (data instanceof ArrayBuffer) {
          if (data.byteLength === 0 || data.byteLength > 64 * 1024 || data.byteLength % 2 !== 0) {
            this.onDisconnect(new Error("Unexpected PCM16 audio data from server."));
            return;
          }
          // Echo keeps fixed-size chunks; Gemini can return variable-size chunks.
          if (this.endpoint === "/ws/audio" && data.byteLength !== 640) {
            this.onDisconnect(new Error("Unexpected echo audio chunk size."));
            return;
          }
          this.onChunk(data);
        } else if (typeof data === "string") {
          try {
            const event = JSON.parse(data);
            if (typeof event.type !== "string") throw new Error("Missing event type");
            this.onEvent(event);
          } catch {
            this.onDisconnect(new Error("Invalid server event."));
          }
        }
      };
    });
  }

  send(chunk) {
    const socket = this.socket;
    if (!socket || socket.readyState !== WebSocket.OPEN) return false;
    if (!(chunk instanceof Int16Array) || chunk.byteLength !== 640) return false;
    if (socket.bufferedAmount > MAX_BUFFERED_BYTES) return false;
    socket.send(chunk);
    return true;
  }

  sendControl(type) {
    const socket = this.socket;
    if (this.endpoint !== "/ws/live" || !socket || socket.readyState !== WebSocket.OPEN) return false;
    if (!["accept", "decline", "no_answer", "end_call", "playback_finished"].includes(type)) return false;
    socket.send(JSON.stringify({ type }));
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
