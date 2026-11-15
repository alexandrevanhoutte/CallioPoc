import assert from "node:assert/strict";
import test from "node:test";
import { TranscriptView } from "../web/transcript.js";
import { SummaryView } from "../web/summary.js";

class FakeNode {
  constructor(tag = "div") {
    this.tag = tag;
    this.children = [];
    this.className = "";
    this.textContent = "";
    this.hidden = false;
    this.scrollTop = 0;
    this.scrollHeight = 100;
    this.classes = new Set();
    this.attributes = new Map();
    this.classList = {
      toggle: (name, enabled) => {
        if (enabled) this.classes.add(name);
        else this.classes.delete(name);
      },
    };
  }

  setAttribute(name, value) { this.attributes.set(name, value); }
  append(...nodes) { this.children.push(...nodes); }
  replaceChildren(...nodes) { this.children = [...nodes]; }
  querySelector(selector) {
    return this.children.find(node =>
      selector.startsWith(".") ? node.className.split(" ").includes(selector.slice(1))
        : node.tag === selector
    ) ?? null;
  }
}

// Node has no DOM; this minimal fixture only supports the view operations tested.
globalThis.document ??= { createElement: tag => new FakeNode(tag) };

function view() {
  const container = new FakeNode();
  const status = new FakeNode();
  const announcement = new FakeNode();
  return { container, status, announcement, transcript: new TranscriptView(container, status, announcement) };
}

test("provisional text is visible but announced only when finalized", () => {
  const { transcript, container, status, announcement } = view();
  transcript.begin(true);
  assert.match(status.textContent, /Listening/);
  const base = { type: "transcript", segment_id: "caller-1", speaker: "CALLER", order: 1 };
  const restore = globalThis.document;
  globalThis.document = { createElement: tag => new FakeNode(tag) };
  try {
    transcript.update({ ...base, text: "Bon", final: false });
    assert.equal(announcement.textContent, "");
    assert.equal(container.children.length, 1);
    assert.equal(container.children[0].querySelector("p").textContent, "Bon");
    assert.equal(container.children[0].classes.has("transcript-partial"), true);
    transcript.update({ ...base, text: "Bonjour", final: true });
    assert.equal(container.children.length, 1);
    assert.equal(announcement.textContent, "Caller: Bonjour");
    assert.equal(container.children[0].classes.has("transcript-partial"), false);
    transcript.update({ ...base, text: "duplicate", final: true });
    assert.equal(announcement.textContent, "Caller: Bonjour");
    transcript.finish();
    assert.match(status.textContent, /Conversation ended/);
    transcript.clear();
    assert.equal(container.children.length, 0);
    assert.equal(announcement.textContent, "");
  } finally {
    globalThis.document = restore;
  }
});

test("late transcript updates preserve order and do not repeat messages", () => {
  const { transcript, container } = view();
  const restore = globalThis.document;
  globalThis.document = { createElement: tag => new FakeNode(tag) };
  try {
    transcript.update({ type: "transcript", segment_id: "a", speaker: "AI", text: "Hello", order: 3, final: true });
    transcript.update({ type: "transcript", segment_id: "b", speaker: "CALLER", text: "Hi", order: 2, final: true });
    assert.deepEqual(container.children.map(row => row.querySelector("p").textContent), ["Hi", "Hello"]);
    transcript.update({ type: "transcript", segment_id: "b", speaker: "CALLER", text: "Duplicate", order: 2, final: true });
    assert.equal(container.children.length, 2);
  } finally {
    globalThis.document = restore;
  }
});

test("simulated handoff stays pending until a confirmed outcome", () => {
  const { transcript, container } = view();
  const restore = globalThis.document;
  globalThis.document = { createElement: tag => new FakeNode(tag) };
  try {
    const event = {
      type: "handoff_required",
      reason: "owner_requested",
      context: { purpose: "Recruitment", summary: "Wants to speak to the owner" },
    };
    transcript.addHandoff(event);
    assert.equal(container.children.length, 1);
    const card = container.children[0];
    const status = card.querySelector(".handoff-result");
    assert.match(status.textContent, /Awaiting your decision/);
    transcript.resolveHandoff("unknown");
    assert.match(status.textContent, /Awaiting your decision/);
    transcript.resolveHandoff("decline");
    assert.match(status.textContent, /declined/);
    transcript.clear();
    assert.equal(container.children.length, 0);
    transcript.addHandoff(event);
    assert.match(container.children[0].querySelector(".handoff-result").textContent, /Awaiting/);
  } finally {
    globalThis.document = restore;
  }
});

function summaryView() {
  const refs = Object.fromEntries(
    ["panel", "status", "details", "caller", "reason", "urgency", "handoff", "text", "actions"]
      .map(key => [key, new FakeNode()])
  );
  return { refs, summary: new SummaryView(refs) };
}

test("completed call summary resolves success and unavailable states", () => {
  const { refs, summary } = summaryView();
  summary.reset();
  assert.equal(refs.panel.hidden, true);
  summary.loading();
  assert.equal(refs.panel.hidden, false);
  assert.match(refs.status.textContent, /Generating/);
  summary.unavailable();
  assert.match(refs.status.textContent, /Summary unavailable/);
  assert.equal(refs.details.hidden, true);
  summary.render({
    type: "call_summary",
    summary: {
      summary: "Un recruteur a appelé.",
      caller_name: null,
      reason: "Recruitment",
      urgency: "normal",
      handoff_requested: true,
      follow_up_actions: ["Reply to recruiter"],
    },
  });
  assert.equal(refs.details.hidden, false);
  assert.equal(refs.caller.textContent, "Unknown");
  assert.equal(refs.reason.textContent, "Recruitment");
  assert.equal(refs.handoff.textContent, "Yes");
  assert.equal(refs.text.textContent, "Un recruteur a appelé.");
  assert.equal(refs.actions.children.length, 1);
  assert.equal(refs.actions.children[0].textContent, "Reply to recruiter");
  summary.unavailable();
  assert.equal(refs.details.hidden, false, "a late disconnect cannot erase an available summary");
  summary.reset();
  assert.equal(refs.panel.hidden, true);
  assert.equal(refs.actions.children.length, 1, "hidden content is replaced on next summary");
});

test("summary error and a dropped socket do not trap the loading state", () => {
  const { refs, summary } = summaryView();
  summary.reset();
  summary.loading();
  summary.render({ type: "summary_error", error: "Model unavailable" });
  assert.match(refs.status.textContent, /Summary unavailable/);
  assert.equal(refs.details.hidden, true);
  summary.reset();
  summary.loading();
  summary.unavailable();
  assert.match(refs.status.textContent, /Summary unavailable/);
});
