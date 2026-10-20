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

import {
  mdiAutoFix,
  mdiBookOpenPageVariantOutline,
  mdiChevronDown,
  mdiClose,
  mdiCogOutline,
  mdiDeleteOutline,
  mdiFolderOpenOutline,
  mdiFolderOutline,
  mdiGit,
  mdiHistory,
  mdiInformationOutline,
  mdiLightningBolt,
  mdiPencilOutline,
  mdiSourceBranch,
  mdiSourceCommit,
  mdiWebPlus,
} from "@mdi/js";
import type { ChangesSource } from "./Changes";
import { canPreview } from "./desktop";
import { openDocs, openWeb, startSetup } from "./events";
import { docSections } from "./docs/sections";
import { t, tn } from "./i18n";
import { displayName, forgetWorkspace, openWorkspace, openWorkspaces, recentWorkspaces } from "./prefs";
import type { Section } from "./SettingsDrawer";
import { studioGroups, studioIcons, type StudioKind } from "./studio/StudioDrawer";
import { useApp } from "./state";
import { menuIds } from "./simpleMode";
import { workspaceColor } from "./palette";
import { Icon, IconButton, Menu, ToolbarSearch, type MenuEntry } from "./ui/controls";
import { useWorkerFailures } from "./workerFailures";

/** The app's name and mark, at the left of the top bar. */
export function Brand() {
  return (
    <span className="brand">
      <Icon path={mdiLightningBolt} className="brand-mark" />
      <span className="brand-name">{t("desktop.brand")}</span>
    </span>
  );
}

/**
 * The top bar's menus, from the left edge of the center panel: Workspaces
 * (open one, switch, edit its settings, close), Changes (the session's or
 * git's), Studio (a button: the workspace's agents, skills, workers and
 * the rest, in one drawer), Help; at the right, the
 * search (the command palette) and Settings (the global settings, an icon).
 * What they open shows over the editor, in a dialog. Without a workspace
 * (the welcome screen) only Workspaces, Help, the search and Settings
 * remain.
 */
