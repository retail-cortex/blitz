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

import { useState } from "react";
import { createPortal } from "react-dom";
import {
  mdiAutoFix,
  mdiBookOpenPageVariantOutline,
  mdiChevronDown,
  mdiClose,
  mdiCogOutline,
  mdiDeleteOutline,
  mdiFolderOpenOutline,
  mdiFolderAccountOutline,
  mdiFolderPlusOutline,
  mdiFolderSyncOutline,
  mdiFolderOutline,
  mdiGit,
  mdiHistory,
  mdiInformationOutline,
  mdiLightningBolt,
  mdiPencilOutline,
  mdiSourceBranch,
  mdiPlusBoxMultipleOutline,
  mdiShieldKeyOutline,
  mdiSourceCommit,
  mdiWebPlus,
} from "@mdi/js";
import type { ChangesSource } from "./Changes";
import { canPreview } from "./desktop";
import { configChanged, openDocs, openWeb, startSetup } from "./events";
import { OrgWorkspaceDialog } from "./OrgWorkspaceDialog";
import { docSections } from "./docs/sections";
import { t, tn } from "./i18n";
import { displayName, forgetWorkspace, openWorkspace, openWorkspaces, recentWorkspaces } from "./prefs";
import type { Section } from "./SettingsDrawer";
import { studioGroups, studioIcons, type StudioKind } from "./studio/StudioDrawer";
import { useApp } from "./state";
import { menuIds } from "./simpleMode";
import { workspaceColor } from "./palette";
import { Icon, IconButton, Menu, ToolbarSearch, useSnackbar, type MenuEntry } from "@blitz/webui";
import { config } from "./api";
import { message } from "./errors";
import { useWorkerFailures } from "./workerFailures";
import { appName, logoFor, useDarkTheme, useOrgBranding, useOrgWorkspaces } from "./orgBranding";
import type { OrgWorkspace } from "./gen/blitz/v1/config_pb";

/** The app's name and mark, at the left of the top bar: the organization's when it has its own (EN-51). */
export function Brand() {
  const org = useOrgBranding();
  const dark = useDarkTheme();
  const logo = logoFor(org, dark);
  return (
    <span className="brand">
      {logo ? <img src={logo} alt="" className="icon brand-mark" /> : <Icon path={mdiLightningBolt} className="brand-mark" />}
      <span className="brand-name ellipsis">{org?.shortName || appName(org)}</span>
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
  onIncludeAll,
  onProtect,
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
  /** Include all changes (git add --all). */
  onIncludeAll?: () => void;
  /** Add the hook that stops secrets being committed. */
  onProtect?: () => void;
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
  const snack = useSnackbar();
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

  const orgWs = useOrgWorkspaces();
  // A workspace kept in step with the organization saves itself: its
  // Changes are this session's and its History, never Git's words (EN-64),
  // but in advanced mode, where Git's items stay beside them.
  const synced = !!dir && !!orgWs?.workspaces.some((w) => w.dir === dir && w.sync && w.role !== "viewer");
  const gitItems = !synced || advanced;
  const changes: MenuEntry[] = [
    { label: t("desktop.menu.changes.session"), detail: t("desktop.changes.session"), icon: mdiHistory, onSelect: () => onChanges?.("session") },
  ];
  if (gitItems) changes.push({ label: t("desktop.menu.changes.git"), icon: mdiGit, onSelect: () => onChanges?.("git") });
  if (onHistory) changes.push({ label: t("desktop.menu.changes.history"), detail: synced ? t("desktop.menu.changes.history_synced") : undefined, icon: mdiSourceBranch, onSelect: onHistory });
  if (gitItems) {
    if (onCommit) changes.push("divider");
    if (onIncludeAll) changes.push({ label: t("desktop.commit.stage_all"), icon: mdiPlusBoxMultipleOutline, onSelect: onIncludeAll });
    if (onProtect) changes.push({ label: t("desktop.secrethook.menu"), detail: t("desktop.secrethook.menu_detail"), icon: mdiShieldKeyOutline, onSelect: onProtect });
    if (onCommit) changes.push({ label: t("desktop.commit.open"), icon: mdiSourceCommit, onSelect: onCommit });
  }

  const status = (w: { dir: string }) => {
    const a = activity[w.dir];
    const n = failures[w.dir] ?? 0;
    return a?.waiting ? t("desktop.switcher.waiting") : a?.running ? t("desktop.switcher.working") : n > 0 ? tn("desktop.switcher.failed_runs", n) : "";
  };
  const recent = recentWorkspaces(prefs).slice(0, 8);
  const org = orgWs;
  const [orgDialog, setOrgDialog] = useState<{ share?: { dir: string; name: string } } | null>(null);
  const orgDone = async (w: OrgWorkspace, notes: string[]) => {
    const shared = !!orgDialog?.share;
    setOrgDialog(null);
    configChanged({ dir: "" }); // the list again
    if (shared) {
      for (const n of notes) snack(n);
      snack(t("desktop.org.shared", { handle: w.handle }));
    } else await openOrg(w);
  };
  const openOrg = async (w: OrgWorkspace) => {
    if (!w.cloned) snack(t("desktop.org.cloning", { name: w.name || w.handle }));
    try {
      const r = await config.openOrgWorkspace({ handle: w.handle });
      update((p) => openWorkspace(p, r.dir));
    } catch (e) {
      snack(message(e));
    }
  };
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
  // The organization's workspaces, cloned the first time they're opened;
  // a new one, and this folder shared with it where it is (EN-60, EN-66).
  const current = dir ? openWorkspaces(prefs).find((w) => w.dir === dir) : undefined;
  const isOrgs = !!dir && !!org?.workspaces.some((w) => w.dir === dir);
  if (org) {
    workspaceItems.push(
      { heading: org.organization },
      ...org.workspaces.map(
        (w): MenuEntry => ({
          label: w.name || w.handle,
          detail: [t(`desktop.org.role.${w.role}`), w.cloned ? "" : t("desktop.org.not_cloned")].filter(Boolean).join(" · "),
          icon: mdiFolderAccountOutline,
          onSelect: () => void openOrg(w),
        }),
      ),
      { label: t("desktop.org.new"), icon: mdiFolderPlusOutline, onSelect: () => setOrgDialog({}) },
      ...(dir && current && !isOrgs
        ? [{ label: t("desktop.org.share", { name: displayName(current), org: org.organization }), icon: mdiFolderSyncOutline, onSelect: () => setOrgDialog({ share: { dir, name: displayName(current) } }) }]
        : []),
    );
  }
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
      {orgDialog &&
        org &&
        createPortal(<OrgWorkspaceDialog org={org.organization} share={orgDialog.share} onClose={() => setOrgDialog(null)} onDone={(w, notes) => void orgDone(w, notes)} />, document.body)}
      <Menu className="menubar-menu wide-menu" trigger={(p) => <MenuButton label={t("desktop.menu.workspaces")} dot={othersWaiting} {...p} />} items={workspaceItems} />
      {dir && (
        <>
          {/* Simple mode: Changes is this session's, a button; Git is advanced. */}
          {advanced || (synced && onHistory) ? (
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

