import { MicrophoneCapture } from "./microphone.js";
import { AudioSocket } from "./websocket.js";
import { AudioPlayer } from "./audio-player.js";

const healthButton = document.querySelector("#check-health");
const healthStatus = document.querySelector("#health-status");

healthButton.addEventListener("click", async () => {
  healthButton.disabled = true;
  healthStatus.textContent = "Checking…";
  try {
    const response = await fetch("/health");
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    const body = await response.json();
    if (body.status !== "ok") throw new Error("Unexpected health response");
    healthStatus.textContent = "Connected";
  } catch (error) {
    healthStatus.textContent = `Connection failed: ${error.message}`;
  } finally {
    healthButton.disabled = false;
  }
});

const startButton = document.querySelector("#mic-start");
const stopButton = document.querySelector("#mic-stop");
const micStatus = document.querySelector("#mic-status");
const signalStatus = document.querySelector("#mic-signal");
const level = document.querySelector("#mic-level");
const chunks = document.querySelector("#mic-chunks");
const socketStatus = document.querySelector("#ws-status");
const sent = document.querySelector("#ws-sent");
const received = document.querySelector("#ws-received");
const dropped = document.querySelector("#ws-dropped");
const echo = document.querySelector("#echo-playback");

let session = null;

function readableError(error) {
  if (error?.name === "NotAllowedError") return "Microphone permission denied.";
  if (error?.name === "NotFoundError") return "No microphone found.";
  if (error?.name === "NotReadableError") return "Microphone unavailable or in use.";
  return error?.message || "Unknown audio error.";
}

async function stopSession(error) {
  if (!session) return;
  const current = session;
  session = null;
  startButton.disabled = true;
  stopButton.disabled = true;
  echo.disabled = true;
  echo.checked = false;
  try {
    // Always release all resources, even if one cleanup fails.
    try { await current.microphone?.stop(); } catch (e) { error ??= e; }
    try { current.socket.close(); } catch (e) { error ??= e; }
    try { await current.player.disable(); } catch (e) { error ??= e; }
  } finally {
    micStatus.textContent = error ? `Error: ${readableError(error)}` : "Stopped";
    socketStatus.textContent = error ? "Disconnected (error)" : "Disconnected";
    signalStatus.textContent = "No active audio input";
    level.value = 0;
    startButton.disabled = false;
  }
}

startButton.addEventListener("click", async () => {
  startButton.disabled = true;
  micStatus.textContent = "Connecting to Go…";
  socketStatus.textContent = "Connecting…";
  signalStatus.textContent = "Waiting for audio chunks…";
  level.value = 0;
  for (const counter of [chunks, sent, received, dropped]) counter.value = "0";

  const current = { socket: null, microphone: null, player: new AudioPlayer() };
  const socket = new AudioSocket({
    onChunk: (buffer) => {
      if (session !== current) return;
      received.value = String(Number(received.value) + 1);
      if (echo.checked) current.player.play(buffer);
    },
    onDisconnect: (error) => {
      if (session === current) void stopSession(error);
    },
  });
  current.socket = socket;
  session = current;

  try {
    await socket.connect();
    if (session !== current) return;
    socketStatus.textContent = "Connected";

    const microphone = new MicrophoneCapture({
      onAudioChunk: (pcm16, rms) => {
        if (session !== current) return;
        chunks.value = String(Number(chunks.value) + 1);
        const db = rms > 0 ? 20 * Math.log10(rms) : -Infinity;
        level.value = Math.max(0, Math.min(100, Math.round((db + 60) * (100 / 54))));
        signalStatus.textContent = rms >= 0.008
          ? "Sound detected" : "Silence / low input (chunks arriving)";
        if (socket.send(pcm16)) {
          sent.value = String(Number(sent.value) + 1);
        } else {
          dropped.value = String(Number(dropped.value) + 1);
        }
      },
      onError: (error) => { if (session === current) void stopSession(error); },
    });
    current.microphone = microphone;
    micStatus.textContent = "Requesting microphone permission…";
    await microphone.start();
    if (session !== current) {
      await microphone.stop(); // Startup finished after a disconnect.
      return;
    }
    micStatus.textContent = "Recording";
    stopButton.disabled = false;
    echo.disabled = false;
  } catch (error) {
    if (session === current) await stopSession(error);
  }
});

stopButton.addEventListener("click", () => { void stopSession(); });

echo.addEventListener("change", () => {
  if (!session) return;
  if (echo.checked) {
    void session.player.enable().catch((error) => {
      echo.checked = false;
      socketStatus.textContent = `Playback error: ${readableError(error)}`;
    });
  } else {
    void session.player.disable();
  }
});
