// Keep protocol inspection bounded and independent of the audio hot path.
// Only transport metadata is accepted: never store PCM or JSON message bodies.
export const MAX_TRAFFIC_EVENTS = 200;
const RENDER_DELAY_MS = 250;
const EVENT_TYPE = /^[a-z][a-z0-9_]{0,63}$/;

export class TrafficLog {
  constructor(limit = MAX_TRAFFIC_EVENTS) {
    if (!Number.isInteger(limit) || limit < 1) throw new RangeError("invalid traffic log limit");
    this.limit = limit;
    this.entries = [];
  }

  add({ timestamp = Date.now(), direction, kind, type, size } = {}) {
    if (!["in", "out"].includes(direction) || !["json", "binary"].includes(kind) ||
        !Number.isSafeInteger(size) || size < 0) return false;
    this.entries.push({
      timestamp: Number.isFinite(timestamp) ? timestamp : Date.now(),
      direction,
      kind,
      type: kind === "json" ? (EVENT_TYPE.test(type) ? type : "json_event") : "audio_frame",
      size,
    });
    if (this.entries.length > this.limit) this.entries.shift();
    return true;
  }

  clear() { this.entries.length = 0; }

  filter(direction = "all", kind = "all") {
    return this.entries.filter(event =>
      (direction === "all" || event.direction === direction) &&
      (kind === "all" || event.kind === kind));
  }
}

export class EventInspector {
  constructor({ advanced, list, count, clearButton, directionFilter, kindFilter }) {
    Object.assign(this, { advanced, list, count, clearButton, directionFilter, kindFilter });
    this.log = new TrafficLog();
    this.timer = null;
    advanced.addEventListener("toggle", () => {
      if (advanced.open) this.render();
      else this.cancelRender();
    });
    directionFilter.addEventListener("change", () => this.render());
    kindFilter.addEventListener("change", () => this.render());
    clearButton.addEventListener("click", () => this.clear());
  }

  record(event) {
    if (!this.log.add(event) || !this.advanced.open || this.timer !== null) return;
    // At most four DOM renders per second, even with 50+ audio frames/second.
    this.timer = setTimeout(() => {
      this.timer = null;
      if (this.advanced.open) this.render();
    }, RENDER_DELAY_MS);
  }

  cancelRender() {
    if (this.timer !== null) clearTimeout(this.timer);
    this.timer = null;
  }

  clear() {
    this.log.clear();
    this.cancelRender();
    this.render();
  }

  render() {
    if (!this.advanced.open) return;
    const events = this.log.filter(this.directionFilter.value, this.kindFilter.value).reverse();
    const rows = events.map(event => {
      const row = document.createElement("li");
      row.className = "event-entry";
      const fields = [
        ["event-time", new Date(event.timestamp).toLocaleTimeString("en-GB", { hour12: false })],
        ["event-direction", event.direction.toUpperCase()],
        ["event-type", event.kind === "binary" ? "Audio frame (PCM16)" : event.type],
        ["event-size", event.size + " B"],
      ];
      for (const [className, value] of fields) {
        const span = document.createElement("span");
        span.className = className;
        span.textContent = value;
        row.append(span);
      }
      row.dataset.direction = event.direction;
      return row;
    });
    this.list.replaceChildren(...rows);
    this.count.textContent = events.length + " shown · " + this.log.entries.length +
      " buffered (max " + this.log.limit + ")";
    this.clearButton.disabled = this.log.entries.length === 0;
  }
}
