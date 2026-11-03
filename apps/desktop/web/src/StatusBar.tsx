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


import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useServiceApprovals, useServiceRuns } from "./serviceRuns";
import { notify } from "./notify";
import type { GoingServiceRun } from "./gen/blitz/v1/config_pb";
import { ms, took } from "./workflowRuns";
import {
  mdiAccountAlertOutline,
  mdiAccountCheckOutline,
  mdiAccountClockOutline,
  mdiCheck,
  mdiClose,
  mdiAlertCircleOutline,
  mdiAlertOutline,
  mdiFileCompare,
  mdiCloudSyncOutline,
  mdiCloudOffOutline,
  mdiCloudAlertOutline,
  mdiCloudUploadOutline,
  mdiCloudCheckOutline,
  mdiCloudOutline,
  mdiCircleMedium,
  mdiCloseCircleOutline,
  mdiCreationOutline,
  mdiDeveloperBoard,
  mdiInboxOutline,
  mdiLoading,
  mdiRobotOutline,
  mdiSourceBranch,
  mdiSourceMerge,
  mdiStopCircleOutline,
  mdiTextBoxOutline,
  mdiTuneVariant,
  mdiVectorPolyline,
} from "@mdi/js";
import { config, workspaces } from "./api";
import { inboxCounts, useRuns } from "./backgroundRuns";
import { FlowRunsItem } from "./FlowRunAlerts";
import { showTasks, useTasks } from "./humanTasks";
import { message, reason } from "./errors";
import { configChanged, filesTouchedEvent, showPending, showView } from "./events";
import type { AgentInfo, GetGitStatusResponse, WorkspaceProcess } from "./gen/blitz/v1/workspace_pb";
import type { GetOrgSyncResponse } from "./gen/blitz/v1/config_pb";
import { t, tn } from "./i18n";
import { effortIcon, efforts, modeOf, modes } from "./options";
import { workspaceColor } from "./palette";
import { inProgress, syncTitle } from "./gitRemote";
import { displayName } from "./prefs";
import { byWorkspace, countLabel, flaggedOf, isFlagged, processDetail, stopProcess, useProcesses } from "./processes";
import { InboxDrawer } from "./RunsInbox";
import { useAdvanced, useApp } from "./state";
import { shownVersion } from "./serviceVersion";
import { contextShare, tokens, useWorkspaceStatus } from "./status";
import { Button, Dialog, Icon, Menu, useSnackbar, type MenuEntry } from "@blitz/webui";
import { useWorkerFailures } from "./workerFailures";

/** How often the branch and changed files are asked for while shown. */
const gitPollMs = 10_000;

/**
 * The status bar along the window's foot (spec_024_desktop DSK-39): the
 * service and the workspace in view, its git branch, what runs, and the
 * agent, model, permission mode, context and cost, with the editor's
 * cursor when a file is shown. Items open what they describe.
 */
export function StatusBar({ serviceUp, version, onWorkspaceSettings }: { serviceUp: boolean; version?: string; onWorkspaceSettings: (dir: string) => void }) {
  const { prefs, activity, theme } = useApp();
  const ws = prefs.workspaces.find((w) => w.dir === prefs.active && w.open);
  const dir = ws?.dir ?? "";
  const [orgSync, setOrgSync] = useOrgSync(serviceUp ? dir : "");
  // A workspace kept in step shows its sync; Git's branch beside it in
  // advanced mode, where Changes' Git work is.
  const advanced = useAdvanced();
  return (
    <footer className="status-bar t-label-sm" role="contentinfo" aria-label={t("desktop.status.label")}>
      {/* The service: an icon, green while it answers and red while it's
          reconnected to; what it means in its hover text. */}
      <span
        className={`sb-item sb-service ${serviceUp ? "up" : "lost"}`}
        role="status"
        title={serviceUp ? t("desktop.status.service_up", { version: shownVersion(version ?? "") }) : t("desktop.app.lost")}
        aria-label={serviceUp ? t("desktop.status.connected") : t("desktop.status.reconnecting")}
      >
        <Icon path={mdiVectorPolyline} size="sm" />
      </span>
      {/* First on the left, clear of the turn's activity on the right. */}
      {ws && serviceUp && <ProcessesItem dir={dir} />}
      {ws && (
        <>
          <span className="sb-item sb-ws" title={ws.dir}>
            <span className="sb-swatch" style={{ background: workspaceColor(ws.color, theme) }} />
            <span className="ellipsis">{displayName(ws)}</span>
          </span>
          {serviceUp && orgSync && <OrgSyncItem dir={dir} state={orgSync} onState={setOrgSync} />}
          {serviceUp && (!orgSync || advanced) && <GitItem dir={dir} />}
        </>
      )}
      <span className="spacer" />
      {ws && <Activity dir={dir} running={!!activity[dir]?.running} waiting={!!activity[dir]?.waiting} />}
      {serviceUp && <TasksItem />}
      {serviceUp && <ApprovalsItem dir={dir} />}
      {serviceUp && <RunsItem />}
      {serviceUp && <FlowRunsItem />}
      {ws && <FailedItem dir={dir} />}
      {ws && serviceUp && <WorkspaceItems dir={dir} onWorkspaceSettings={() => onWorkspaceSettings(dir)} />}
    </footer>
  );
}

