// The page's text, from the same catalogs as the terminal
// (internal/i18n/locales, embedded at build time; the page's keys start
// with "desktop."). Lookups follow the same chain as the terminal's
// (fr-CA → fr → en-US; a bare language borrows a regional catalog),
// placeholders are {name}, and plurals pick ".one" or ".other" by the
// language's CLDR rules. The pseudo-locale en-XA shows English accented
// and bracketed, to find text that isn't in a catalog.
import { useSyncExternalStore } from "react";
import enUS from "../../../internal/i18n/locales/en-US.json";
import es from "../../../internal/i18n/locales/es.json";
import frCA from "../../../internal/i18n/locales/fr-CA.json";

interface Catalog {
  meta: { locale: string; name: string; english_name: string };
  messages: Record<string, string>;
}

const catalogs: Catalog[] = [enUS, es, frCA];
const byTag = new Map(catalogs.map((c) => [c.meta.locale.toLowerCase(), c]));
const english = enUS as Catalog;

/** The languages the window can show, for the setting. */
export const languages = catalogs.map((c) => ({ tag: c.meta.locale, name: c.meta.name, english: c.meta.english_name }));

export const pseudoLocale = "en-XA";

/** The catalogs to look in for tag, most specific first, English last. */
export function chain(tag: string): Catalog[] {
  const t = tag.toLowerCase();
  const base = t.split(/[-_]/)[0];
  const out: Catalog[] = [];
  const add = (c?: Catalog) => c && !out.includes(c) && out.push(c);
  add(byTag.get(t));
  add(byTag.get(base));
  // A bare language borrows the lowest-tag regional catalog (fr → fr-CA).
  add(catalogs.filter((c) => c.meta.locale.toLowerCase().startsWith(base + "-")).sort((a, b) => a.meta.locale.localeCompare(b.meta.locale))[0]);
  add(english);
  return out;
}

/** The tag to use for a preference: "system" is the system's language. */
export function resolveLanguage(pref: string, system: readonly string[]): string {
  if (pref === pseudoLocale) return pseudoLocale;
  const wanted = pref && pref !== "system" ? [pref] : system;
  for (const w of wanted) {
    const c = chain(w)[0];
    if (c !== english || w.toLowerCase().startsWith("en")) return c.meta.locale;
  }
  return english.meta.locale;
}

let current = "en-US";
let lookup = chain(current);
const listeners = new Set<() => void>();

/** Shows the window in tag's language ("system" follows the system). */
export function setLanguage(pref: string) {
  const next = resolveLanguage(pref, typeof navigator === "undefined" ? [] : navigator.languages ?? [navigator.language]);
  if (next === current) return;
  current = next;
  lookup = chain(next === pseudoLocale ? "en-US" : next);
  listeners.forEach((f) => f());
}

/** The language shown now (also for dates and numbers). */
export const language = () => (current === pseudoLocale ? "en-US" : current);

function fill(text: string, params?: Record<string, string | number>): string {
  if (!params) return text;
  return text.replace(/\{(\w+)\}/g, (m, name) => (name in params ? String(params[name]) : m));
}

const accents: Record<string, string> = { a: "á", e: "é", i: "í", o: "ó", u: "ú", A: "Á", E: "É", I: "Í", O: "Ó", U: "Ú", c: "ç", n: "ñ" };

/** The pseudo-locale's version of English: accented, bracketed, placeholders kept. */
export function pseudo(text: string): string {
  return "⟦" + text.replace(/\{\w+\}|[aeiouAEIOUcn]/g, (m) => (m.startsWith("{") ? m : accents[m])) + "⟧";
}

/** The text for key, with its placeholders filled. A missing key shows as the key. */
export function t(key: string, params?: Record<string, string | number>): string {
  for (const c of lookup) {
    const text = c.messages[key];
    if (text !== undefined) return fill(current === pseudoLocale ? pseudo(text) : text, params);
  }
  return key;
}

/** The plural form of key for count (key.one or key.other), with {count} filled. */
export function tn(key: string, count: number, params?: Record<string, string | number>): string {
  const form = new Intl.PluralRules(language()).select(count) === "one" ? "one" : "other";
  return t(`${key}.${form}`, { count: count.toLocaleString(language()), ...params });
}

/** Re-renders the component when the language changes; returns the language. */
export function useLanguage(): string {
  return useSyncExternalStore(
    (f) => {
      listeners.add(f);
      return () => listeners.delete(f);
    },
    () => current,
  );
}
