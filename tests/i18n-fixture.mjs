import { readFile } from "node:fs/promises";
import { loadLocales } from "../web/i18n.js";

export async function loadTestLocales() {
  await loadLocales(async path => ({
    ok: true,
    json: async () => JSON.parse(await readFile(new URL("../web" + path, import.meta.url), "utf8")),
  }));
}
