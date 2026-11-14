// State is separate from DOM rendering so finalized segments are easy to
// consume for the later summary and persistence tickets.
export class TranscriptStore {
  constructor() {
    this.segments = new Map();
  }

  upsert(event) {
    if (!event || event.type !== "transcript" ||
        typeof event.segment_id !== "string" || !event.segment_id ||
        !["CALLER", "AI"].includes(event.speaker) ||
        typeof event.text !== "string" || !event.text.trim()) return null;

    const old = this.segments.get(event.segment_id);
    if (old?.final || (old && old.speaker !== event.speaker)) return null;
    const segment = {
      id: event.segment_id,
      speaker: event.speaker,
      text: event.text,
      final: event.final === true,
      timestamp: old?.timestamp ?? event.timestamp ?? null,
    };
    this.segments.set(segment.id, segment);
    return segment;
  }

  snapshot() {
    return [...this.segments.values()].map(segment => ({ ...segment }));
  }

  finalized() {
    return this.snapshot().filter(segment => segment.final);
  }

  clear() {
    this.segments.clear();
  }
}

export class TranscriptView {
  constructor(container, status) {
    this.container = container;
    this.status = status;
    this.store = new TranscriptStore();
    this.rows = new Map();
  }

  update(event) {
    const segment = this.store.upsert(event);
    if (!segment) return;
    this.status.textContent = "Live transcript";
    let row = this.rows.get(segment.id);
    if (!row) {
      row = document.createElement("div");
      row.className = "transcript-segment";
      const speaker = document.createElement("strong");
      speaker.textContent = segment.speaker === "AI" ? "Callio" : "Caller";
      const text = document.createElement("p");
      row.append(speaker, text);
      this.container.append(row);
      this.rows.set(segment.id, row);
    }
    row.classList.toggle("transcript-partial", !segment.final);
    row.querySelector("p").textContent = segment.text;
    this.container.scrollTop = this.container.scrollHeight;
  }

  finish() {
    this.status.textContent = this.store.segments.size
      ? "Conversation ended — transcript retained in this page"
      : "No transcript received";
  }

  clear() {
    this.store.clear();
    this.rows.clear();
    this.container.replaceChildren();
    this.status.textContent = "Waiting for conversation…";
  }
}
