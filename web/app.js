import { MicrophoneCapture } from "./microphone.js";
import { VoiceActivityGate } from "./voice-activity-gate.js";
import { AudioSocket } from "./websocket.js";
import { AudioPlayer } from "./audio-player.js";
import { TranscriptView } from "./transcript.js";
import { SetupView, selectedMode } from "./setup.js";
import { SummaryView } from "./summary.js";

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
const filtered = document.querySelector("#ws-filtered");
const playbackScheduled = document.querySelector("#playback-scheduled");
const playbackDropped = document.querySelector("#playback-dropped");
const silentChunks = document.querySelector("#ai-silent-chunks");
const interruptions = document.querySelector("#ai-interruptions");
const echo = document.querySelector("#echo-playback");
const micReady = document.querySelector(".mic-ready");
const callStateLabel = document.querySelector("#call-state");
const callFeedback = document.querySelector("#call-feedback");
const summaryPanel = document.querySelector("#call-summary");
const summaryStatus = document.querySelector("#summary-status");
const summaryDetails = document.querySelector("#summary-details");

const summaryView = new SummaryView({
  panel: summaryPanel,
  status: summaryStatus,
  details: summaryDetails,
  caller: document.querySelector("#summary-caller"),
  reason: document.querySelector("#summary-reason"),
  urgency: document.querySelector("#summary-urgency"),
  handoff: document.querySelector("#summary-handoff"),
  text: document.querySelector("#summary-text"),
  actions: document.querySelector("#summary-actions"),
});

const setup = new SetupView({
  status: document.querySelector("#health-status"),
  dot: document.querySelector("#health-dot"),
  retry: document.querySelector("#check-health"),
  selector: modeSelector,
  description: document.querySelector("#mode-help"),
  notice: document.querySelector("#setup-notice"),
  start: startButton,
});
void setup.check();

const handoffControls = document.querySelector("#handoff-controls");
const handoffButtons = [
  ["#call-accept", "accept"],
  ["#call-decline", "decline"],
  ["#call-no-answer", "no_answer"],
].map(([selector, action]) => ({ button: document.querySelector(selector), action }));

function showCallState(current, state, outcome = "") {
  current.callState = state;
  current.pendingAction = false;
  callStateLabel.textContent = state.replaceAll("_", " ");
  handoffControls.hidden = state !== "handoff_required";
  for (const { button } of handoffButtons) button.disabled = state !== "handoff_required";
  if (outcome === "decline") callFeedback.textContent = "Owner declined — Callio can take a message.";
  else if (outcome === "no_answer") callFeedback.textContent = "No answer — Callio can take a message.";
  else if (state === "human_active") callFeedback.textContent = "Human takeover simulated — Gemini stopped. No real phone connection.";
  else if (state === "completed") callFeedback.textContent = `Call ended · ${outcome || "completed"} · microphone and AI off.`;
  else if (state === "handoff_required") callFeedback.textContent = "Waiting for owner response.";
}

function sendHandoffAction(action) {
  const current = session;
  if (!current || !current.live || current.callState !== "handoff_required" || current.pendingAction) return;
  if (!current.socket.sendControl(action)) {
    callFeedback.textContent = "Could not send command.";
    return;
  }
  current.pendingAction = true;
  callFeedback.textContent = "Waiting for confirmation…";
  for (const { button } of handoffButtons) button.disabled = true;
}

for (const { button, action } of handoffButtons) {
  button.addEventListener("click", () => sendHandoffAction(action));
}
const transcript = new TranscriptView(
  document.querySelector("#transcript-list"),
  document.querySelector("#transcript-status"),
);

let session = null;

function readableError(error) {
  if (error?.name === "NotAllowedError") return "Microphone permission denied. Allow microphone access in your browser settings, then try again.";
  if (error?.name === "NotFoundError") return "No microphone found. Connect a microphone and try again.";
  if (error?.name === "NotReadableError") return "Microphone unavailable or in use by another app.";
  return error?.message || "Unknown audio error.";
}

async function stopSession(error) {
  if (!session) return;
  const current = session;
  session = null;
  setup.setActive(true);
  stopButton.disabled = true;
  stopButton.hidden = true;
  modeSelector.disabled = false;
  echo.disabled = true;
  echo.checked = false;
  micReady.classList.remove("is-active");
  current.gate?.reset();
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
    handoffControls.hidden = true;
    if (error) callStateLabel.textContent = "Disconnected";
    else if (current.callState === "completed") callStateLabel.textContent = "Completed · disconnected";
    else callStateLabel.textContent = "Disconnected";
    signalStatus.textContent = "No active audio input";
    level.value = 0;
    startButton.textContent = "Start new call";
    setup.setActive(false);
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
        if (session !== current || current.finishing) return;
        chunks.value = String(Number(chunks.value) + 1);
        const db = rms > 0 ? 20 * Math.log10(rms) : -Infinity;
        level.value = Math.max(0, Math.min(100, Math.round((db + 60) * (100 / 54))));
        signalStatus.textContent = rms >= 0.008
          ? "Sound detected"
          : live ? "Silence / low input (filtered when idle)" : "Silence / low input (chunks arriving)";
        const result = current.gate?.push(pcm16, rms) ?? { frames: [pcm16], end: false };
        if (current.gate) filtered.value = String(current.gate.filtered);
        for (const frame of result.frames) {
          if (socket.send(frame)) {
            sent.value = String(Number(sent.value) + 1);
          } else {
            dropped.value = String(Number(dropped.value) + 1);
          }
        }
        // The audio frames and their final boundary must remain in WS order.
        if (result.end && !socket.sendControl("audio_stream_end")) {
          void stopSession(new Error("Could not finalize microphone audio stream."));
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
    micReady.classList.add("is-active");
    if (!live) aiStatus.textContent = "Echo test";
    if (!live) echo.disabled = false;
  } catch (error) {
    if (session === current) await stopSession(error);
  } finally {
    current.microphoneStarting = false;
  }
}

