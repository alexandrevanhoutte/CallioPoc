import { getLocale, setLocale, hasStoredLocale, SUPPORTED_LOCALES, t, localizePage } from "./i18n.js";
import { MicrophoneCapture } from "./microphone.js";
import { VoiceActivityGate } from "./voice-activity-gate.js";
import { AudioSocket } from "./websocket.js";
import { AudioPlayer } from "./audio-player.js";
// Versioned import avoids mixing cached frontend modules after updates.
import { TranscriptView } from "./transcript.js?v=ci-18";
import { SetupView, selectedMode } from "./setup.js";
import { SummaryView } from "./summary.js";
import { EventInspector } from "./event-inspector.js";
import { MetricTooltips } from "./metric-tooltip.js";

const modeSelector = document.querySelector("#stream-mode");
const languageSelect = document.querySelector("#language-select");
languageSelect.value = getLocale();
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
const startButtonLabel = startButton.querySelector("span");
new MetricTooltips({
  triggers: document.querySelectorAll(".metric-help-trigger"),
  tooltip: document.querySelector("#metric-help-popover"),
  advanced: document.querySelector(".advanced"),
});
const inspector = new EventInspector({
  advanced: document.querySelector(".advanced"),
  list: document.querySelector("#event-list"),
  count: document.querySelector("#events-count"),
  clearButton: document.querySelector("#events-clear"),
  directionFilter: document.querySelector("#event-direction"),
  kindFilter: document.querySelector("#event-kind"),
});

function updateAdvancedConfiguration() {
  const live = selectedMode(modeSelector) === "live";
  document.querySelector("#advanced-mode").textContent = live ? t("call.mode.liveTitle") : t("call.mode.echoTitle");
  document.querySelector("#advanced-provider").textContent = live ? "Gemini Live" : t("provider.echo");
  document.querySelector("#advanced-endpoint").textContent = live ? "/ws/live" : "/ws/audio";
  document.querySelector("#advanced-received-format").textContent =
    live ? "PCM16 · 24 kHz · mono" : "PCM16 · 16 kHz · mono";
}
modeSelector.addEventListener("change", updateAdvancedConfiguration);
updateAdvancedConfiguration();

// Backend outcomes stay language-neutral; translate only their UI labels.
function outcomeLabel(outcome) {
  const labels = {
    completed: "outcome.completed",
    conversation_finished: "outcome.conversationFinished",
    ended: "outcome.ended",
    requested: "outcome.requested",
    accepted: "outcome.accepted",
    decline: "outcome.decline",
    no_answer: "outcome.noAnswer",
    idle_timeout: "outcome.idleTimeout",
    caller_finished: "outcome.callerFinished",
    manual: "outcome.manual",
  };
  return labels[outcome] ? t(labels[outcome]) : (outcome || t("outcome.completed"));
}

function setCallState(label, tone = "") {
  callStateLabel.dataset.labelKey = label;
  callStateLabel.textContent = t(label);
  if (tone) callStateLabel.dataset.tone = tone;
  else delete callStateLabel.dataset.tone;
}
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
  onCapabilities: ({ greeting_locale }) => {
    if (!hasStoredLocale() && SUPPORTED_LOCALES.includes(greeting_locale)) applyLocale(greeting_locale, false);
  },
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
  const labels = {
    ai_active: "call.state.answering",
    handoff_required: "handoff.requested",
    human_active: "call.state.human",
    completed: "call.state.completed",
  };
  const tones = {
    ai_active: "active",
    handoff_required: "attention",
    human_active: "attention",
    completed: "done",
  };
  setCallState(labels[state] || state.replaceAll("_", " "), tones[state]);
  transcript.resolveHandoff(outcome);
  handoffControls.hidden = state !== "handoff_required";
  for (const { button } of handoffButtons) button.disabled = state !== "handoff_required";
  if (outcome === "decline") callFeedback.textContent = t("handoff.declinedCanMessage");
  else if (outcome === "no_answer") callFeedback.textContent = t("handoff.noAnswerCanMessage");
  else if (state === "human_active") callFeedback.textContent = t("handoff.humanAcceptedDetail");
  else if (state === "completed") callFeedback.textContent = t("call.completedFeedback", { reason: outcomeLabel(outcome) });
  else if (state === "handoff_required") callFeedback.textContent = t("handoff.chooseResponse");
  else if (state === "ai_active") callFeedback.textContent = "";
}