/** How often an organization workspace's sync state is asked for. */
const orgSyncPollMs = 3_000;

/**
 * An organization workspace's sync, while the service keeps it in step
 * (spec_053_enterprise EN-64); undefined for any other workspace.
 */
function useOrgSync(dir: string): [GetOrgSyncResponse | undefined, (s: GetOrgSyncResponse) => void] {
  const [state, setState] = useState<GetOrgSyncResponse>();
  useEffect(() => {
    setState(undefined);
    if (!dir) return;
    let live = true;
    const ask = () =>
      config.getOrgSync({ workspace: dir }).then(
        (r) => live && setState(r.state ? r : undefined),
        () => live && setState(undefined),
      );
    void ask();
    const timer = setInterval(() => void ask(), orgSyncPollMs);
    return () => {
      live = false;
      clearInterval(timer);
    };
  }, [dir]);
  return [state, setState];
}

const orgSyncIcons: Record<string, string> = {
  saved: mdiCloudCheckOutline,
  saving: mdiCloudSyncOutline,
  offline: mdiCloudOffOutline,
  signed_out: mdiAccountAlertOutline,
  conflicts: mdiFileCompare,
  refused: mdiCloudAlertOutline,
};

/**
 * The sync's state in words, never Git's: Saved, Saving…, Offline, Signed
 * out, Both versions kept. Its menu has Save now, which saves and sends
 * what waits at once, and says why it isn't saved.
 */
function OrgSyncItem({ dir, state, onState }: { dir: string; state: GetOrgSyncResponse; onState: (s: GetOrgSyncResponse) => void }) {
  const snack = useSnackbar();
  const [saving, setSaving] = useState(false);
  const label = state.state === "conflicts" ? tn("desktop.org.sync.conflicts", state.pairs.length) : t(`desktop.org.sync.${state.state}`);
  const detail = [state.error, ...state.pairs.map((p) => t("desktop.org.sync.pair", { shared: p.shared, mine: p.mine }))].filter(Boolean).join("\n");
  const warn = ["conflicts", "signed_out", "refused"].includes(state.state);
  const saveNow = async () => {
    setSaving(true);
    try {
      const r = await config.saveOrgSync({ workspace: dir });
      onState({ ...state, state: r.state, error: r.error, pairs: r.pairs });
      snack(r.state === "saved" || r.state === "conflicts" ? t("desktop.org.sync.saved_now") : t(`desktop.org.sync.${r.state}`), { error: orgSyncFailed(r.state) });
    } catch (e) {
      snack(reason(e) === "GIT_BUSY" ? t("desktop.org.sync.busy") : message(e), { error: reason(e) !== "GIT_BUSY" });
    } finally {
      setSaving(false);
    }
  };
  const items: MenuEntry[] = [
    { heading: label },
    { label: t("desktop.org.sync.save_now"), detail: detail || undefined, icon: mdiCloudUploadOutline, disabled: saving, onSelect: () => void saveNow() },
  ];
  return (
    <Menu
      placement="up start"
      className="wide-menu"
      trigger={(p) => (
        <button className={`sb-item sb-button ${warn ? "warn-text" : ""}`} title={detail || label} aria-label={label} {...p}>
          <Icon path={orgSyncIcons[saving ? "saving" : state.state] ?? mdiCloudCheckOutline} size="sm" />
          <span className="ellipsis">{saving ? t("desktop.org.sync.saving") : label}</span>
        </button>
      )}
      items={items}
    />
  );
}

/** Whether a sync's state means the service hasn't got this computer's saves. */
export function orgSyncFailed(state: string): boolean {
  return state === "offline" || state === "signed_out" || state === "refused";
}

