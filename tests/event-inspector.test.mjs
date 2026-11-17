import assert from "node:assert/strict";
import test from "node:test";
import { EventInspector, MAX_TRAFFIC_EVENTS, TrafficLog } from "../web/event-inspector.js";
import { setLocale } from "../web/i18n.js";
import { loadTestLocales } from "./i18n-fixture.mjs";

// Existing view tests assert English copy explicitly; the app defaults to French.
await loadTestLocales();
setLocale("en-US", { persist: false });

const frame = (direction = "in", kind = "binary", type = "", size = 640) =>
  ({ timestamp: 1763180000000, direction, kind, type, size });

test("traffic log retains only the latest bounded metadata", () => {
  const log = new TrafficLog();
  for (let i = 0; i < MAX_TRAFFIC_EVENTS + 10; i++) {
    assert.equal(log.add({ ...frame(), size: i }), true);
  }
  assert.equal(log.entries.length, MAX_TRAFFIC_EVENTS);
  assert.equal(log.entries[0].size, 10);
  assert.equal(log.entries.at(-1).size, MAX_TRAFFIC_EVENTS + 9);
  log.clear();
  assert.equal(log.entries.length, 0);
});

test("direction and kind filters do not retain payloads or unsafe types", () => {
  const log = new TrafficLog(3);
  log.add({ ...frame("in", "json", "transcript", 35), text: "private caller transcript" });
  log.add(frame("out", "binary", "", 640));
  log.add(frame("out", "json", "<img src=x onerror=alert(1)>", 18));
  assert.equal(log.filter("in").length, 1);
  assert.equal(log.filter("out", "json").length, 1);
  assert.equal(log.filter("all", "binary").length, 1);
  assert.equal(log.entries[2].type, "json_event");
  assert.doesNotMatch(JSON.stringify(log.entries), /private caller transcript|<img/);
  assert.equal(log.add(frame("invalid")), false);
  assert.equal(log.add({ ...frame(), size: -1 }), false);
});

function element() {
  return {
    open: false, value: "all", disabled: false, textContent: "", children: [], dataset: {},
    addEventListener(name, handler) { this[name] = handler; },
    append(child) { this.children.push(child); },
    replaceChildren(...children) { this.children = children; },
  };
}

test("inspector renders only when open, filters, and clears on demand", () => {
  const previousDocument = globalThis.document;
  globalThis.document = { createElement: () => element() };
  try {
    const ui = { advanced: element(), list: element(), count: element(),
      clearButton: element(), directionFilter: element(), kindFilter: element() };
    const inspector = new EventInspector(ui);
    inspector.record(frame("in", "json", "turn_end", 42));
    inspector.record(frame("out", "binary", "", 640));
    assert.equal(ui.list.children.length, 0, "closed Advanced must not render on audio events");

    ui.advanced.open = true;
    ui.advanced.toggle();
    assert.equal(ui.list.children.length, 2);
    assert.match(ui.count.textContent, /2 shown/);
    ui.directionFilter.value = "in";
    ui.directionFilter.change();
    assert.equal(ui.list.children.length, 1);
    assert.equal(ui.list.children[0].children[2].textContent, "turn_end");

    ui.clearButton.click();
    assert.equal(ui.list.children.length, 0);
    assert.equal(ui.clearButton.disabled, true);
    inspector.record(frame());
    ui.advanced.open = false;
    ui.advanced.toggle();
    assert.equal(inspector.timer, null, "closing Advanced cancels queued render");
  } finally {
    globalThis.document = previousDocument;
  }
});
