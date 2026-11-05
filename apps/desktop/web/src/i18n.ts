/**
 * Copyright 2026 Retail Cortex
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// The page's text, from the same catalogs as the terminal
// (pkg/i18n/locales, embedded at build time; the page's keys start
// with "desktop."). Lookups follow the same chain as the terminal's
// (fr-CA → fr → en-US; a bare language borrows a regional catalog),
// placeholders are {name}, and plurals pick ".one" or ".other" by the
// language's CLDR rules. The pseudo-locale en-XA shows English accented
// and bracketed, to find text that isn't in a catalog.
import { setTranslator } from "@blitz/webui";
import { useSyncExternalStore } from "react";
import enUS from "../../../../pkg/i18n/locales/en-US.json";
import es from "../../../../pkg/i18n/locales/es.json";
import frCA from "../../../../pkg/i18n/locales/fr-CA.json";
import fixedKeys from "../../../../pkg/i18n/fixed_keys.json";

interface Catalog {
  meta: { locale: string; name: string; english_name: string };
  messages: Record<string, string>;
}

const catalogs: Catalog[] = [enUS, es, frCA];
let byTag = new Map(catalogs.map((c) => [c.meta.locale.toLowerCase(), c]));
const english = enUS as Catalog;

const listLanguages = () => catalogs.map((c) => ({ tag: c.meta.locale, name: c.meta.name, english: c.meta.english_name || c.meta.name }));

/** The languages the window can show, for the setting. */
export let languages = listLanguages();

// The terminal's language setting ([ui] locale), which System tries
// before the system's own (BL-DSK-41).
let settingsLanguage = "";
let lastPref = "system";

/**
 * Takes the user's settings from the service: their [ui] locale, which
 * System follows, and their own catalogs (~/.blitz/locales), which add
 * languages or override the built-in translations, as in the terminal.
 */
export function applyUserLanguage(locale: string, userCatalogs: readonly string[]) {
  for (const raw of userCatalogs) {
    let c: Catalog;
    try {
      c = JSON.parse(raw) as Catalog;
    } catch {
      continue;
    }
    if (!c?.meta?.locale || !c.messages || typeof c.messages !== "object") continue;
    const known = byTag.get(c.meta.locale.toLowerCase());
    if (known) {
      known.messages = { ...known.messages, ...c.messages };
      if (c.meta.name) known.meta = { ...known.meta, name: c.meta.name };
    } else {
      catalogs.push({ meta: { locale: c.meta.locale, name: c.meta.name || c.meta.locale, english_name: c.meta.english_name || c.meta.locale }, messages: { ...c.messages } });
    }
  }
  byTag = new Map(catalogs.map((c) => [c.meta.locale.toLowerCase(), c]));
  languages = listLanguages();
  settingsLanguage = locale;
  current = "";
  setLanguage(lastPref);
}

/** The pseudo-locale: English, accented and bracketed, to spot text that isn't in a catalog. */
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

// The organization's name, said in place of Blitz's (spec_053 EN-51); ""
// for Blitz's own.
let appName = "";

/**
 * Whether key names the program itself, so keeps "Blitz" under an organization's name (EN-51): "Built on Blitz",
 * Blitz's releases, license, themes and its reinstall hints. The keys are pkg/i18n/fixed_keys.json, which the
 * service reads too; one ending in "." covers every key under it.
 */
export const fixedName = (key: string) => fixedKeys.some((k) => (k.endsWith(".") ? key.startsWith(k) : key === k));

/**
 * Says name in place of "Blitz" in the window's words, but where they name
 * the program itself ("" for Blitz's own).
 */
export function setAppName(name: string) {
  if (name === appName) return;
  appName = name;
  listeners.forEach((f) => f());
}

function rename(key: string, text: string): string {
  return appName && !fixedName(key) ? text.replace(/\bBlitz\b/g, () => appName) : text;
}

/**
 * Shows the window in tag's language ("system" follows the terminal's
 * language setting when there is one, else the system's).
 */
export function setLanguage(pref: string) {
  lastPref = pref;
  const system = typeof navigator === "undefined" ? [] : [...(navigator.languages ?? [navigator.language])];
  const next = resolveLanguage(pref, settingsLanguage ? [settingsLanguage, ...system] : system);
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
    if (text !== undefined) return fill(current === pseudoLocale ? pseudo(rename(key, text)) : rename(key, text), params);
  }
  return key;
}

/** The plural form of key for count (key.one or key.other), with {count} filled. */
export function tn(key: string, count: number, params?: Record<string, string | number>): string {
  const form = new Intl.PluralRules(language()).select(count) === "one" ? "one" : "other";
  return t(`${key}.${form}`, { count: count.toLocaleString(language()), ...params });
}

/**
 * Re-renders the component when the language or the organization's name
 * changes; returns the language, with the name after a "+" while there is
 * one, so an effect that says words can depend on it.
 */
export function useLanguage(): string {
  return useSyncExternalStore(
    (f) => {
      listeners.add(f);
      return () => listeners.delete(f);
    },
    () => (appName ? `${current}+${appName}` : current),
  );
}

// The shared controls (@blitz/webui) say their words through t.
setTranslator(t);
