import { MicrophoneCapture } from "./microphone.js";
import { AudioSocket } from "./websocket.js";
import { AudioPlayer } from "./audio-player.js";
import { TranscriptView } from "./transcript.js";

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

const modeSelector = document.querySelector("#stream-mode");
const startButton = document.querySelector("#mic-start");
const stopButton = document.querySelector("#mic-stop");
const micStatus = document.querySelector("#mic-status");
const signalStatus = document.querySelector("#mic-signal");
const level = document.querySelector("#mic-level");
const chunks = document.querySelector("#mic-chunks");
const socketStatus = document.querySelector("#ws-status");
const aiStatus = document.querySelector("#ai-status");
const sent = document.querySelector("#ws-sent");
const received = document.querySelector("#ws-received");
const dropped = document.querySelector("#ws-dropped");
const playbackScheduled = document.querySelector("#playback-scheduled");
const playbackDropped = document.querySelector("#playback-dropped");
const silentChunks = document.querySelector("#ai-silent-chunks");
const interruptions = document.querySelector("#ai-interruptions");
const echo = document.querySelector("#echo-playback");
const handoffSection = document.querySelector("#handoff-section");
const handoffReason = document.querySelector("#handoff-reason");
const handoffCaller = document.querySelector("#handoff-caller");
const handoffPurpose = document.querySelector("#handoff-purpose");
const handoffSummary = document.querySelector("#handoff-summary");
const handoffContact = document.querySelector("#handoff-contact");
const transcript = new TranscriptView(
  document.querySelector("#transcript-list"),
  document.querySelector("#transcript-status"),
);

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
  modeSelector.disabled = false;
  echo.disabled = true;
  echo.checked = false;
  try {
    // Always attempt all cleanup operations, even if one fails.
    try { current.socket.close(); } catch (e) { error ??= e; }
    try { await current.microphone?.stop(); } catch (e) { error ??= e; }
    try { await current.player.disable(); } catch (e) { error ??= e; }
  } finally {
    micStatus.textContent = error ? `Error: ${readableError(error)}` : "Stopped";
    socketStatus.textContent = error ? "Disconnected (error)" : "Disconnected";
    aiStatus.textContent = error ? readableError(error) : "Idle";
    transcript.finish();
    signalStatus.textContent = "No active audio input";
    level.value = 0;
    startButton.disabled = false;
  }
}

async function startMicrophone(current, socket) {
  if (session !== current || current.microphone || current.microphoneStarting) return;
  current.microphoneStarting = true;
  const live = current.live;
  try {
    micStatus.textContent = "Requesting microphone permission…";
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
    await microphone.start();
    if (session !== current) {
      await microphone.stop();
      return;
    }
    micStatus.textContent = "Recording";
    if (!live) aiStatus.textContent = "Echo test";
    if (!live) echo.disabled = false;
  } catch (error) {
    if (session === current) await stopSession(error);
  } finally {
    current.microphoneStarting = false;
  }
}

startButton.addEventListener("click", async () => {
  startButton.disabled = true;
  modeSelector.disabled = true;
  const live = modeSelector.value === "live";
  micStatus.textContent = "Connecting to Go…";
  socketStatus.textContent = "Connecting…";
  aiStatus.textContent = live ? "Connecting to Gemini…" : "Echo test";
  signalStatus.textContent = "Waiting for audio chunks…";
  level.value = 0;
  transcript.clear();
  handoffSection.hidden = true;
  for (const counter of [chunks, sent, received, dropped, playbackScheduled, playbackDropped, silentChunks, interruptions]) counter.value = "0";

  const current = { socket: null, microphone: null, player: new AudioPlayer(), live, microphoneStarting: false, generationFinished: false };
  current.player.onIdle = () => {
    if (session !== current || !current.generationFinished) return;
    aiStatus.textContent = "Listening";
  };
  const socket = new AudioSocket({
    endpoint: live ? "/ws/live" : "/ws/audio",
    onChunk: (buffer) => {
      if (session !== current) return;
      received.value = String(Number(received.value) + 1);
      if (live && new Uint8Array(buffer).every((byte) => byte === 0)) {
        silentChunks.value = String(Number(silentChunks.value) + 1);
      }
      if (live || echo.checked) {
        if (current.player.play(buffer, live ? 24000 : 16000)) {
          playbackScheduled.value = String(Number(playbackScheduled.value) + 1);
        } else {
          playbackDropped.value = String(Number(playbackDropped.value) + 1);
          if (Number(playbackDropped.value) === 1) {
            console.warn("Audio playback rejected a PCM16 chunk", {
              contextState: current.player.context?.state,
              queuedSeconds: current.player.context
                ? Math.max(0, current.player.nextStart - current.player.context.currentTime)
                : null,
            });
          }
        }
      }
      if (live) {
        current.generationFinished = false;
        aiStatus.textContent = "AI speaking";
      }
    },
    onEvent: (event) => {
      if (session !== current) return;
      if (event.type === "transcript") transcript.update(event);
      if (event.type === "handoff_required") {
        console.info("Callio handoff_required received", event);
        const details = event.context || {};
        handoffReason.textContent = `Reason: ${event.reason || ""}`;
        handoffCaller.textContent = `Caller: ${details.caller_name || "Not provided"}`;
        handoffPurpose.textContent = `Purpose: ${details.purpose || ""}`;
        handoffSummary.textContent = `Summary: ${details.summary || ""}`;
        handoffContact.textContent = [details.company, details.email, details.phone_number].filter(Boolean).join(" · ");
        handoffSection.hidden = false;
      }
      if (event.type === "turn_end") {
        current.generationFinished = true;
        aiStatus.textContent = current.player.sources.size > 0
          ? "Finishing playback…" : "Listening";
      }
      if (event.type === "interrupted") {
        interruptions.value = String(Number(interruptions.value) + 1);
        current.generationFinished = false;
        current.player.clear();
        aiStatus.textContent = "Interrupted — listening";
      }
    },
    onDisconnect: (error) => {
      if (session === current) void stopSession(error);
    },
  });
  current.socket = socket;
  session = current;

  try {
    // User-gesture activation is required by browser audio autoplay rules.
    if (live) await current.player.enable();
    if (session !== current) return;
    await socket.connect();
    if (session !== current) return;
    socketStatus.textContent = "Connected";

    stopButton.disabled = false;
    if (live) aiStatus.textContent = "Greeting / listening…";
    // Start microphone immediately for true full duplex barge-in, including
    // during the initial assistant greeting. Gemini VAD handles activity.
    await startMicrophone(current, socket);
  } catch (error) {
    if (session === current) await stopSession(error);
  }
});

stopButton.addEventListener("click", () => { void stopSession(); });

echo.addEventListener("change", () => {
  if (!session || session.live) return;
  if (echo.checked) {
    void session.player.enable().catch((error) => {
      echo.checked = false;
      socketStatus.textContent = `Playback error: ${readableError(error)}`;
    });
  } else {
    void session.player.disable();
  }
});
