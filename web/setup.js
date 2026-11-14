// One small view for local setup and backend readiness.
// "live_configured" is only a server-side configuration hint, not a guarantee of Gemini availability.
export function selectedMode(group) {
  return group.querySelector('input[name="mode"]:checked')?.value ?? "live";
}

export function modeDescription(mode, liveConfigured) {
  if (mode === "echo") return "Test microphone streaming without an API key. Playback can be enabled in Advanced.";
  return liveConfigured === false
    ? "Gemini Live is not configured. Add GEMINI_API_KEY to .env and restart Go, or try Echo."
    : "Talk to Callio in different languages. Requires a Gemini API key configured on the server.";
}

export function setupMessage({ online, liveConfigured, mode }) {
  if (!online) return "Backend unavailable. Start Go with 'go run ./cmd/callio', then select Retry.";
  if (mode === "live" && liveConfigured === false) return "Gemini Live is not configured. Add GEMINI_API_KEY to .env, restart Go, or select Echo.";
  return "";
}

export class SetupView {
  constructor({ status, dot, retry, selector, description, notice, start }) {
    Object.assign(this, { status, dot, retry, selector, description, notice, start });
    this.online = false;
    this.liveConfigured = null;
    this.busy = false;
    this.active = false;
    selector.addEventListener("change", () => this.render());
    retry.addEventListener("click", () => void this.check());
    this.render();
  }

  setActive(active) {
    this.active = active;
    this.render();
  }

  render() {
    const mode = selectedMode(this.selector);
    const message = this.busy ? "" : setupMessage({ online: this.online, liveConfigured: this.liveConfigured, mode });
    this.description.textContent = modeDescription(mode, this.liveConfigured);
    this.notice.hidden = !message;
    this.notice.textContent = message;
    this.notice.classList.toggle("is-error", !this.online);
    this.start.disabled = this.active || this.busy || !this.online || (mode === "live" && this.liveConfigured === false);
    this.dot.classList.toggle("is-connected", this.online);
    this.dot.classList.toggle("is-error", !this.online && !this.busy);
    this.status.textContent = this.busy ? "Checking backend…" : this.online ? "Backend connected" : "Backend unavailable";
    this.retry.disabled = this.busy;
    this.retry.hidden = this.online;
  }

  async check() {
    if (this.busy) return;
    this.busy = true;
    this.render();
    try {
      const response = await fetch("/capabilities", { cache: "no-store" });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const data = await response.json();
      if (data.status !== "ok" || typeof data.live_configured !== "boolean") throw new Error("Invalid capabilities response");
      this.online = true;
      this.liveConfigured = data.live_configured;
    } catch {
      this.online = false;
      this.liveConfigured = null;
    } finally {
      this.busy = false;
      this.render();
    }
  }
}