function GitItem({ dir }: { dir: string }) {
  // Git's changes and branches are advanced: simple mode shows this session's.
  const advanced = useAdvanced();
  const [git, setGit] = useState<GetGitStatusResponse>();
  useEffect(() => {
    let live = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const ask = () =>
      workspaces
        .getGitStatus({ workspace: dir })
        .then((r) => live && setGit(r))
        .catch(() => live && setGit(undefined)); // an older service, or git missing
    const soon = (e: Event) => {
      if ((e as CustomEvent<{ dir: string }>).detail.dir !== dir) return;
      clearTimeout(timer);
      timer = setTimeout(ask, 500);
    };
    ask();
    const poll = setInterval(() => document.visibilityState === "visible" && ask(), gitPollMs);
    window.addEventListener(filesTouchedEvent, soon);
    window.addEventListener("focus", ask);
    return () => {
      live = false;
      clearTimeout(timer);
      clearInterval(poll);
      window.removeEventListener(filesTouchedEvent, soon);
      window.removeEventListener("focus", ask);
    };
  }, [dir]);
  if (!git?.repo) return null;
  const merging = inProgress(git);
  const label = [
    git.changed > 0 ? tn("desktop.status.git_changed", git.changed, { branch: git.branch }) : t("desktop.status.git_clean", { branch: git.branch }),
    merging ? tn("desktop.git.conflicts_left", git.conflicts.length) : git.upstream && (git.ahead || git.behind) ? syncTitle(git) : "",
  ]
    .filter(Boolean)
    .join(" ");
  return (
    <button className="sb-item sb-button" title={label} aria-label={label} onClick={() => showView({ dir, view: "changes", source: advanced ? "git" : "session", toggle: true })}>
      <Icon path={merging ? mdiSourceMerge : mdiSourceBranch} size="sm" />
      <span className="ellipsis sb-branch">{git.branch}</span>
      {git.changed > 0 && <span className="sb-count">±{git.changed}</span>}
      {git.behind > 0 && <span className="sb-count">↓{git.behind}</span>}
      {git.ahead > 0 && <span className="sb-count">↑{git.ahead}</span>}
    </button>
  );
}

// A turn running, or waiting for the user: clicking the latter shows what it asks.
function Activity({ dir, running, waiting }: { dir: string; running: boolean; waiting: boolean }) {
  if (!running && !waiting) return null;
  if (waiting)
    return (
      <button className="sb-item sb-button sb-activity waiting" title={t("desktop.status.waiting_title")} onClick={() => showPending({ dir })}>
        <Icon path={mdiAlertCircleOutline} size="sm" />
        <span className="sb-low">{t("desktop.status.waiting")}</span>
      </button>
    );
  return (
    <span className="sb-item sb-activity" role="status">
      <Icon path={mdiLoading} size="sm" spin />
      <span className="sb-low">{t("desktop.status.working")}</span>
    </span>
  );
}

function RunsItem() {
  const list = useRuns();
  const [open, setOpen] = useState(false);
  const { active, waiting } = inboxCounts(list);
  if (active === 0 && !open) return null;
  const label = waiting > 0 ? tn("desktop.inbox.waiting", waiting) : tn("desktop.inbox.running", active);
  return (
    <>
      <button className={`sb-item sb-button ${waiting > 0 ? "sb-warn" : ""}`} title={label} aria-label={label} onClick={() => setOpen(true)}>
        <Icon path={mdiInboxOutline} size="sm" />
        <span>{waiting || active}</span>
      </button>
      {open && <InboxDrawer list={list} onClose={() => setOpen(false)} />}
    </>
  );
}

