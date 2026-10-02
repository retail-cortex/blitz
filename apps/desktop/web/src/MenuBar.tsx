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

import { useCallback, useEffect, useState } from "react";
import {
  mdiAutoFix,
  mdiBookOpenPageVariantOutline,
  mdiCalendarClock,
  mdiCalendarPlus,
  mdiChevronDown,
  mdiClose,
  mdiCogOutline,
  mdiDeleteOutline,
  mdiFolderOpenOutline,
  mdiFolderOutline,
  mdiGit,
  mdiHistory,
  mdiInboxOutline,
  mdiInformationOutline,
  mdiLightningBolt,
  mdiPauseCircleOutline,
  mdiPencilOutline,
  mdiPlay,
  mdiShieldCheckOutline,
  mdiRobotOutline,
  mdiPlus,
} from "@mdi/js";
import { workers, workspaces } from "./api";
import { inboxCounts, useRuns } from "./backgroundRuns";
import type { ChangesSource } from "./Changes";
import { openURL } from "./desktop";
import { startSetup } from "./events";
import { fileName } from "./agentForm";
import { AgentScope, type AgentFile } from "./gen/blitz/v1/workspace_pb";
import { WorkerState, type Worker } from "./gen/blitz/v1/worker_pb";
import { intent, type ItemIntent } from "./intent";
import { t, tn } from "./i18n";
import { displayName, forgetWorkspace, openWorkspace, openWorkspaces, recentWorkspaces } from "./prefs";
import { InboxDialog } from "./RunsInbox";
import { workerStateLabel } from "./Workers";
import type { Section } from "./SettingsDialog";
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

/** Where the user guide is published. */
export const guideURL = "https://retail-cortex.github.io/blitz/guide/";

/**
 * The top bar's menus, from the left edge of the center panel: Workspaces
 * (open one, switch, edit its settings, close), Changes (the session's or
 * git's), Agents and Workers (add one, or open, run, edit or remove one
 * from the list; the background runs), Help; at the right, the
 * search (the command palette) and Settings (the global settings, an icon).
 * What they open shows over the editor, in a dialog. Without a workspace
 * (the welcome screen) only Workspaces, Help, the search and Settings
 * remain.
 */
