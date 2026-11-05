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

// State shared across the window: the preferences (saved as they change),
// the theme in effect, and what each open workspace is doing (for the
// workspace dropdown's indicators).
import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { getPrefs, savePrefs } from "./desktop";
import { setLanguage, t } from "./i18n";
import { defaultPrefs, PrefsKeeper, type Prefs } from "./prefs";
import { applyDocCss, docCssFor } from "./files/docCss";
import { applyTheme, onSystemThemeChange, paletteOf, resolveTheme, systemIsDark, type Theme } from "./theme";

/** What a workspace is doing: a turn running, or waiting for the user. */
export interface Activity {
  running: boolean;
  waiting: boolean;
}

interface AppState {
  prefs: Prefs;
  loaded: boolean;
  update: (f: (p: Prefs) => Prefs) => void;
  theme: Theme;
  activity: Record<string, Activity>;
  /** Sets what dir is doing; null forgets it (the workspace closed). */
  setActivity: (dir: string, a: Activity | null) => void;
  prefsError: string;
  /** Registers how to stop dir's running turn (resolving once it ended). */
  registerStop: (dir: string, stop: (() => Promise<void>) | null) => void;
  stop: (dir: string) => Promise<void>;
}

/** all with dir's activity set to a (null: dropped), the same object when nothing changed. */
export function nextActivity(all: Record<string, Activity>, dir: string, a: Activity | null): Record<string, Activity> {
  if (!a) {
    if (!(dir in all)) return all;
    const rest = { ...all };
    delete rest[dir];
    return rest;
  }
  return all[dir]?.running === a.running && all[dir]?.waiting === a.waiting ? all : { ...all, [dir]: a };
}

const Ctx = createContext<AppState | null>(null);

/**
 * Provides the window's shared state: loads the preferences, applies the
 * theme and language, and tracks each workspace's activity.
 */
export function AppStateProvider({ children }: { children: ReactNode }) {
  const [prefs, setPrefs] = useState<Prefs>(defaultPrefs);
  const [loaded, setLoaded] = useState(false);
  const [prefsError, setPrefsError] = useState("");
  const [systemDark, setSystemDark] = useState(systemIsDark);
  const [activity, setAllActivity] = useState<Record<string, Activity>>({});
  // Holds changes until the preferences load, and saves nothing when
  // they couldn't be read (so the defaults never replace the file).
  const [keeper] = useState(() => new PrefsKeeper({ show: setPrefs, save: savePrefs, error: (e) => setPrefsError(String(e)) }));

  useEffect(() => {
    getPrefs()
      .then(
        (p) => keeper.loaded(p),
        (e) => keeper.unread(t("desktop.app.prefs_unread", { error: String(e) })),
      )
      .finally(() => setLoaded(true));
  }, [keeper]);
  useEffect(() => onSystemThemeChange(setSystemDark), []);

  const theme = resolveTheme(prefs.theme, systemDark);
  // Before the screen is painted, so no text shows in the wrong language.
  useLayoutEffect(() => setLanguage(prefs.language), [prefs.language]);
  const palette = paletteOf(theme, prefs.light_theme, prefs.dark_theme);
  useEffect(() => applyTheme(theme, prefs.density, prefs.width, prefs.transparency, prefs.theme, palette), [theme, prefs.density, prefs.width, prefs.transparency, prefs.theme, palette]);
  // The user's CSS for Markdown previews, the theme in effect's.
  const docCss = docCssFor(prefs, theme);
  useEffect(() => applyDocCss(docCss), [docCss]);

  // Saves run one after another, so the file ends with the latest.
  const update = useCallback((f: (p: Prefs) => Prefs) => keeper.update(f), [keeper]);

  const setActivity = useCallback((dir: string, a: Activity | null) => {
    setAllActivity((all) => nextActivity(all, dir, a));
  }, []);

  const stoppers = useRef(new Map<string, () => Promise<void>>());
  const registerStop = useCallback((dir: string, f: (() => Promise<void>) | null) => {
    if (f) stoppers.current.set(dir, f);
    else stoppers.current.delete(dir);
  }, []);
  const stop = useCallback(async (dir: string) => {
    await stoppers.current.get(dir)?.();
  }, []);

  return (
    <Ctx.Provider value={{ prefs, loaded, update, theme, activity, setActivity, prefsError, registerStop, stop }}>{children}</Ctx.Provider>
  );
}

/** The window's shared state (inside AppStateProvider). */
export function useApp(): AppState {
  const s = useContext(Ctx);
  if (!s) throw new Error("useApp outside AppStateProvider");
  return s;
}

/** Whether advanced settings are shown (Settings › Appearance; simpleMode.ts). */
export function useAdvanced(): boolean {
  return useApp().prefs.advanced;
}
