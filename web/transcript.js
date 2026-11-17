import { t } from "./i18n.js";

// State is separate from DOM rendering so finalized segments are easy to
// consume for the later summary and persistence tickets.
export class TranscriptStore {
  constructor() {
    this.segments = new Map();
    this.handoff = null;
  }

  upsert(event) {
    if (!event || event.type !== "transcript" ||
        typeof event.segment_id !== "string" || !event.segment_id ||
        !["CALLER", "AI"].includes(event.speaker) ||
        typeof event.text !== "string" || event.text.length === 0) return null;

    const old = this.segments.get(event.segment_id);
    if (old?.final || (old && old.speaker !== event.speaker)) return null;
    const segment = {
      id: event.segment_id,
      speaker: event.speaker,
      order: Number.isSafeInteger(event.order) && event.order > 0 ? event.order : (old?.order ?? Number.MAX_SAFE_INTEGER),
      text: event.text,
      final: event.final === true,
      timestamp: old?.timestamp ?? event.timestamp ?? null,
    };
    this.segments.set(segment.id, segment);
    return segment;
  }

  snapshot() {
    return [...this.segments.values()]
      .sort((a, b) => a.order - b.order)
      .map(segment => ({ ...segment }));
  }

  addHandoff(event) {
    if (this.handoff || event?.type !== "handoff_required" ||
        !["owner_requested", "ai_unable"].includes(event.reason) ||
        typeof event.context?.purpose !== "string" || event.context.purpose.length === 0 ||
        typeof event.context?.summary !== "string" || event.context.summary.length === 0) return false;

    // Mark the position in the logical transcript order at detection time.
    // Late segments with earlier order will still appear before this card.
    const lastOrder = this.snapshot()
      .filter(segment => Number.isSafeInteger(segment.order) && segment.order < Number.MAX_SAFE_INTEGER)
      .reduce((max, segment) => Math.max(max, segment.order), 0);
    this.handoff = { reason: event.reason, context: { ...event.context }, afterOrder: lastOrder };
    return true;
  }

  timeline() {
    const segments = this.snapshot();
    if (!this.handoff) return segments.map(segment => ({ type: "transcript", ...segment }));
    const entries = [];
    let inserted = false;
    for (const segment of segments) {
      if (!inserted && segment.order > this.handoff.afterOrder) {
        entries.push({ type: "handoff", ...this.handoff });
        inserted = true;
      }
      entries.push({ type: "transcript", ...segment });
    }
    if (!inserted) entries.push({ type: "handoff", ...this.handoff });
    return entries;
  }

  finalized() {
    return this.snapshot().filter(segment => segment.final);
  }

  clear() {
    this.segments.clear();
    this.handoff = null;
  }
}

export class TranscriptView {
  constructor(container, status, announcement = null) {
    this.container = container;
    this.status = status;
    this.announcement = announcement;
    this.store = new TranscriptStore();
    this.rows = new Map();
    this.handoffRow = null;
    this.handoffEvent = null;
    this.handoffOutcome = null;
    this.statusKey = "transcript.noConversation";
  }

  setStatus(key) {
    this.statusKey = key;
    this.status.textContent = t(key);
  }

  update(event) {
    const segment = this.store.upsert(event);
    if (!segment) return;
    const label = this.store.handoff ? "handoff.requested" : "transcript.live";
    if (this.statusKey !== label) this.setStatus(label);
    let row = this.rows.get(segment.id);
    if (!row) {
      row = document.createElement("div");
      row.className = `transcript-segment ${segment.speaker === "CALLER" ? "is-caller" : "is-ai"}`;
      const speaker = document.createElement("strong");
      speaker.textContent = segment.speaker === "AI" ? "Callio" : t("fields.caller");
      const text = document.createElement("p");
      const pending = document.createElement("span");
      pending.className = "transcript-progress";
      pending.textContent = t("transcript.transcribing");
      pending.setAttribute("aria-hidden", "true");
      row.append(speaker, text, pending);
      this.rows.set(segment.id, row);
      this.container.append(row);
    }
    row.classList.toggle("transcript-partial", !segment.final);
    row.querySelector("p").textContent = segment.text;
    this.renderTimeline();
    // Provisional updates can arrive rapidly. Announce finalized text only.
    if (segment.final && this.announcement) {
      this.announcement.textContent = `${segment.speaker === "AI" ? "Callio" : t("fields.caller")}: ${segment.text}`;
    }
  }

