// The page's theme: the user's choice, or the system's when they chose
// "system" (the default), applied as html[data-theme] and kept in step
// with the system's setting as it changes.

export type ThemePref = "system" | "light" | "dark";
export type Theme = "light" | "dark";

/** The theme to show for a preference, given whether the system is dark. */
export function resolveTheme(pref: ThemePref, systemDark: boolean): Theme {
  if (pref === "light" || pref === "dark") return pref;
  return systemDark ? "dark" : "light";
}

const darkQuery = () => window.matchMedia?.("(prefers-color-scheme: dark)");

/** Whether the system is in dark mode now. */
export function systemIsDark(): boolean {
  return darkQuery()?.matches ?? false;
}

/** Applies the theme and density to the document. */
export function applyTheme(theme: Theme, density: "comfortable" | "compact" = "comfortable") {
  const root = document.documentElement;
  root.dataset.theme = theme;
  root.dataset.density = density;
}

/** Calls f whenever the system switches between light and dark. */
export function onSystemThemeChange(f: (dark: boolean) => void): () => void {
  const q = darkQuery();
  if (!q) return () => {};
  const listener = (e: MediaQueryListEvent) => f(e.matches);
  q.addEventListener("change", listener);
  return () => q.removeEventListener("change", listener);
}
