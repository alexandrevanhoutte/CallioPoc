import assert from "node:assert/strict";
import test from "node:test";
import { readFile } from "node:fs/promises";
import { SUPPORTED_LOCALES, FALLBACK_LOCALE, getLocale, setLocale, t } from "../web/i18n.js";
import { loadTestLocales } from "./i18n-fixture.mjs";

const catalogs = Object.fromEntries(await Promise.all(
  SUPPORTED_LOCALES.map(async locale => [locale, JSON.parse(await readFile(
    new URL("../web/locales/" + locale.slice(0, 2) + ".json", import.meta.url), "utf8"))])
));

test("translation catalogs have identical stable keys and placeholder names", () => {
  assert.equal(getLocale(), "fr-FR", "first run defaults to French");
  assert.deepEqual(SUPPORTED_LOCALES, ["fr-FR", "en-US", "ko-KR"]);
  assert.equal(FALLBACK_LOCALE, "en-US");
  const english = catalogs["en-US"];
  for (const locale of SUPPORTED_LOCALES) {
    const dict = catalogs[locale];
    assert.deepEqual(Object.keys(dict).sort(), Object.keys(english).sort(), "missing key for " + locale);
    for (const [key, translated] of Object.entries(dict)) {
      assert.match(key, /^[a-z][a-zA-Z]*(?:\.[a-z][a-zA-Z]*)+$/, "invalid key: " + key);
      assert.ok(typeof translated === "string" && translated.trim(), "empty translation: " + key);
      const slots = text => [...text.matchAll(/\{([a-z_]+)\}/g)].map(x => x[1]).sort();
      assert.deepEqual(slots(translated), slots(english[key]), "placeholder mismatch: " + key);
    }
  }
});

await loadTestLocales();

test("locale switch translates stable keys and interpolates named placeholders", () => {
  const original = getLocale();
  try {
    assert.equal(setLocale("fr-FR", { persist: false }), true);
    assert.equal(t("common.unknown"), "Inconnu");
    assert.equal(t("call.completedFeedback", { reason: "test" }), "Appel terminé · test · micro et IA désactivés.");
    assert.equal(setLocale("ko-KR", { persist: false }), true);
    assert.equal(t("common.unknown"), "알 수 없음");
    assert.equal(t("urgency.high"), "높음");
    assert.equal(setLocale("en-US", { persist: false }), true);
    assert.equal(t("common.unknown"), "Unknown");
    assert.equal(t("unlisted.string"), "unlisted.string");
    assert.equal(setLocale("unsupported", { persist: false }), false);
    assert.equal(getLocale(), "en-US");
  } finally { setLocale(original, { persist: false }); }
});

test("static HTML translations use declared keys and do not scan English text", async () => {
  const html = await readFile(new URL("../web/index.html", import.meta.url), "utf8");
  const refs = [...html.matchAll(/data-i18n(?:-[a-z-]+)?="([a-z][a-zA-Z]*(?:\.[a-z][a-zA-Z]*)+)"/g)].map(x => x[1]);
  assert.ok(refs.length > 30, "expected explicit translation attributes");
  for (const key of refs) assert.ok(Object.hasOwn(catalogs["en-US"], key), "missing HTML translation: " + key);
  for (const path of ["app.js", "setup.js", "transcript.js", "summary.js", "event-inspector.js"]) {
    const source = await readFile(new URL("../web/" + path, import.meta.url), "utf8");
    for (const match of source.matchAll(/\bt\("([a-z][a-zA-Z]*(?:\.[a-z][a-zA-Z]*)+)"/g)) {
      assert.ok(Object.hasOwn(catalogs["en-US"], match[1]), path + ": unknown translation " + match[1]);
    }
  }
});