startButton.addEventListener("click", async () => {
  setup.setActive(true);
  stopButton.hidden = false;
  startButton.textContent = "Start conversation";
  modeSelector.disabled = true;
  const live = selectedMode(modeSelector) === "live";
  micStatus.textContent = "Connecting to Go…";
  socketStatus.textContent = "Connecting…";
  aiStatus.textContent = live ? "Connecting to Gemini…" : "Echo test";
  signalStatus.textContent = "Waiting for audio chunks…";
  level.value = 0;
  transcript.clear();
  summaryView.reset();
  callStateLabel.textContent = live ? "Connecting" : "Echo test";
  callFeedback.textContent = "";
  handoffControls.hidden = true;
  for (const counter of [chunks, sent, received, dropped, filtered, playbackScheduled, playbackDropped, silentChunks, interruptions]) counter.value = "0";

  const current = { socket: null, microphone: null, player: new AudioPlayer(), gate: live ? new VoiceActivityGate() : null, live, microphoneStarting: false, generationFinished: false, callState: "ai_active", pendingAction: false, finishing: false, playbackAcknowledged: false };
  current.player.onIdle = () => {
    if (session !== current) return;
    acknowledgePlayback();
    if (!current.finishing && current.generationFinished && current.callState !== "human_active") {
      aiStatus.textContent = "Listening";
    }
  };
  const acknowledgePlayback = () => {
    if (session !== current || !current.finishing || current.playbackAcknowledged || current.player.sources.size > 0) return;
    current.playbackAcknowledged = true;
    if (!current.socket.sendControl("playback_finished")) {
      void stopSession(new Error("Could not confirm final audio playback"));
    }
  };
  const socket = new AudioSocket({
    endpoint: live ? "/ws/live" : "/ws/audio",
    onChunk: (buffer) => {
      if (session !== current || current.callState === "human_active" || current.callState === "completed") return;
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
      if (event.type === "call_summary" || event.type === "summary_error") {
        summaryView.render(event);
        void stopSession();
        return;
      }
      if (event.type === "call_finishing") {
        current.finishing = true;
        callStateLabel.textContent = "Finishing call…";
        callFeedback.textContent = `Finishing final audio · ${event.reason || "conversation finished"}`;
        stopButton.disabled = true;
        void current.microphone?.stop().catch(error => console.warn("Microphone stop failed", error));
        // Leave queued audio playing. Only acknowledge after all sources finish.
        acknowledgePlayback();
      }
      if (event.type === "call_state") {
        if (!["ai_active", "handoff_required", "human_active", "completed"].includes(event.state)) return;
        showCallState(current, event.state, event.outcome);
        if (event.state === "human_active") {
          current.gate?.reset();
          current.player.clear();
          void current.microphone?.stop().catch((error) => {
            console.warn("Failed to stop microphone after takeover", error);
          });
          current.microphone = null;
          void current.player.disable().catch((error) => {
            console.warn("Failed to disable playback after takeover", error);
          });
          micStatus.textContent = "Microphone stopped — simulated human takeover";
          aiStatus.textContent = "AI stopped";
        }
        if (event.state === "completed") {
          current.finishing = true;
          stopButton.disabled = true;
          summaryView.loading();
          void current.microphone?.stop().catch(error => console.warn("Microphone stop failed", error));
        }
      }
      if (event.type === "call_error") {
        current.pendingAction = false;
        if (event.action === "playback_finished") {
          void stopSession(new Error(event.error || "Completion was rejected"));
          return;
        }
        stopButton.disabled = false;
        callFeedback.textContent = `Command ${event.action || ""} rejected: ${event.error || "Unknown error"}`;
        for (const { button } of handoffButtons) button.disabled = current.callState !== "handoff_required";
      }
      if (event.type === "handoff_required") {
        console.info("Callio handoff_required received", event);
        transcript.addHandoff(event);
      }
      if (event.type === "turn_end" && current.callState !== "human_active") {
        current.generationFinished = true;
        aiStatus.textContent = current.player.sources.size > 0
          ? "Finishing playback…" : "Listening";
      }
      if (event.type === "interrupted" && current.callState !== "human_active") {
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
    if (live) callStateLabel.textContent = "AI active";
    if (live) aiStatus.textContent = "Greeting / listening…";
    // Start microphone immediately for true full duplex barge-in, including
    // during the initial assistant greeting. Gemini VAD handles activity.
    await startMicrophone(current, socket);
  } catch (error) {
    if (session === current) await stopSession(error);
  }
});

stopButton.addEventListener("click", () => {
  if (!session?.live) {
    void stopSession();
    return;
  }
  if (!session.socket.sendControl("end_call")) {
    void stopSession(new Error("Could not send end_call command"));
    return;
  }
  stopButton.disabled = true;
  callFeedback.textContent = "Ending call…";
});

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