// The organization's jobs waiting for the person's answer (spec_053
// EN-43): a count, and a menu of what each asks to do with Approve and
// Deny; each new one notifies, as an agent waiting does.
function ApprovalsItem({ dir }: { dir: string }) {
  const { prefs } = useApp();
  const snack = useSnackbar();
  const { runs, reload } = useServiceApprovals();
  const seen = useRef<Set<string> | null>(null);
  useEffect(() => {
    const ids = new Set(runs.map((g) => g.run?.id ?? ""));
    if (seen.current && prefs.notifications === "on") {
      for (const g of runs) {
        const id = g.run?.id ?? "";
        if (!seen.current.has(id)) notify(t("desktop.approvals.notify_title", { job: g.jobName }), `${g.run?.pending?.tool}: ${g.run?.pending?.detail}`, g.dir || dir);
      }
    }
    seen.current = ids; // the first list is what was there already
  }, [runs, prefs.notifications, dir]);
  if (runs.length === 0) return null;
  const answer = async (g: GoingServiceRun, approve: boolean) => {
    try {
      await config.answerServiceRun({ runId: g.run?.id ?? "", approve, note: "" });
      snack(t(approve ? "desktop.approvals.approved" : "desktop.approvals.denied", { job: g.jobName }));
    } catch (e) {
      snack(message(e), { error: true });
    }
    reload();
  };
  const label = t("desktop.approvals.label", { count: runs.length });
  const items: MenuEntry[] = [
    { heading: t("desktop.approvals.heading") },
    ...runs.map((g) => ({
      label: g.jobName,
      detail: [t("desktop.approvals.asks", { tool: g.run?.pending?.tool ?? "", detail: g.run?.pending?.detail ?? "" }), displayName(prefs.workspaces.find((w) => w.dir === g.dir) ?? { dir: g.dir })].join(" · "),
      icon: mdiCloudOutline,
      onSelect: () => (g.dir === dir ? showView({ dir, view: "workers", worker: g.jobName }) : undefined),
      actions: [
        { icon: mdiCheck, label: t("desktop.approvals.approve"), removes: true, onSelect: () => void answer(g, true) },
        { icon: mdiClose, label: t("desktop.approvals.deny"), removes: true, onSelect: () => void answer(g, false) },
      ],
    })),
  ];
  return (
    <Menu
      placement="up start"
      className="wide-menu"
      trigger={(p) => (
        <button className="sb-item sb-button sb-warn" title={label} aria-label={label} {...p}>
          <Icon path={mdiAccountClockOutline} size="sm" />
          <span>{runs.length}</span>
        </button>
      )}
      items={items}
    />
  );
}

// Workflows' human tasks waiting, in any workspace: opens the tasks dialog.
function TasksItem() {
  const { tasks } = useTasks();
  if (tasks.length === 0) return null;
  const label = tn("desktop.human_tasks.waiting", tasks.length);
  return (
    <button className="sb-item sb-button sb-warn" title={label} aria-label={label} onClick={showTasks}>
      <Icon path={mdiAccountCheckOutline} size="sm" />
      <span>{tasks.length}</span>
    </button>
  );
}

// The agents' background processes, in any workspace
// (spec_052_processes PR-50): a board icon, green while some run (amber
// when one is flagged) and greyed when none do, the count in its hover
// text. It opens a menu of them by workspace, each with what it uses and
// Stop; a pick, or Output and details, opens the drawer on its output.
function ProcessesItem({ dir }: { dir: string }) {
  const { prefs } = useApp();
  const snack = useSnackbar();
  const { list, receivedAt } = useProcesses();
  const [confirm, setConfirm] = useState(false);
  const on = list.length > 0;
  const flagged = flaggedOf(list);
  const label = on ? countLabel(list.length, flagged.length) : t("desktop.processes.empty_all");
  const state = !on ? "off" : flagged.length > 0 ? "sb-warn" : "on";
  const stop = async (ps: WorkspaceProcess[]) => {
    const failed = (await Promise.allSettled(ps.map(stopProcess))).find((r): r is PromiseRejectedResult => r.status === "rejected");
    if (failed) snack(message(failed.reason), { error: true });
  };
  const open = (p?: WorkspaceProcess) => showView({ dir, view: "processes", process: p?.process?.id, processIn: p?.workspace });
  const now = Date.now();
  // The organization's runs going now (spec_053 EN-42), after this computer's.
  const { runs: serviceRuns } = useServiceRuns();
  const service: MenuEntry[] = serviceRuns.length
    ? [
        { heading: t("desktop.processes.service") },
        ...serviceRuns.map((g) => ({
          label: g.jobName,
          detail: [displayName(prefs.workspaces.find((w) => w.dir === g.dir) ?? { dir: g.dir }), t(`desktop.service_job.status.${g.run?.status}`), took(ms(g.run?.created), now)].join(" · "),
          icon: mdiCloudOutline,
          onSelect: () => (g.dir === dir ? showView({ dir, view: "workers", worker: g.jobName }) : open()),
        })),
      ]
    : [];
  const items: MenuEntry[] = !on
    ? [{ heading: t("desktop.processes.title") }, { label: t("desktop.processes.empty_all"), disabled: true, onSelect: () => {} }, ...service]
    : [
        ...byWorkspace(list, dir).flatMap((g): MenuEntry[] => [
          { heading: displayName(prefs.workspaces.find((w) => w.dir === g.workspace) ?? { dir: g.workspace }) },
          ...g.processes.map((p) => ({
            label: p.process?.command ?? "",
            detail: processDetail(p, receivedAt, now),
            icon: mdiCircleMedium,
            iconColor: isFlagged(p) ? "var(--md-warning, #b8860b)" : "var(--md-tertiary)",
            onSelect: () => open(p),
            actions: [{ icon: mdiStopCircleOutline, label: t("desktop.processes.stop"), removes: true, onSelect: () => void stop([p]) }],
          })),
        ]),
        ...service,
        "divider",
        ...(flagged.length > 0
          ? [{ label: `${t("desktop.processes.stop_flagged")} (${flagged.length})`, icon: mdiStopCircleOutline, danger: true, onSelect: () => setConfirm(true) }]
          : []),
        { label: t("desktop.processes.details"), icon: mdiTextBoxOutline, onSelect: () => open() },
      ];
  return (
    <>
      <Menu
        placement="up start"
        className="wide-menu"
        trigger={(p) => (
          <button className={`sb-item sb-button sb-procs ${state}`} title={label} aria-label={label} {...p}>
            <Icon path={mdiDeveloperBoard} size="sm" />
          </button>
        )}
        items={items}
      />
      {confirm &&
        createPortal(
          <Dialog
            title={t("desktop.processes.stop_flagged_title")}
            icon={mdiStopCircleOutline}
            onClose={() => setConfirm(false)}
            footer={
              <>
                <Button onClick={() => setConfirm(false)}>{t("desktop.cancel")}</Button>
                <Button
                  variant="filled"
                  danger
                  onClick={() => {
                    setConfirm(false);
                    void stop(flagged);
                  }}
                >
                  {t("desktop.processes.stop")}
                </Button>
              </>
            }
          >
            <p>{tn("desktop.processes.stop_flagged_body", flagged.length)}</p>
          </Dialog>,
          document.body,
        )}
    </>
  );
}

