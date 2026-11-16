import assert from "node:assert/strict";
import test from "node:test";
import { MetricTooltips, positionMetricTooltip } from "../web/metric-tooltip.js";

function fakeElement({ tooltip = false, help = "", id = "" } = {}) {
  const listeners = new Map();
  const attributes = new Map();
  const el = {
    id,
    dataset: { help },
    style: {},
    textContent: "",
    hidden: false,
    open: true,
    visible: false,
    addEventListener(type, fn) { listeners.set(type, fn); },
    dispatch(type, event = {}) { listeners.get(type)?.(event); },
    setAttribute(key, value) { attributes.set(key, value); },
    removeAttribute(key) { attributes.delete(key); },
    getAttribute(key) { return attributes.get(key); },
    getBoundingClientRect() {
      return tooltip
        ? { width: 200, height: 60 }
        : { left: 100, top: 80, right: 128, bottom: 108, width: 28, height: 28 };
    },
    matches(selector) {
      return selector === ":popover-open" ? this.visible : false;
    },
    contains(target) { return this === target; },
    showPopover() { this.visible = true; },
    hidePopover() { this.visible = false; },
  };
  return el;
}

function fixture({ hover = true } = {}) {
  const previousDocument = globalThis.document;
  const previousWindow = globalThis.window;
  const document = fakeElement();
  const window = fakeElement();
  window.innerWidth = 800;
  window.innerHeight = 600;
  window.matchMedia = () => ({ matches: hover });
  globalThis.document = document;
  globalThis.window = window;
  const one = fakeElement({ help: "Captured audio chunks" });
  const two = fakeElement({ help: "Uploaded audio chunks" });
  const tooltip = fakeElement({ tooltip: true, id: "metric-help-popover" });
  const advanced = fakeElement();
  const manager = new MetricTooltips({ triggers: [one, two], tooltip, advanced });
  return {
    one, two, tooltip, advanced, manager, document, window,
    restore() {
      globalThis.document = previousDocument;
      globalThis.window = previousWindow;
    },
  };
}

test("desktop pointer hover opens tooltip; leaving closes it", () => {
  const t = fixture();
  try {
    t.one.dispatch("pointerenter", { pointerType: "mouse" });
    assert.equal(t.manager.active, t.one);
    assert.equal(t.tooltip.visible, true);
    assert.equal(t.tooltip.textContent, "Captured audio chunks");
    assert.equal(t.one.getAttribute("aria-expanded"), "true");
    t.one.dispatch("pointerleave", { pointerType: "mouse" });
    assert.equal(t.tooltip.visible, false);
    assert.equal(t.one.getAttribute("aria-expanded"), "false");
  } finally { t.restore(); }
});

test("only one tooltip stays open when switching between metrics", () => {
  const t = fixture();
  try {
    t.one.dispatch("pointerenter", { pointerType: "mouse" });
    t.two.dispatch("pointerenter", { pointerType: "mouse" });
    assert.equal(t.manager.active, t.two);
    assert.equal(t.one.getAttribute("aria-expanded"), "false");
    assert.equal(t.two.getAttribute("aria-expanded"), "true");
    assert.equal(t.tooltip.textContent, "Uploaded audio chunks");
  } finally { t.restore(); }
});

test("touch toggles a tooltip and tapping outside dismisses it", () => {
  const t = fixture({ hover: false });
  try {
    t.one.dispatch("pointerdown", { pointerType: "touch" });
    t.one.dispatch("click", { detail: 1 });
    assert.equal(t.manager.active, t.one);
    t.one.dispatch("pointerdown", { pointerType: "touch" });
    t.one.dispatch("click", { detail: 1 });
    assert.equal(t.manager.active, null);
    t.two.dispatch("pointerdown", { pointerType: "touch" });
    t.two.dispatch("click", { detail: 1 });
    assert.equal(t.manager.active, t.two);
    t.document.dispatch("pointerdown", { target: fakeElement() });
    assert.equal(t.manager.active, null);
  } finally { t.restore(); }
});

test("keyboard focus, Escape and closing Advanced dismiss the tooltip", () => {
  const t = fixture();
  try {
    t.one.dispatch("click", { detail: 0 });
    assert.equal(t.manager.active, t.one);
    let prevented = false;
    t.document.dispatch("keydown", { key: "Escape", preventDefault() { prevented = true; } });
    assert.equal(prevented, true);
    assert.equal(t.manager.active, null);
    t.two.dispatch("click", { detail: 0 });
    t.advanced.open = false;
    t.advanced.dispatch("toggle");
    assert.equal(t.manager.active, null);
  } finally { t.restore(); }
});

test("tooltip geometry flips above a low trigger and stays inside viewport", () => {
  const trigger = fakeElement();
  trigger.getBoundingClientRect = () => ({
    left: 780, right: 808, width: 28,
    top: 555, bottom: 583, height: 28,
  });
  const tooltip = fakeElement({ tooltip: true });
  assert.equal(positionMetricTooltip(trigger, tooltip, { width: 800, height: 600 }), true);
  assert.equal(tooltip.style.left, "588px");
  assert.equal(tooltip.style.top, "487px");
});
