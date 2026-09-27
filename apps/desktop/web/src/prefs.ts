// The window's own settings and the workspaces it knows, as the Go side
// keeps them in ~/.blitz/desktop.json. The helpers here are pure, so the
// rules for opening, closing and editing workspaces are tested alone.
import type { ThemePref } from "./theme";

export interface WorkspacePrefs {
  dir: string;
  name?: string; // "" or unset shows the directory's name
  description?: string;
  color?: string;
  open: boolean;
}

export interface Prefs {
  theme: ThemePref;
  workspaces: WorkspacePrefs[];
  active?: string;
  drawer: "open" | "rail";
  run_settings: boolean;
  show_thoughts: boolean;
  density: "comfortable" | "compact";
  notifications: "on" | "off";
  /** The window's language: "system" (the default) or a catalog's tag. */
  language: string;
}

export const defaultPrefs: Prefs = {
  theme: "system",
  workspaces: [],
  drawer: "open",
  run_settings: false,
  show_thoughts: false,
  density: "comfortable",
  notifications: "on",
  language: "system",
};

const pick = <T extends string>(v: unknown, allowed: readonly T[], fallback: T): T => (allowed.includes(v as T) ? (v as T) : fallback);

/**
 * Makes preferences from whatever was loaded (the Go side, localStorage,
 * or an older file): every field of the right type, else its default.
 */
export function normalizePrefs(raw: unknown): Prefs {
  const r = (raw && typeof raw === "object" ? raw : {}) as Record<string, unknown>;
  const list = Array.isArray(r.workspaces) ? r.workspaces : [];
  const workspaces: WorkspacePrefs[] = list
    .filter((w): w is Record<string, unknown> => !!w && typeof w === "object" && typeof (w as { dir?: unknown }).dir === "string" && (w as { dir: string }).dir !== "")
    .map((w) => ({
      dir: w.dir as string,
      name: typeof w.name === "string" ? w.name : undefined,
      description: typeof w.description === "string" ? w.description : undefined,
      color: typeof w.color === "string" ? w.color : undefined,
      open: w.open === true,
    }));
  const active = typeof r.active === "string" && workspaces.some((w) => w.open && w.dir === r.active) ? r.active : workspaces.find((w) => w.open)?.dir;
  return {
    theme: pick(r.theme, ["system", "light", "dark"] as const, "system"),
    workspaces,
    active,
    drawer: pick(r.drawer, ["open", "rail"] as const, "open"),
    run_settings: r.run_settings === true,
    show_thoughts: r.show_thoughts === true,
    density: pick(r.density, ["comfortable", "compact"] as const, "comfortable"),
    notifications: pick(r.notifications, ["on", "off"] as const, "on"),
    language: typeof r.language === "string" && /^[A-Za-z]{2,3}(-[A-Za-z0-9]+)*$|^system$/.test(r.language) ? r.language : "system",
  };
}

/** The last element of a directory path. */
export function baseName(dir: string): string {
  const parts = dir.replace(/[/\\]+$/, "").split(/[/\\]/);
  return parts[parts.length - 1] || dir;
}

/** The name a workspace is shown by. */
export function displayName(w: Pick<WorkspacePrefs, "dir" | "name">): string {
  return w.name?.trim() || baseName(w.dir);
}

export const openWorkspaces = (p: Prefs) => p.workspaces.filter((w) => w.open);
export const recentWorkspaces = (p: Prefs) => p.workspaces.filter((w) => !w.open);

/** Opens dir (keeping what the window remembers about it) and shows it. */
export function openWorkspace(p: Prefs, dir: string): Prefs {
  const known = p.workspaces.find((w) => w.dir === dir);
  if (known?.open) return { ...p, active: dir };
  const rest = p.workspaces.filter((w) => w.dir !== dir);
  const opened = { ...(known ?? { dir }), open: true };
  const open = rest.filter((w) => w.open);
  return { ...p, workspaces: [...open, opened, ...rest.filter((w) => !w.open)], active: dir };
}

/**
 * Closes dir: it moves to the front of the recent ones, and the tab next
 * to it (after, else before) is shown if it was.
 */
export function closeWorkspace(p: Prefs, dir: string): Prefs {
  const open = openWorkspaces(p);
  const i = open.findIndex((w) => w.dir === dir);
  if (i < 0) return p;
  const closed = { ...open[i], open: false };
  const stillOpen = open.filter((w) => w.dir !== dir);
  let active = p.active;
  if (active === dir) active = (open[i + 1] ?? open[i - 1])?.dir;
  return { ...p, workspaces: [...stillOpen, closed, ...recentWorkspaces(p).filter((w) => w.dir !== dir)], active };
}

/** Forgets a closed workspace. */
export function forgetWorkspace(p: Prefs, dir: string): Prefs {
  return { ...p, workspaces: p.workspaces.filter((w) => w.open || w.dir !== dir) };
}

/** Changes a workspace's name, description or colour. */
export function editWorkspace(p: Prefs, dir: string, change: Partial<Pick<WorkspacePrefs, "name" | "description" | "color">>): Prefs {
  return {
    ...p,
    workspaces: p.workspaces.map((w) =>
      w.dir === dir
        ? {
            ...w,
            ...change,
            name: (change.name ?? w.name)?.trim(),
            description: (change.description ?? w.description)?.trim(),
          }
        : w,
    ),
  };
}

/** Moves an open workspace to position to among the open ones. */
export function moveWorkspace(p: Prefs, dir: string, to: number): Prefs {
  const open = openWorkspaces(p);
  const from = open.findIndex((w) => w.dir === dir);
  if (from < 0) return p;
  const [w] = open.splice(from, 1);
  open.splice(Math.max(0, Math.min(to, open.length)), 0, w);
  return { ...p, workspaces: [...open, ...recentWorkspaces(p)] };
}