function sendHandoffAction(action) {
  const current = session;
  if (!current || !current.live || current.callState !== "handoff_required" || current.pendingAction) return;
  if (!current.socket.sendControl(action)) {
    callFeedback.textContent = t("error.commandSend");
    return;
  }
  current.pendingAction = true;
  callFeedback.textContent = t("handoff.waitingConfirmation");
  for (const { button } of handoffButtons) button.disabled = true;
}

for (const { button, action } of handoffButtons) {
  button.addEventListener("click", () => sendHandoffAction(action));
}
const transcript = new TranscriptView(
  document.querySelector("#transcript-list"),
  document.querySelector("#transcript-status"),
  document.querySelector("#transcript-announcement"),
);

let session = null;

function applyLocale(locale, persist = true) {
  if (session || !setLocale(locale, { persist })) return;
  languageSelect.value = getLocale();
  setup.render();
  summaryView.refreshLocale();
  transcript.refreshLocale();
  updateAdvancedConfiguration();
  inspector.render();
  if (callStateLabel.dataset.labelKey) setCallState(callStateLabel.dataset.labelKey, callStateLabel.dataset.tone || "");
  startButtonLabel.textContent = t(startButtonLabel.dataset.labelKey || "call.start");
}
languageSelect.addEventListener("change", () => applyLocale(languageSelect.value));

function readableError(error) {
  if (error?.name === "NotAllowedError") return t("mic.permissionDenied");
  if (error?.name === "NotFoundError") return t("mic.notFound");
  if (error?.name === "NotReadableError") return t("mic.unavailable");
  return error?.message || t("error.audioUnknown");
}

async function stopSession(error) {
  if (!session) return;
  const current = session;
  session = null;
  setup.setActive(true);
  stopButton.disabled = true;
  stopButton.hidden = true;
  modeSelector.disabled = false;
  languageSelect.disabled = false;
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
    micStatus.textContent = error ? t("error.generic", { message: readableError(error) }) : t("mic.stopped");
    socketStatus.textContent = error ? t("socket.disconnectedError") : t("socket.disconnected");
    aiStatus.textContent = error && current.callState !== "completed"
      ? readableError(error) : current.callState === "completed" ? t("call.state.finished") : t("call.state.idle");
    if (current.callState === "completed") summaryView.unavailable();
    transcript.finish();
    handoffControls.hidden = true;
    if (current.callState === "completed") setCallState("call.state.completed", "done");
    else if (error) setCallState("socket.disconnected", "error");
    else setCallState("call.state.ended", "done");
    signalStatus.textContent = t("mic.noInput");
    level.value = 0;
    startButtonLabel.dataset.labelKey = "call.startNew";
    startButtonLabel.textContent = t("call.startNew");
    setup.setActive(false);
  }
}