export function MenuBar({
  dir = "",
  onChanges,
  onCommit,
  onHistory,
  onStudio,
  studioKind = null,
  onCloseStudio,
  onSearch,
  onSettings,
  onOpenWorkspace,
  onCloseWorkspace,
}: {
  dir?: string;
  onChanges?: (s: ChangesSource) => void;
  /** Opens the commit dialog (Changes › Commit…). */
  onCommit?: () => void;
  /** Shows the workspace's history (View revisions for every file). */
  onHistory?: () => void;
  /** Opens Studio on a kind, or shows that kind when it's open. */
  onStudio?: (kind: StudioKind) => void;
  /** The kind Studio shows, null when it's closed: the menu ticks it, and offers Close Studio. */
  studioKind?: StudioKind | null;
  /** Closes Studio, as its ✕ does. */
  onCloseStudio?: () => void;
  onSearch: () => void;
  onSettings: (section: Section, scope?: string) => void;
  onOpenWorkspace: () => void;
  onCloseWorkspace: (dir: string) => void;
}) {
  const { prefs, update, activity, theme } = useApp();
  // Simple mode keeps Workspaces, Changes and Help (simpleMode.ts).
  const advanced = prefs.advanced;
  const menus = menuIds(advanced);
  const failures = useWorkerFailures();
  const failed = dir ? (failures[dir] ?? 0) : 0;

  // Studio: its kinds, as the drawer lists them (icons only there), a
  // failed worker run noted on Workers; Close Studio while it's open.
  const studio: MenuEntry[] = [
    ...studioGroups.flatMap((group, i): MenuEntry[] => [
      ...(i > 0 ? (["divider"] as MenuEntry[]) : []),
      ...group.map((k) => ({
        label: t(`desktop.studio.${k}`),
        icon: studioIcons[k],
        on: studioKind === k,
        detail: k === "workers" && failed > 0 ? tn("desktop.switcher.failed_runs", failed) : undefined,
        onSelect: () => onStudio?.(k),
      })),
    ]),
    ...(studioKind ? (["divider", { label: t("desktop.studio.close"), icon: mdiClose, onSelect: () => onCloseStudio?.() }] as MenuEntry[]) : []),
  ];

  const changes: MenuEntry[] = [
    { label: t("desktop.menu.changes.session"), detail: t("desktop.changes.session"), icon: mdiHistory, onSelect: () => onChanges?.("session") },
    { label: t("desktop.menu.changes.git"), icon: mdiGit, onSelect: () => onChanges?.("git") },
  ];
  if (onHistory) changes.push({ label: t("desktop.menu.changes.history"), icon: mdiSourceBranch, onSelect: onHistory });
  if (onCommit) changes.push("divider", { label: t("desktop.commit.open"), icon: mdiSourceCommit, onSelect: onCommit });

  const status = (w: { dir: string }) => {
    const a = activity[w.dir];
    const n = failures[w.dir] ?? 0;
    return a?.waiting ? t("desktop.switcher.waiting") : a?.running ? t("desktop.switcher.working") : n > 0 ? tn("desktop.switcher.failed_runs", n) : "";
  };
  const recent = recentWorkspaces(prefs).slice(0, 8);
  const workspaceItems: MenuEntry[] = [
    { label: t("desktop.menu.workspaces.add"), icon: mdiFolderOpenOutline, onSelect: onOpenWorkspace },
    "divider",
    ...openWorkspaces(prefs).map(
      (w): MenuEntry => ({
        label: displayName(w),
        detail: status(w) || w.description || w.dir,
        icon: mdiFolderOutline,
        iconColor: workspaceColor(w.color, theme),
        on: w.dir === prefs.active,
        onSelect: () => update((p) => openWorkspace(p, w.dir)),
        actions: [
          // Its settings: Settings › Workspaces, on it.
          { icon: mdiPencilOutline, label: t("desktop.menu.workspaces.edit", { name: displayName(w) }), onSelect: () => onSettings("workspaces", w.dir) },
          { icon: mdiClose, label: t("desktop.switcher.close", { name: displayName(w) }), onSelect: () => onCloseWorkspace(w.dir) },
        ],
      }),
    ),
    ...(recent.length
      ? [
          { heading: t("desktop.recent") },
          ...recent.map(
            (w): MenuEntry => ({
              label: displayName(w),
              detail: w.description || w.dir,
              icon: mdiFolderOutline,
              iconColor: workspaceColor(w.color, theme),
              onSelect: () => update((p) => openWorkspace(p, w.dir)),
              actions: [{ icon: mdiDeleteOutline, label: t("desktop.forget"), removes: true, onSelect: () => update((p) => forgetWorkspace(p, w.dir)) }],
            }),
          ),
        ]
      : []),
  ];
  const othersWaiting = openWorkspaces(prefs).some((w) => w.dir !== prefs.active && activity[w.dir]?.waiting);

  const help: MenuEntry[] = [
    // The docs of this release, in the app (DSK-103).
    { label: t("desktop.menu.guide"), icon: mdiBookOpenPageVariantOutline, onSelect: () => openDocs({}) }, // where it was left, Tutorials the first time
    "divider",
    // Each of the guide's sections, a link of its own.
    ...docSections.map((d) => ({ label: t(d.labelKey), icon: d.icon, onSelect: () => openDocs({ page: d.page }) })),
    "divider",
    ...(dir ? [{ label: t("desktop.setup.title"), icon: mdiAutoFix, onSelect: () => startSetup(dir) }] : []),
    { label: t("desktop.menu.about"), icon: mdiInformationOutline, onSelect: () => onSettings("about") },
  ];

  return (
    <nav className="menubar no-drag" aria-label={t("desktop.menu.label")}>
      <Menu className="menubar-menu wide-menu" trigger={(p) => <MenuButton label={t("desktop.menu.workspaces")} dot={othersWaiting} {...p} />} items={workspaceItems} />
      {dir && (
        <>
          {/* Simple mode: Changes is this session's, a button; Git is advanced. */}
          {advanced ? (
            <Menu className="menubar-menu" trigger={(p) => <MenuButton label={t("desktop.view.changes")} {...p} />} items={changes} />
          ) : (
            <button type="button" className="menubar-item" onClick={() => onChanges?.("session")}>
              {t("desktop.view.changes")}
            </button>
          )}
          {/* Studio: everything the workspace's .agents folder holds, in one drawer; a dot when a worker failed. */}
          {menus.includes("studio") && <Menu className="menubar-menu" trigger={(p) => <MenuButton label={t("desktop.studio")} dot={failed > 0} {...p} />} items={studio} />}
        </>
      )}
      <Menu className="menubar-menu" trigger={(p) => <MenuButton label={t("desktop.menu.help")} {...p} />} items={help} />
      <span className="spacer" />
      <ToolbarSearch onClick={onSearch} />
      {/* A web page in the editor (EL-102), from anywhere: the editor's own button shows only with a file open. */}
      {dir && canPreview() && <IconButton icon={mdiWebPlus} label={t("desktop.web.open")} onClick={() => openWeb({ dir, url: "" })} />}
      <IconButton icon={mdiCogOutline} label={t("desktop.menu.settings_hint")} onClick={() => onSettings("providers")} />
    </nav>
  );
}

/** A menu's button in the bar: its name, a badge or dot for what waits there, and a chevron. */
function MenuButton({
  label,
  badge,
  dot,
  ...rest
}: {
  label: string;
  badge?: { n: number; waiting: boolean };
  dot?: boolean;
  onClick: () => void;
  "aria-expanded": boolean;
  "aria-haspopup": "menu";
}) {
  return (
    <button type="button" className="menubar-item" {...rest}>
      {label}
      {badge && <span className={`badge menubar-badge ${badge.waiting ? "waiting" : ""}`}>{badge.n}</span>}
      {!badge && dot && <span className="dot menubar-dot" />}
      <Icon path={mdiChevronDown} size="sm" className="menubar-chevron" />
    </button>
  );
}

