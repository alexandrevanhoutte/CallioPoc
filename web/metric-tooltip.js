// One tooltip in the browser top layer prevents cards from overlapping it.
// Content comes from the trusted, static metric descriptions in index.html.
const GAP = 8;
const EDGE = 12;

export function positionMetricTooltip(trigger, tooltip, viewport) {
  const anchor = trigger.getBoundingClientRect();
  const panel = tooltip.getBoundingClientRect();
  const width = viewport.width;
  const height = viewport.height;

  if (anchor.bottom < 0 || anchor.top > height) return false;

  const below = anchor.bottom + GAP;
  const above = anchor.top - panel.height - GAP;
  const placeAbove = below + panel.height > height - EDGE && above >= EDGE;
  const top = Math.max(EDGE, Math.min(
    placeAbove ? above : below,
    height - panel.height - EDGE,
  ));
  const centered = anchor.left + anchor.width / 2 - panel.width / 2;
  const left = Math.max(EDGE, Math.min(centered, width - panel.width - EDGE));

  tooltip.style.left = `${Math.round(left)}px`;
  tooltip.style.top = `${Math.round(top)}px`;
  return true;
}

export class MetricTooltips {
  constructor({ triggers, tooltip, advanced }) {
    this.triggers = [...triggers];
    this.tooltip = tooltip;
    this.advanced = advanced;
    this.active = null;
    this.lastPointerType = null;
    this.fallback = typeof tooltip.showPopover !== "function";

    if (this.fallback) {
      tooltip.removeAttribute("popover");
      tooltip.hidden = true;
    }

    for (const trigger of this.triggers) {
      trigger.addEventListener("pointerenter", event => {
        if (event.pointerType === "mouse" && this.canHover()) this.show(trigger);
      });
      trigger.addEventListener("pointerleave", event => {
        if (event.pointerType === "mouse" && this.active === trigger &&
            !trigger.matches(":focus-visible")) this.hide();
      });
      trigger.addEventListener("pointerdown", event => {
        this.lastPointerType = event.pointerType;
      });
      trigger.addEventListener("click", event => {
        // Keyboard activation should not close an already visible tooltip.
        if (event.detail === 0) {
          this.show(trigger);
        } else if (this.lastPointerType === "touch" || !this.canHover()) {
          this.active === trigger ? this.hide() : this.show(trigger);
        }
      });
      trigger.addEventListener("focus", () => {
        if (trigger.matches(":focus-visible")) this.show(trigger);
      });
      trigger.addEventListener("blur", () => {
        if (this.active === trigger && this.canHover()) this.hide();
      });
    }

    document.addEventListener("pointerdown", event => {
      if (this.active && !this.active.contains(event.target)) this.hide();
    });
    document.addEventListener("keydown", event => {
      if (event.key === "Escape" && this.active) {
        this.hide();
        event.preventDefault();
      }
    });
    advanced.addEventListener("toggle", () => {
      if (!advanced.open) this.hide();
    });
    window.addEventListener("resize", () => this.reposition());
    window.addEventListener("scroll", () => this.reposition(), true);
  }

  canHover() {
    return typeof window.matchMedia === "function" &&
      window.matchMedia("(hover: hover) and (pointer: fine)").matches;
  }

  show(trigger) {
    if (!this.advanced.open || this.active === trigger) return;
    this.hide();
    this.active = trigger;
    this.tooltip.textContent = trigger.dataset.help || "";
    trigger.setAttribute("aria-expanded", "true");
    trigger.setAttribute("aria-describedby", this.tooltip.id);
    if (this.fallback) this.tooltip.hidden = false;
    else this.tooltip.showPopover();
    this.reposition();
  }

  reposition() {
    if (!this.active) return;
    if (!positionMetricTooltip(this.active, this.tooltip, {
      width: window.innerWidth,
      height: window.innerHeight,
    })) this.hide();
  }

  hide() {
    if (!this.active) return;
    this.active.setAttribute("aria-expanded", "false");
    this.active.removeAttribute("aria-describedby");
    this.active = null;
    if (this.fallback) this.tooltip.hidden = true;
    else if (this.tooltip.matches(":popover-open")) this.tooltip.hidePopover();
  }
}
