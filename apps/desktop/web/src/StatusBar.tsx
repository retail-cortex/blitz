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


import { useEffect, useState } from "react";
import {
  mdiAlertCircleOutline,
  mdiCircleMedium,
  mdiCreationOutline,
  mdiInboxOutline,
  mdiLoading,
  mdiRobotOutline,
  mdiSourceBranch,
  mdiTuneVariant,
} from "@mdi/js";
import { workspaces } from "./api";
import { inboxCounts, useRuns } from "./backgroundRuns";
import { message, reason } from "./errors";
import { configChanged, filesTouchedEvent, showPending, showView } from "./events";
import type { AgentInfo, GetGitStatusResponse } from "./gen/blitz/v1/workspace_pb";
import { t, tn } from "./i18n";
import { effortIcon, efforts, modeOf, modes } from "./options";
import { workspaceColor } from "./palette";
import { displayName } from "./prefs";
import { InboxDialog } from "./RunsInbox";
import { useApp } from "./state";
import { contextShare, tokens, useWorkspaceStatus } from "./status";
import { Icon, Menu, useSnackbar } from "./ui/controls";
import { useWorkerFailures } from "./workerFailures";

/** How often the branch and changed files are asked for while shown. */
const gitPollMs = 10_000;

/**
 * The status bar along the window's foot (spec_desktop_024 DSK-39): the
 * service and the workspace in view, its git branch, what runs, and the
 * agent, model, permission mode, context and cost, with the editor's
 * cursor when a file is shown. Items open what they describe.
 */
export function StatusBar({ serviceUp, version, onWorkspaceSettings }: { serviceUp: boolean; version?: string; onWorkspaceSettings: (dir: string) => void }) {
  const { prefs, activity, theme } = useApp();
  const ws = prefs.workspaces.find((w) => w.dir === prefs.active && w.open);
  const dir = ws?.dir ?? "";
  return (
    <footer className="status-bar t-label-sm" role="contentinfo" aria-label={t("desktop.status.label")}>
      <span className={`sb-item sb-service ${serviceUp ? "up" : "lost"}`} title={serviceUp ? t("desktop.status.service_up", { version: version ?? "" }) : t("desktop.app.lost")}>
        <Icon path={mdiCircleMedium} size="sm" />
        <span className="sb-low">{serviceUp ? t("desktop.status.connected") : t("desktop.status.reconnecting")}</span>
      </span>
      {ws && (
        <>
          <span className="sb-item sb-ws" title={ws.dir}>
            <span className="sb-swatch" style={{ background: workspaceColor(ws.color, theme) }} />
            <span className="ellipsis">{displayName(ws)}</span>
          </span>
          {serviceUp && <GitItem dir={dir} />}
        </>
      )}
      <span className="spacer" />
      {ws && <Activity dir={dir} running={!!activity[dir]?.running} waiting={!!activity[dir]?.waiting} />}
      {serviceUp && <RunsItem />}
      {ws && <FailedItem dir={dir} />}
      {ws && serviceUp && <WorkspaceItems dir={dir} onWorkspaceSettings={() => onWorkspaceSettings(dir)} />}
    </footer>
  );
}

function GitItem({ dir }: { dir: string }) {
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
  const label = git.changed > 0 ? tn("desktop.status.git_changed", git.changed, { branch: git.branch }) : t("desktop.status.git_clean", { branch: git.branch });
  return (
    <button className="sb-item sb-button" title={label} aria-label={label} onClick={() => showView({ dir, view: "changes" })}>
      <Icon path={mdiSourceBranch} size="sm" />
      <span className="ellipsis sb-branch">{git.branch}</span>
      {git.changed > 0 && <span className="sb-count">±{git.changed}</span>}
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
      {open && <InboxDialog list={list} onClose={() => setOpen(false)} />}
    </>
  );
}

function FailedItem({ dir }: { dir: string }) {
  const failed = useWorkerFailures()[dir] ?? 0;
  if (failed === 0) return null;
  const label = tn("desktop.status.failed_runs", failed);
  return (
    <button className="sb-item sb-button sb-error" title={label} aria-label={label} onClick={() => showView({ dir, view: "workers" })}>
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
  const mode = modeOf(s?.permissionMode ?? "default");
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
              detail: [a.description, a.pinnedModel && t("desktop.status.agent_pinned", { model: a.pinnedModel })].filter(Boolean).join(" · "),
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