export function MenuBar({
  dir = "",
  onChanges,
  onAgent,
  onWorker,
  onSearch,
  onSettings,
  onOpenWorkspace,
  onCloseWorkspace,
}: {
  dir?: string;
  onChanges?: (s: ChangesSource) => void;
  onAgent?: (i: ItemIntent) => void;
  onWorker?: (i: ItemIntent) => void;
  onSearch: () => void;
  onSettings: (section: Section, scope?: string) => void;
  onOpenWorkspace: () => void;
  onCloseWorkspace: (dir: string) => void;
}) {
  const { prefs, update, activity, theme } = useApp();
  // Simple mode keeps Workspaces, Changes and Help (simpleMode.ts).
  const advanced = prefs.advanced;
  const menus = menuIds(advanced);
  const runs = useRuns();
  const failures = useWorkerFailures();
  const [agents, setAgents] = useState<AgentFile[]>([]);
  const [workerList, setWorkerList] = useState<Worker[]>([]);
  const [inbox, setInbox] = useState(false);

  // The lists are asked for when the workspace opens and each time their
  // menu opens, so they show what's on disk now.
  const loadAgents = useCallback(() => {
    if (!dir) return;
    workspaces.listAgentFiles({ workspace: dir, scope: AgentScope.WORKSPACE }).then(
      (r) => setAgents(r.files),
      () => setAgents([]),
    );
  }, [dir]);
  const loadWorkers = useCallback(() => {
    if (!dir) return;
    workers.listWorkers({ workspace: dir }).then(
      (r) => setWorkerList(r.workers),
      () => setWorkerList([]),
    );
  }, [dir]);
  useEffect(() => {
    loadAgents();
    loadWorkers();
  }, [loadAgents, loadWorkers]);

  const { active, waiting } = inboxCounts(runs);
  const failed = dir ? (failures[dir] ?? 0) : 0;

  const changes: MenuEntry[] = [
    { label: t("desktop.menu.changes.session"), detail: t("desktop.changes.session"), icon: mdiHistory, onSelect: () => onChanges?.("session") },
    { label: t("desktop.menu.changes.git"), icon: mdiGit, onSelect: () => onChanges?.("git") },
  ];

  const agentItems: MenuEntry[] = [
    { label: t("desktop.menu.agents.add"), icon: mdiPlus, onSelect: () => onAgent?.(intent("new")) },
    "divider",
    ...(agents.length === 0
      ? [{ heading: t("desktop.menu.agents.none") }]
      : agents.map((f): MenuEntry => {
          const name = f.agent?.displayName || f.agent?.name || fileName(f.path);
          return {
            label: name,
            detail: f.problem ? t("desktop.agents.broken") : f.agent?.description,
            icon: mdiRobotOutline,
            danger: !!f.problem,
            onSelect: () => onAgent?.(intent("edit", f.path)),
            actions: [{ icon: mdiDeleteOutline, label: t("desktop.menu.remove", { name }), removes: true, onSelect: () => onAgent?.(intent("delete", f.path)) }],
          };
        })),
  ];

  const workerItems: MenuEntry[] = [
    { label: t("desktop.menu.workers.add"), icon: mdiCalendarPlus, onSelect: () => onWorker?.(intent("new")) },
    {
      label: t("desktop.inbox.title"),
      detail: waiting > 0 ? tn("desktop.inbox.waiting", waiting) : active > 0 ? tn("desktop.inbox.running", active) : undefined,
      icon: mdiInboxOutline,
      onSelect: () => setInbox(true),
    },
    "divider",
    ...(workerList.length === 0
      ? [{ heading: t("desktop.menu.workers.none") }]
      : workerList.map(
          (w): MenuEntry => ({
            label: w.name,
            detail: `${workerStateLabel(w.state)} · ${w.schedule}`,
            icon: mdiCalendarClock,
            onSelect: () => onWorker?.(intent("show", w.name)),
            actions: workerActions(w).map(([kind, icon, label]) => ({
              icon,
              label: t(label, { name: w.name }),
              removes: kind === "delete",
              onSelect: () => onWorker?.(intent(kind, w.name)),
            })),
          }),
        )),
  ];

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
    { label: t("desktop.menu.guide"), icon: mdiBookOpenPageVariantOutline, onSelect: () => void openURL(guideURL).catch(() => {}) },
    ...(dir ? [{ label: t("desktop.setup.title"), icon: mdiAutoFix, onSelect: () => startSetup(dir) }] : []),
    "divider",
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
          {menus.includes("agents") && (
            <Menu
              className="menubar-menu"
              trigger={(p) => <MenuButton label={t("desktop.view.agents")} {...p} onClick={() => (loadAgents(), p.onClick())} />}
              items={agentItems}
            />
          )}
          {menus.includes("workers") && (
          <Menu
            className="menubar-menu"
            trigger={(p) => (
              <MenuButton
                label={t("desktop.view.workers")}
                badge={active > 0 ? { n: waiting || active, waiting: waiting > 0 } : undefined}
                dot={failed > 0}
                {...p}
                onClick={() => (loadWorkers(), p.onClick())}
              />
            )}
            items={workerItems}
          />
          )}
        </>
      )}
      <Menu className="menubar-menu" trigger={(p) => <MenuButton label={t("desktop.menu.help")} {...p} />} items={help} />
      <span className="spacer" />
      <ToolbarSearch onClick={onSearch} />
      <IconButton icon={mdiCogOutline} label={t("desktop.menu.settings_hint")} onClick={() => onSettings("appearance")} />
      {inbox && <InboxDialog list={runs} onClose={() => setInbox(false)} />}
    </nav>
  );
}

/**
 * The buttons on a worker's row in the Workers menu: Run (enabled ones),
 * Disable, or Review and enable (which shows it, its permissions and
 * Enable, rather than enabling unseen), Edit and Delete.
 */
export function workerActions(w: Pick<Worker, "state">): [ItemIntent["kind"], string, string][] {
  const enabled = w.state === WorkerState.ENABLED;
  const out: [ItemIntent["kind"], string, string][] = [];
  if (enabled) out.push(["run", mdiPlay, "desktop.menu.workers.run"], ["disable", mdiPauseCircleOutline, "desktop.menu.workers.disable"]);
  else out.push(["show", mdiShieldCheckOutline, "desktop.menu.workers.review"]);
  out.push(["edit", mdiPencilOutline, "desktop.menu.workers.edit"], ["delete", mdiDeleteOutline, "desktop.menu.remove"]);
  return out;
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