async function startMicrophone(current, socket) {
  if (session !== current || current.microphone || current.microphoneStarting) return;
  current.microphoneStarting = true;
  const live = current.live;
  try {
    micStatus.textContent = t("mic.permissionRequest");
    const microphone = new MicrophoneCapture({
      onAudioChunk: (pcm16, rms) => {
        if (session !== current || current.finishing) return;
        chunks.value = String(Number(chunks.value) + 1);
        const db = rms > 0 ? 20 * Math.log10(rms) : -Infinity;
        level.value = Math.max(0, Math.min(100, Math.round((db + 60) * (100 / 54))));
        signalStatus.textContent = rms >= 0.008
          ? t("mic.soundDetected")
          : live ? t("mic.lowInputFiltered") : t("mic.lowInputReceived");
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
    micStatus.textContent = t("mic.recording");
    micReady.classList.add("is-active");
    if (!live) aiStatus.textContent = t("call.state.echo");
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
  startButtonLabel.dataset.labelKey = "call.start";
  startButtonLabel.textContent = t("call.start");
  modeSelector.disabled = true;
  languageSelect.disabled = true;
  const live = selectedMode(modeSelector) === "live";
  micStatus.textContent = t("socket.connectingGo");
  socketStatus.textContent = t("socket.connecting");
  aiStatus.textContent = live ? t("socket.connectingGemini") : t("call.state.echo");
  signalStatus.textContent = t("mic.waitingChunks");
  level.value = 0;
  transcript.clear();
  transcript.begin(live);
  summaryView.reset();
  setCallState(live ? "call.state.connecting" : "call.state.echo", live ? "active" : "");
  callFeedback.textContent = "";
  inspector.clear();
  handoffControls.hidden = true;
  for (const counter of [chunks, sent, received, dropped, filtered, playbackScheduled, playbackDropped, silentChunks, interruptions]) counter.value = "0";

  const current = { socket: null, microphone: null, player: new AudioPlayer(), gate: live ? new VoiceActivityGate() : null, live, microphoneStarting: false, generationFinished: false, callState: "ai_active", pendingAction: false, finishing: false, playbackAcknowledged: false };
  current.player.onIdle = () => {
    if (session !== current) return;
    acknowledgePlayback();
    if (!current.finishing && current.generationFinished && current.callState !== "human_active") {
      aiStatus.textContent = t("call.state.listening");
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
    locale: live ? getLocale() : null,
    onTraffic: (event) => {
      if (session === current) inspector.record(event);
    },
    onChunk: (buffer) => {
      if (session !== current || current.callState === "handoff_required" || current.callState === "human_active" || current.callState === "completed") return;
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
        aiStatus.textContent = t("call.state.speaking");
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
        setCallState("call.state.finishing", "attention");
        aiStatus.textContent = t("call.state.finalizing");
        callFeedback.textContent = t("call.finalAudioFeedback", { reason: outcomeLabel(event.reason || "conversation_finished") });
        stopButton.disabled = true;
        void current.microphone?.stop().catch(error => console.warn("Microphone stop failed", error));
        // Leave queued audio playing. Only acknowledge after all sources finish.
        acknowledgePlayback();
      }
      if (event.type === "call_state") {
        if (!["ai_active", "handoff_required", "human_active", "completed"].includes(event.state)) return;
        showCallState(current, event.state, event.outcome);
        if (event.state === "handoff_required") { current.player.clear(); current.generationFinished = true; }
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
          micStatus.textContent = t("mic.handoffStopped");
          aiStatus.textContent = t("call.state.aiStopped");
        }
        if (event.state === "completed") {
          current.finishing = true;
          stopButton.disabled = true;
          aiStatus.textContent = t("summary.preparing");
          summaryView.loading();
          void current.microphone?.stop().catch(error => console.warn("Microphone stop failed", error));
        }
      }
      if (event.type === "call_end_cancelled") {
        callFeedback.textContent = t("call.continuing");
        setCallState("call.state.answering", "active");
        stopButton.disabled = false;
      }
      if (event.type === "call_error") {
        current.pendingAction = false;
        if (event.action === "playback_finished") {
          void stopSession(new Error(event.error || "Completion was rejected"));
          return;
        }
        if (!current.finishing && current.callState !== "completed") stopButton.disabled = false;
        callFeedback.textContent = t("error.commandRejected", { action: event.action || "", error: event.error || t("error.unknown") });
        for (const { button } of handoffButtons) button.disabled = current.callState !== "handoff_required";
      }
      if (event.type === "handoff_required") {
        current.callState = "handoff_required";
        current.player.clear();
        current.generationFinished = true;
        console.info("Callio handoff_required received");
        transcript.addHandoff(event);
      }
      if (event.type === "turn_end" && current.callState !== "human_active") {
        current.generationFinished = true;
        aiStatus.textContent = current.player.sources.size > 0
          ? t("call.state.playing") : t("call.state.listeningCaller");
      }
      if (event.type === "interrupted" && current.callState !== "human_active") {
        interruptions.value = String(Number(interruptions.value) + 1);
        current.generationFinished = false;
        current.player.clear();
        aiStatus.textContent = t("call.state.interrupted");
      }
    },
    onDisconnect: (error) => {
      // An already completed call is successful even when the socket closes
      // before the optional summary arrives.
      if (session === current) void stopSession(current.callState === "completed" ? undefined : error);
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
    socketStatus.textContent = t("socket.connected");

    stopButton.disabled = false;
    if (live) setCallState("call.state.answering", "active");
    if (live) aiStatus.textContent = t("call.state.greeting");
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
  callFeedback.textContent = t("call.state.ending");
});

echo.addEventListener("change", () => {
  if (!session || session.live) return;
  if (echo.checked) {
    void session.player.enable().catch((error) => {
      echo.checked = false;
      socketStatus.textContent = t("error.playback", { message: readableError(error) });
    });
  } else {
    void session.player.disable();
  }
});

// HTML is served in English; localize it after view initialization.
localizePage();
