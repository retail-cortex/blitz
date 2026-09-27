// State shared across the window: the preferences (saved as they change),
// the theme in effect, and what each open workspace is doing (for the
// drawer's indicators).
import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { getPrefs, savePrefs } from "./desktop";
import { defaultPrefs, type Prefs } from "./prefs";
import { applyTheme, narrowQuery, onSystemThemeChange, resolveTheme, systemIsDark, type Theme } from "./theme";

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
  /** The drawer as shown: the user's choice, or a rail in a narrow window. */
  drawer: "open" | "rail";
  activity: Record<string, Activity>;
  setActivity: (dir: string, a: Activity) => void;
  prefsError: string;
  /** Registers how to stop dir's running turn (resolving once it ended). */
  registerStop: (dir: string, stop: (() => Promise<void>) | null) => void;
  stop: (dir: string) => Promise<void>;
}

const Ctx = createContext<AppState | null>(null);

export function AppStateProvider({ children }: { children: ReactNode }) {
  const [prefs, setPrefs] = useState<Prefs>(defaultPrefs);
  const [loaded, setLoaded] = useState(false);
  const [prefsError, setPrefsError] = useState("");
  const [systemDark, setSystemDark] = useState(systemIsDark);
  const [narrow, setNarrow] = useState(() => narrowQuery(980)?.matches ?? false);
  useEffect(() => {
    const q = narrowQuery(980);
    if (!q) return;
    const f = (e: MediaQueryListEvent) => setNarrow(e.matches);
    q.addEventListener("change", f);
    return () => q.removeEventListener("change", f);
  }, []);
  const [activity, setAllActivity] = useState<Record<string, Activity>>({});
  const saving = useRef<Promise<unknown>>(Promise.resolve());

  useEffect(() => {
    getPrefs()
      .then(setPrefs)
      .catch((e) => setPrefsError(String(e)))
      .finally(() => setLoaded(true));
  }, []);
  useEffect(() => onSystemThemeChange(setSystemDark), []);

  const theme = resolveTheme(prefs.theme, systemDark);
  useEffect(() => applyTheme(theme, prefs.density), [theme, prefs.density]);

  // Saves run one after another, so the file ends with the latest.
  const update = useCallback((f: (p: Prefs) => Prefs) => {
    setPrefs((p) => {
      const next = f(p);
      saving.current = saving.current.then(() => savePrefs(next).catch((e) => setPrefsError(String(e))));
      return next;
    });
  }, []);

  const setActivity = useCallback((dir: string, a: Activity) => {
    setAllActivity((all) => (all[dir]?.running === a.running && all[dir]?.waiting === a.waiting ? all : { ...all, [dir]: a }));
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
    <Ctx.Provider value={{ prefs, loaded, update, theme, drawer: narrow ? "rail" : prefs.drawer, activity, setActivity, prefsError, registerStop, stop }}>{children}</Ctx.Provider>
  );
}

export function useApp(): AppState {
  const s = useContext(Ctx);
  if (!s) throw new Error("useApp outside AppStateProvider");
  return s;
}
