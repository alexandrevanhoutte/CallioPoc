import assert from "node:assert/strict";
import test from "node:test";
import { modeDescription, selectedMode, setupMessage, SetupView } from "../web/setup.js";

test("local setup keeps Echo usable without Gemini credentials", () => {
  assert.match(modeDescription("echo", false), /without an API key/);
  assert.equal(setupMessage({ online: true, liveConfigured: false, mode: "echo" }), "");
  assert.match(setupMessage({ online: true, liveConfigured: false, mode: "live" }), /GEMINI_API_KEY/);
  assert.match(setupMessage({ online: false, liveConfigured: null, mode: "echo" }), /go run/);
});

function element() {
  return {
    textContent: "", disabled: false, hidden: false, value: "live",
    classList: { toggle() {} },
    addEventListener(name, callback) { this[name] = callback; },
    querySelector() { return { value: this.value }; },
  };
}

test("setup view checks capabilities and gates only unavailable live mode", async () => {
  const refs = { status: element(), dot: element(), retry: element(), selector: element(),
    description: element(), notice: element(), start: element() };
  const previousFetch = globalThis.fetch;
  globalThis.fetch = async () => ({ ok: true, json: async () => ({ status: "ok", live_configured: false }) });
  try {
    const view = new SetupView(refs);
    await view.check();
    assert.equal(refs.status.textContent, "Backend connected");
    assert.equal(refs.start.disabled, true);
    refs.selector.value = "echo";
    refs.selector.change();
    assert.equal(refs.start.disabled, false);
    view.setActive(true);
    assert.equal(refs.start.disabled, true);
    view.setActive(false);
    assert.equal(refs.start.disabled, false);
  } finally {
    globalThis.fetch = previousFetch;
  }
});

test("setup view gives a recoverable state on backend failure", async () => {
  const refs = { status: element(), dot: element(), retry: element(), selector: element(),
    description: element(), notice: element(), start: element() };
  const previousFetch = globalThis.fetch;
  globalThis.fetch = async () => { throw new Error("Connection refused"); };
  try {
    const view = new SetupView(refs);
    await view.check();
    assert.equal(refs.start.disabled, true);
    assert.equal(refs.retry.hidden, false);
    assert.match(refs.notice.textContent, /Start Go/);
  } finally {
    globalThis.fetch = previousFetch;
  }
});

test("native mode selection reads the checked radio option", () => {
  const group = element();
  group.value = "echo";
  assert.equal(selectedMode(group), "echo");
  group.value = "live";
  assert.equal(selectedMode(group), "live");
});
