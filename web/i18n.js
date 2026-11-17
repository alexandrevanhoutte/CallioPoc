// Browser localization without a framework or build step.
// Translation IDs are stable; one JSON dictionary per language.
export const SUPPORTED_LOCALES = ["fr-FR", "en-US", "ko-KR"];
export const FALLBACK_LOCALE = "en-US";
const DEFAULT_LOCALE = "fr-FR";
const STORAGE_KEY = "callio.locale";
let catalogs = {};
let locale = DEFAULT_LOCALE;
let stored = false;
try {
  const saved = localStorage.getItem(STORAGE_KEY);
  if (SUPPORTED_LOCALES.includes(saved)) { locale = saved; stored = true; }
} catch { /* Local storage may be unavailable. */ }

export function getLocale() { return locale; }
export function hasStoredLocale() { return stored; }

// Load dictionaries once, before importing app.js. This keeps t() synchronous
// during audio streaming, event handling and UI language switches.
export async function loadLocales(fetcher = fetch) {
  const loaded = await Promise.all(SUPPORTED_LOCALES.map(async name => {
    const response = await fetcher("/locales/" + name.slice(0, 2) + ".json");
    if (!response.ok) throw new Error("Unable to load translations for " + name);
    const messages = await response.json();
    if (!messages || typeof messages !== "object" || Array.isArray(messages)) {
      throw new Error("Invalid translation catalog for " + name);
    }
    return [name, messages];
  }));
  catalogs = Object.fromEntries(loaded);
}

export function t(key, params = {}) {
  const source = catalogs[locale]?.[key] ?? catalogs[FALLBACK_LOCALE]?.[key] ?? key;
  return source.replace(/\{([a-z_]+)\}/g, (match, name) =>
    Object.hasOwn(params, name) ? String(params[name]) : match);
}

export function setLocale(value, { persist = true } = {}) {
  if (!SUPPORTED_LOCALES.includes(value)) return false;
  locale = value;
  if (persist) {
    stored = true;
    try { localStorage.setItem(STORAGE_KEY, value); } catch { /* Optional preference. */ }
  }
  if (typeof document !== "undefined") localizePage();
  return true;
}

// Only elements explicitly marked in HTML are translated.
// No scanning of English text nodes; dynamic content stays owned by its view.
export function localizePage() {
  if (typeof document === "undefined") return;
  document.documentElement.lang = locale;
  document.title = t("page.title");
  for (const element of document.querySelectorAll("[data-i18n]")) {
    element.textContent = t(element.dataset.i18n);
  }
  for (const attribute of ["aria-label", "data-help", "title", "placeholder", "content"]) {
    const marker = "data-i18n-" + attribute;
    for (const element of document.querySelectorAll("[" + marker + "]")) {
      element.setAttribute(attribute, t(element.getAttribute(marker)));
    }
  }
}