function FailedItem({ dir }: { dir: string }) {
  const failed = useWorkerFailures()[dir] ?? 0;
  if (failed === 0) return null;
  const label = tn("desktop.status.failed_runs", failed);
  return (
    <button className="sb-item sb-button sb-error" title={label} aria-label={label} onClick={() => showView({ dir, view: "workers", toggle: true })}>
      <Icon path={mdiAlertCircleOutline} size="sm" />
      <span>{failed}</span>
    </button>
  );
}

// The editor's cursor, the agent and model (a menu of the agents, to
// switch the one working on the conversation), the mode, context and
// cost: what the workspace published.
function WorkspaceItems({ dir, onWorkspaceSettings }: { dir: string; onWorkspaceSettings: () => void }) {
  const st = useWorkspaceStatus(dir);
  const snack = useSnackbar();
  const s = st.settings;
  const mode = modeOf(s?.permissionMode ?? "auto");
  const setMode = async (value: string) => {
    try {
      await workspaces.setPermissionMode({ workspace: dir, mode: value });
      configChanged({ dir });
    } catch (e) {
      snack(reason(e) === "BYPASS_NEEDS_SANDBOX" ? t("desktop.bypass_needs_sandbox") : message(e), { error: true });
    }
  };
  const effort = efforts().find((e) => e.value === (s?.effort ?? "")) ?? efforts()[0];
  const setEffort = async (value: string) => {
    try {
      await workspaces.setSetting({ workspace: dir, key: "effort", value: value || "auto" });
      configChanged({ dir });
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  // The agent working on the conversation: /agent's list, switched from
  // the next turn on (SetAgent), the history kept.
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const loadAgents = () =>
    workspaces.listAgents({ workspace: dir }).then(
      (r) => setAgents(r.agents),
      () => {},
    );
  const setAgent = async (name: string, shown: string) => {
    try {
      const r = await workspaces.setAgent({ workspace: dir, name });
      configChanged({ dir });
      snack(t("desktop.agents.switched", { name: r.agent?.displayName || shown }));
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const total = st.total;
  const share = total ? contextShare(total.lastPrompt, st.threshold) : undefined;
  const c = st.cursor;
  return (
    <>
      {c && (
        <span className="sb-item sb-cursor">
          <span>{c.selected > 0 ? t("desktop.status.cursor_selected", { line: c.line, column: c.column, selected: c.selected }) : t("desktop.status.cursor", { line: c.line, column: c.column })}</span>
          {c.language && <span className="sb-low">{c.language}</span>}
          {(c.errors ?? 0) + (c.warnings ?? 0) > 0 && (
            <span className="sb-problems" title={t("desktop.status.problems_title", { errors: c.errors ?? 0, warnings: c.warnings ?? 0 })}>
              {!!c.errors && (
                <span className="sb-errors">
                  <Icon path={mdiCloseCircleOutline} size="sm" />
                  {c.errors}
                </span>
              )}
              {!!c.warnings && (
                <span className="sb-warnings">
                  <Icon path={mdiAlertOutline} size="sm" />
                  {c.warnings}
                </span>
              )}
            </span>
          )}
          {c.server && (
            <span className="sb-low ellipsis sb-server" title={c.server}>
              {c.server}
            </span>
          )}
        </span>
      )}
      {s && (
        <Menu
          placement="up end"
          className="wide-menu"
          trigger={(p) => (
            <button
              className="sb-item sb-button sb-model"
              title={t("desktop.status.model_title", { agent: s.agent, model: s.model, provider: s.provider })}
              {...p}
              onClick={() => (loadAgents(), p.onClick())}
            >
              <Icon path={mdiRobotOutline} size="sm" />
              <span className="sb-low ellipsis">{s.agent}</span>
              <span className="ellipsis">{s.model}</span>
            </button>
          )}
          items={[
            { heading: t("desktop.status.agent_heading") },
            ...agents.map((a) => ({
              label: a.displayName || a.name,
              detail: [
                a.orgLock && t(a.orgLock === "locked" ? "desktop.org.item_locked" : "desktop.org.item_default"),
                a.description,
                a.pinnedModel && t("desktop.status.agent_pinned", { model: a.pinnedModel }),
              ]
                .filter(Boolean)
                .join(" · "),
              icon: mdiRobotOutline,
              on: a.active,
              onSelect: () => void setAgent(a.name, a.displayName || a.name),
            })),
            "divider",
            { label: `${t("desktop.run_settings")}…`, icon: mdiTuneVariant, onSelect: onWorkspaceSettings },
          ]}
        />
      )}
      {s && (
        <Menu
          placement="up end"
          className="wide-menu"
          trigger={(p) => (
            <button className={`sb-item sb-button ${mode.value === "bypass" ? "sb-error" : mode.value !== "default" ? "sb-accent" : ""}`} title={mode.detail} {...p}>
              {mode.icon && <Icon path={mode.icon} size="sm" />}
              <span className="sb-low">{mode.label}</span>
            </button>
          )}
          items={[{ heading: t("desktop.composer.mode") }, ...modes().map((m) => ({ label: m.label, detail: m.detail, icon: m.icon, on: m.value === mode.value, onSelect: () => setMode(m.value) }))]}
        />
      )}
      {s && (
        <Menu
          placement="up end"
          className="wide-menu"
          trigger={(p) => (
            <button className={`sb-item sb-button ${effort.value ? "sb-accent" : ""}`} title={t("desktop.composer.effort_title")} {...p}>
              <Icon path={effortIcon} size="sm" />
              <span className="sb-low">{t("desktop.composer.effort_set", { effort: effort.label })}</span>
            </button>
          )}
          items={[{ heading: t("desktop.composer.effort_heading") }, ...efforts().map((e) => ({ label: e.label, detail: e.detail, on: e.value === effort.value, onSelect: () => setEffort(e.value) }))]}
        />
      )}
      {total && total.calls > 0 && (
        <span
          className="sb-item sb-context"
          title={
            share === undefined
              ? t("desktop.status.context_title", { tokens: tokens(total.lastPrompt) })
              : t("desktop.status.context_threshold", { tokens: tokens(total.lastPrompt), threshold: tokens(st.threshold ?? 0), percent: Math.round(share * 100) })
          }
        >
          <Icon path={mdiCreationOutline} size="sm" />
          {share !== undefined && (
            <span className={`sb-meter ${share > 0.85 ? "high" : ""}`} aria-hidden="true">
              <span style={{ width: `${Math.round(share * 100)}%` }} />
            </span>
          )}
          <span>{tokens(total.lastPrompt)}</span>
        </span>
      )}
      {total && total.calls > 0 && (
        <span className="sb-item sb-cost sb-low" title={[st.turn, t("desktop.status.session", { tokens: tokens(total.input + total.output) })].filter(Boolean).join("\n")}>
          {total.priced ? `$${total.costUsd.toFixed(total.costUsd < 1 ? 4 : 2)}` : t("desktop.status.unpriced")}
        </span>
      )}
    </>
  );
}