  addHandoff(event) {
    if (!this.store.addHandoff(event)) return;
    this.handoffEvent = event;
    this.handoffOutcome = null;
    this.renderHandoff();
    this.setStatus("handoff.requested");
    this.renderTimeline();
  }

  renderHandoff() {
    const event = this.handoffEvent;
    if (!event) return;
    const context = event.context;
    const row = document.createElement("div");
    row.className = "handoff-card";
    const title = document.createElement("strong");
    title.textContent = t("handoff.simulatedTitle");
    row.append(title);
    const detail = (label, value) => {
      if (typeof value !== "string" || value.length === 0) return;
      const line = document.createElement("p");
      line.textContent = t(label) + ": " + value;
      row.append(line);
    };
    detail("fields.reason", event.reason === "owner_requested"
      ? t("handoff.reasonOwner") : t("handoff.reasonAi"));
    detail("fields.caller", context.caller_name);
    detail("fields.company", context.company);
    detail("fields.purpose", context.purpose);
    detail("fields.summary", context.summary);
    detail("fields.email", context.email);
    detail("fields.phone", context.phone_number);
    const note = document.createElement("p");
    note.className = "handoff-result";
    const messages = {
      accepted: "handoff.accepted",
      decline: "handoff.declined",
      no_answer: "handoff.noAnswerContinues",
    };
    note.textContent = t(messages[this.handoffOutcome] || "handoff.awaitingDecision");
    row.append(note);
    this.handoffRow = row;
  }

  // Called only after the backend confirms the simulated handoff outcome.
  resolveHandoff(outcome) {
    if (!this.handoffEvent || !["accepted", "decline", "no_answer"].includes(outcome)) return;
    this.handoffOutcome = outcome;
    const messages = {
      accepted: "handoff.accepted",
      decline: "handoff.declined",
      no_answer: "handoff.noAnswerContinues",
    };
    const note = this.handoffRow?.querySelector(".handoff-result");
    if (note) note.textContent = t(messages[outcome]);
  }

  refreshLocale() {
    this.setStatus(this.statusKey);
    for (const [id, row] of this.rows) {
      const segment = this.store.segments.get(id);
      if (!segment) continue;
      row.querySelector("strong").textContent = segment.speaker === "AI" ? "Callio" : t("fields.caller");
      row.querySelector(".transcript-progress").textContent = t("transcript.transcribing");
    }
    if (this.handoffEvent) {
      this.renderHandoff();
      this.renderTimeline();
    }
  }

  begin(live) {
    this.setStatus(live ? "transcript.listening" : "transcript.echoNotAvailable");
  }

  renderTimeline() {
    // Reuse existing transcript nodes; only their position changes when
    // late transcriptions arrive. The handoff card remains in the timeline.
    const nodes = this.store.timeline().map(item =>
      item.type === "handoff" ? this.handoffRow : this.rows.get(item.id)
    ).filter(Boolean);
    this.container.replaceChildren(...nodes);
    this.container.scrollTop = this.container.scrollHeight;
  }

  finish() {
    this.setStatus(this.store.segments.size || this.store.handoff
      ? "transcript.retained"
      : "transcript.emptyAfterCall");
  }

  clear() {
    this.store.clear();
    this.rows.clear();
    this.handoffRow = null;
    this.handoffEvent = null;
    this.handoffOutcome = null;
    this.container.replaceChildren();
    if (this.announcement) this.announcement.textContent = "";
    this.setStatus("transcript.noConversation");
  }
}
