import { loadLocales } from "./i18n.js";

await loadLocales();
await import("./app.js");
