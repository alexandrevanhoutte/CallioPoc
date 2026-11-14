import assert from "node:assert/strict";
import test from "node:test";
import { TranscriptStore } from "../web/transcript.js";

test("partial transcription updates in place and finalized segments persist", () => {
  const history = new TranscriptStore();
  const base = { type: "transcript", speaker: "CALLER", segment_id: "caller-1", timestamp: "2026-11-14T03:00:00Z" };
  assert.equal(history.upsert({ ...base, text: "Bon", final: false }).text, "Bon");
  assert.equal(history.upsert({ ...base, text: "Bonjour", final: true }).text, "Bonjour");
  assert.equal(history.upsert({ ...base, text: "duplicated", final: true }), null);
  history.upsert({ type: "transcript", speaker: "AI", segment_id: "ai-1", text: "Bonjour", final: false });
  assert.deepEqual(history.finalized().map(x => x.text), ["Bonjour"]);
  assert.deepEqual(history.snapshot().map(x => x.speaker), ["CALLER", "AI"]);
  assert.equal(history.snapshot().length, 2);
  history.clear();
  assert.equal(history.snapshot().length, 0);
});

test("malformed transcription and changed speaker cannot corrupt history", () => {
  const history = new TranscriptStore();
  assert.equal(history.upsert({ type: "transcript", speaker: "AI", text: "missing id" }), null);
  history.upsert({ type: "transcript", speaker: "AI", segment_id: "ai-1", text: "Bonjour" });
  assert.equal(history.upsert({ type: "transcript", speaker: "CALLER", segment_id: "ai-1", text: "wrong" }), null);
  assert.equal(history.snapshot()[0].text, "Bonjour");
});

test("late caller text is ordered before the assistant reply", () => {
  const history = new TranscriptStore();
  history.upsert({ type: "transcript", speaker: "AI", segment_id: "ai-1", order: 1, text: "Bonjour", final: true });
  history.upsert({ type: "transcript", speaker: "AI", segment_id: "ai-2", order: 3, text: "Quel entretien ?", final: true });
  history.upsert({ type: "transcript", speaker: "CALLER", segment_id: "caller-1", order: 2, text: "Je voudrais un entretien", final: false });
  history.upsert({ type: "transcript", speaker: "CALLER", segment_id: "caller-1", order: 2, text: "Je voudrais un entretien.", final: true });
  assert.deepEqual(history.snapshot().map(s => s.id), ["ai-1", "caller-1", "ai-2"]);
  assert.deepEqual(history.finalized().map(s => s.order), [1, 2, 3]);
});

test("handoff appears in transcript timeline and survives late updates", () => {
  const history = new TranscriptStore();
  history.upsert({ type: "transcript", speaker: "AI", segment_id: "ai-1", order: 1, text: "Bonjour", final: true });
  history.upsert({ type: "transcript", speaker: "AI", segment_id: "ai-3", order: 3, text: "Je vais noter la demande", final: true });
  assert.equal(history.addHandoff({
    type: "handoff_required",
    reason: "owner_requested",
    context: { caller_name: null, purpose: "Recruitment", summary: "Recruiter requests owner" },
  }), true);
  assert.equal(history.addHandoff({
    type: "handoff_required", reason: "owner_requested",
    context: { purpose: "Duplicate", summary: "Duplicate" },
  }), false);
  history.upsert({ type: "transcript", speaker: "CALLER", segment_id: "caller-2", order: 2, text: "Recrutement", final: true });
  history.upsert({ type: "transcript", speaker: "AI", segment_id: "ai-4", order: 4, text: "Merci", final: false });
  assert.deepEqual(history.timeline().map(x => x.type === "handoff" ? "handoff" : x.id),
    ["ai-1", "caller-2", "ai-3", "handoff", "ai-4"]);
  assert.equal(history.finalized().length, 3);
  assert.equal(history.timeline().find(x => x.type === "handoff").context.caller_name, null);
  history.clear();
  assert.equal(history.timeline().length, 0);
  assert.equal(history.handoff, null);
});

test("invalid handoff events do not affect transcript timeline", () => {
  const history = new TranscriptStore();
  assert.equal(history.addHandoff({ type: "handoff_required", reason: "unknown", context: {} }), false);
  assert.equal(history.addHandoff({ type: "handoff_required", reason: "ai_unable", context: { purpose: "", summary: "Text" } }), false);
  assert.deepEqual(history.timeline(), []);
});
