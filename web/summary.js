import { t } from "./i18n.js";

// Post-call results are kept as raw data; only labels/enums are translated.
export class SummaryView {
  constructor({ panel, status, details, caller, reason, urgency, handoff, text, actions }) {
    Object.assign(this, { panel, status, details, caller, reason, urgency, handoff, text, actions });
    this.reset();
  }

  reset() {
    this.pending = false;
    this.lastEvent = null;
    this.mode = "waiting";
    this.panel.hidden = true;
    this.details.hidden = true;
    this.status.textContent = t("summary.waiting");
  }

  loading() {
    this.pending = true;
    this.mode = "loading";
    this.panel.hidden = false;
    this.details.hidden = true;
    this.status.textContent = t("summary.generating");
  }

  unavailable() {
    if (!this.pending) return;
    this.pending = false;
    this.mode = "unavailable";
    this.panel.hidden = false;
    this.details.hidden = true;
    this.status.textContent = t("summary.unavailable");
  }

  render(event) {
    this.pending = false;
    this.lastEvent = event;
    this.panel.hidden = false;
    const data = event.type === "call_summary" ? event.summary : null;
    if (!data || typeof data.summary !== "string" || !data.summary.trim()) {
      this.mode = "unavailable";
      this.status.textContent = t("summary.unavailable");
      this.details.hidden = true;
      return;
    }
    this.mode = "result";
    this.status.textContent = t("summary.generated");
    this.details.hidden = false;
    const value = key => typeof data[key] === "string" && data[key].trim() ? data[key] : t("common.unknown");
    this.caller.textContent = value("caller_name");
    this.reason.textContent = value("reason");
    this.urgency.textContent = ["unknown", "low", "normal", "high"].includes(data.urgency)
      ? t("urgency." + data.urgency) : t("common.unknown");
    this.handoff.textContent = data.handoff_requested === true ? t("common.yes") : t("common.no");
    // Narrative and action strings were already generated in the session locale.
    this.text.textContent = data.summary;
    this.actions.replaceChildren();
    const items = Array.isArray(data.follow_up_actions)
      ? data.follow_up_actions.filter(x => typeof x === "string" && x.trim()) : [];
    for (const action of items.length ? items : [t("summary.noFollowUp")]) {
      const item = document.createElement("li");
      item.textContent = action;
      this.actions.append(item);
    }
  }

  refreshLocale() {
    if (this.lastEvent) { this.render(this.lastEvent); return; }
    if (this.mode === "loading") this.loading();
    else if (this.mode === "unavailable") {
      this.status.textContent = t("summary.unavailable");
    } else {
      this.status.textContent = t("summary.waiting");
    }
  }
}
