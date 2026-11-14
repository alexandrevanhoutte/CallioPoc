import { MicrophoneCapture } from "./microphone.js";

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

let microphone = null;

function readableError(error) {
  if (error.name === "NotAllowedError") return "Microphone permission denied.";
  if (error.name === "NotFoundError") return "No microphone found.";
  if (error.name === "NotReadableError") return "Microphone is unavailable or in use.";
  return error.message || "Unknown microphone error.";
}

async function stopMicrophone(error) {
  if (!microphone) return;
  const current = microphone;
  microphone = null;
  stopButton.disabled = true;
  startButton.disabled = true;

  try {
    await current.stop();
  } catch (stopError) {
    error ??= stopError;
  } finally {
    micStatus.textContent = error ? `Error: ${readableError(error)}` : "Stopped";
    signalStatus.textContent = "No active audio input";
    level.value = 0;
    startButton.disabled = false;
  }
}

startButton.addEventListener("click", async () => {
  startButton.disabled = true;
  chunks.value = "0";
  level.value = 0;
  micStatus.textContent = "Requesting microphone permission…";
  signalStatus.textContent = "Waiting for audio chunks…";

  const current = new MicrophoneCapture({
    onAudioChunk: (pcm16, rms) => {
      if (microphone !== current) return;
      chunks.value = String(Number(chunks.value) + 1);
      const db = rms > 0 ? 20 * Math.log10(rms) : -Infinity;
      level.value = Math.max(0, Math.min(100, Math.round((db + 60) * (100 / 54))));
      signalStatus.textContent = rms >= 0.008
        ? "Sound detected"
        : "Silence / low input (chunks arriving)";
      // pcm16 is ready for WebSocket transport in CI-03.
      void pcm16;
    },
    onError: (error) => { void stopMicrophone(error); },
  });

  microphone = current;
  try {
    await current.start();
    if (microphone === current) {
      micStatus.textContent = "Recording";
      stopButton.disabled = false;
    }
  } catch (error) {
    await stopMicrophone(error);
  }
});

stopButton.addEventListener("click", () => { void stopMicrophone(); });
