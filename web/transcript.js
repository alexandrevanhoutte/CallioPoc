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
      this.rows.set(segment.id, row);
      this.container.append(row);
    }
    row.classList.toggle("transcript-partial", !segment.final);
    row.querySelector("p").textContent = segment.text;
    // A late caller transcription can belong before an assistant reply.
    // Reorder existing nodes instead of recreating rows (and losing updates).
    for (const item of this.store.snapshot()) {
      const node = this.rows.get(item.id);
      if (node) this.container.append(node);
    }
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
