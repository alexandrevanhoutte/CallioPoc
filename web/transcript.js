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
  }

  update(event) {
    const segment = this.store.upsert(event);
    if (!segment) return;
    const label = this.store.handoff ? "Handoff requested" : "Live transcript";
    if (this.status.textContent !== label) this.status.textContent = label;
    let row = this.rows.get(segment.id);
    if (!row) {
      row = document.createElement("div");
      row.className = `transcript-segment ${segment.speaker === "CALLER" ? "is-caller" : "is-ai"}`;
      const speaker = document.createElement("strong");
      speaker.textContent = segment.speaker === "AI" ? "Callio" : "Caller";
      const text = document.createElement("p");
      const pending = document.createElement("span");
      pending.className = "transcript-progress";
      pending.textContent = "Transcribing…";
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
      this.announcement.textContent = `${segment.speaker === "AI" ? "Callio" : "Caller"}: ${segment.text}`;
    }
  }

  addHandoff(event) {
    if (!this.store.addHandoff(event)) return;
    const context = event.context;
    const row = document.createElement("div");
    row.className = "handoff-card";
    const title = document.createElement("strong");
    title.textContent = "Simulated handoff requested";
    row.append(title);
    const detail = (label, value) => {
      if (typeof value !== "string" || value.length === 0) return;
      const line = document.createElement("p");
      line.textContent = `${label}: ${value}`;
      row.append(line);
    };
    detail("Reason", event.reason === "owner_requested"
      ? "Caller asked for the owner" : "Callio needs assistance");
    detail("Caller", context.caller_name);
    detail("Company", context.company);
    detail("Purpose", context.purpose);
    detail("Summary", context.summary);
    detail("Email", context.email);
    detail("Phone", context.phone_number);
    const note = document.createElement("p");
    note.className = "handoff-result";
    note.textContent = "Awaiting your decision — no real transfer will occur.";
    row.append(note);
    this.handoffRow = row;
    this.status.textContent = "Handoff requested";
    this.renderTimeline();
  }

  // Called only after the backend confirms the simulated handoff outcome.
  resolveHandoff(outcome) {
    const status = this.handoffRow?.querySelector(".handoff-result");
    if (!status) return;
    const messages = {
      accepted: "Human takeover accepted (simulation only).",
      decline: "Takeover declined; Callio will continue.",
      no_answer: "No answer simulated; Callio will continue.",
    };
    if (messages[outcome]) status.textContent = messages[outcome];
  }

  begin(live) {
    this.status.textContent = live ? "Listening for speech…" : "Echo test — no transcript in this mode";
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
    this.status.textContent = this.store.segments.size || this.store.handoff
      ? "Conversation ended — transcript retained until the next call"
      : "Call ended — no transcript received";
  }

  clear() {
    this.store.clear();
    this.rows.clear();
    this.handoffRow = null;
    this.container.replaceChildren();
    if (this.announcement) this.announcement.textContent = "";
    this.status.textContent = "No conversation yet";
  }
}
