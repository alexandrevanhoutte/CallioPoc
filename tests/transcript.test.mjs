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
