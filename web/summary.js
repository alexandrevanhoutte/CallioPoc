// The post-call summary owns its own DOM rendering; the call lifecycle remains in app.js.
export class SummaryView {
  constructor({ panel, status, details, caller, reason, urgency, handoff, text, actions }) {
    Object.assign(this, { panel, status, details, caller, reason, urgency, handoff, text, actions });
  }

  reset() {
    this.panel.hidden = true;
    this.details.hidden = true;
    this.status.textContent = "Waiting for call completion…";
  }

  loading() {
    this.panel.hidden = false;
    this.details.hidden = true;
    this.status.textContent = "Generating summary…";
  }

  render(event) {
    this.panel.hidden = false;
    const data = event.type === "call_summary" ? event.summary : null;
    if (!data || typeof data.summary !== "string" || !data.summary.trim()) {
      this.status.textContent = "Summary unavailable";
      this.details.hidden = true;
      return;
    }
    this.status.textContent = "Generated summary";
    this.details.hidden = false;
    const value = key => typeof data[key] === "string" && data[key].trim() ? data[key] : "Unknown";
    this.caller.textContent = value("caller_name");
    this.reason.textContent = value("reason");
    this.urgency.textContent = value("urgency");
    this.handoff.textContent = data.handoff_requested === true ? "Yes" : "No";
    this.text.textContent = data.summary;
    this.actions.replaceChildren();
    const items = Array.isArray(data.follow_up_actions) ? data.follow_up_actions.filter(x => typeof x === "string" && x.trim()) : [];
    for (const action of items.length ? items : ["No follow-up required"]) {
      const item = document.createElement("li");
      item.textContent = action;
      this.actions.append(item);
    }
  }
}
