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

import { useEffect, useMemo, useRef, useState } from "react";
import { mdiViewDashboardOutline, mdiBookOpenPageVariantOutline, mdiCalendarClock, mdiRefresh, mdiFlaskOutline, mdiKeyVariant, mdiPowerPlugOutline, mdiPuzzleOutline, mdiRobotOutline, mdiScaleBalance, mdiShieldLockOutline, mdiSitemapOutline } from "@mdi/js";
import { AgentEditor } from "../AgentEditor";
import { workspaces } from "../api";
import { message } from "../errors";
import { intent as newIntent, type ItemIntent } from "../intent";
import { reloadSummary } from "../reload";
import { AgentScope } from "../gen/blitz/v1/workspace_pb";
import { t, tn } from "../i18n";
import { SecretsEditor } from "../SecretsEditor";
import { Icon, IconButton, useSnackbar } from "../ui/controls";
import { Drawer } from "../ui/Drawer";
import { ConnectorsPane } from "./ConnectorsPane";
import { EvalsPane } from "./EvalsPane";
import { GuardrailsPane } from "./GuardrailsPane";
import { RulesPane } from "./RulesPane";
import { SkillsPane } from "./SkillsPane";
import { WorkersPane } from "./WorkersPane";
import { WorkflowsPane } from "./WorkflowsPane";
import { GradesContext } from "./GradeBadge";
import { checkEnded, studioKindOf, useWorkspaceHealth, type HealthKind } from "./health";
import { cost } from "../workflowRuns";
import { OverviewPane } from "./OverviewPane";

/**
 * The kinds Studio holds (spec_studio_040), after the Overview of them all
 * (spec_health_044), in the order an agent is built: the agent and the
 * evals that prove it, what it follows (skills, rules, guardrails), what
 * runs (workflows, workers), and what it reaches (connectors, secrets).
 */
export const studioKinds = ["overview", "agents", "evals", "skills", "rules", "guardrails", "workers", "workflows", "connectors", "secrets"] as const;
/** A kind of item in Studio. */
export type StudioKind = (typeof studioKinds)[number];
/** Studio's kinds in groups, a line between each (the menu's and the side list's): the dashboard; the agent and what proves it; what it follows; what runs, simple to complex (jobs, then workflows); what it reaches (connectors, secrets). */
export const studioGroups: readonly (readonly StudioKind[])[] = [["overview"], ["agents", "evals", "skills"], ["rules", "guardrails"], ["workers", "workflows"], ["connectors", "secrets"]];

/** What Studio opens on: a kind, and for workers what to show (a worker, a new one). */
export interface StudioOpen {
  kind: StudioKind;
  intent?: ItemIntent;
  /** For workflows (an intent showing one): the run to show on its canvas. */
  run?: string;
}

/** Each kind's icon. */
export const studioIcons: Record<StudioKind, string> = {
  overview: mdiViewDashboardOutline,
  agents: mdiRobotOutline,
  skills: mdiPuzzleOutline,
  workflows: mdiSitemapOutline,
  workers: mdiCalendarClock,
  evals: mdiFlaskOutline,
  rules: mdiScaleBalance,
  guardrails: mdiShieldLockOutline,
  connectors: mdiPowerPlugOutline,
  secrets: mdiKeyVariant,
};

/**
 * The workspace's Studio (spec_studio_040): what its .agents folder
 * holds, kind by kind, each listed and edited here: agents, skills,
 * workflows, evals, rules, guardrails, the connectors its agents use, and
 * its own secrets.
 */
export function StudioDrawer({
  dir,
  initial,
  onClose,
  onWorkers,
  closeRequest,
  ask,
  onKind,
}: {
  dir: string;
  initial: StudioOpen;
  onClose: () => void;
  onWorkers?: () => void;
  closeRequest?: number;
  /** A kind the menu bar's Studio menu asks for while it's open (n bumped each time): shown as a click on it would. */
  ask?: { kind: StudioKind; n: number };
  /** The kind shown, each time it changes (the menu ticks it). */
  onKind?: (kind: StudioKind) => void;
}) {
  const snack = useSnackbar();
  const [kind, setKind] = useState<StudioKind>(initial.kind);
  // Whether the kind's form holds unsaved changes: closing asks first.
  const [dirty, setDirty] = useState(false);
  // The agent Studio › Evals opens for (from an agent's form).
  const [evalsFor, setEvalsFor] = useState("");
  // Showing the workers sees their failed runs (BL-WK-11).
  useEffect(() => {
    if (kind === "workers") onWorkers?.();
  }, [kind, onWorkers]);
  // A workflow's canvas takes the room: the kinds shrink to their icons.
  const [compact, setCompact] = useState(false);
  const choose = (k: StudioKind) => {
    setDirty(false);
    setCompact(false);
    setKind(k);
  };
  useEffect(() => onKind?.(kind), [kind, onKind]);
  // Only a request made while it's open: the count it opened with is old.
  const menuAsked = useRef(ask?.n ?? 0);
  useEffect(() => {
    if (!ask || ask.n === menuAsked.current) return;
    menuAsked.current = ask.n;
    if (ask.kind !== kind) choose(ask.kind);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ask?.n]);
  // Asked again for a workflow's run while open (the status bar, an
  // alert): back to Workflows, whose pane opens it.
  const asked = initial.intent?.n;
  useEffect(() => {
    if (asked && initial.kind === "workflows") setKind("workflows");
  }, [asked, initial.kind]);
  // The dashboard, followed while Studio is open: the Overview's, and
  // each list's grade badges (spec_health_044).
  const { health, refresh: refreshHealth } = useWorkspaceHealth(dir);
  const grades = useMemo(() => {
    const m = new Map<string, string>();
    for (const it of health?.items ?? []) if (it.verdict?.grade) m.set(`${it.kind}/${it.name}`, it.verdict.grade);
    return { grades: m, judging: health?.checking ? health.progress?.item : undefined };
  }, [health]);
  // A check that ends while Studio is open says how it went.
  const seenHealth = useRef(health);
  useEffect(() => {
    const before = seenHealth.current;
    seenHealth.current = health;
    if (!checkEnded(before, health) || !health) return;
    const errors = health.lastErrors.length;
    snack(tn(errors ? "desktop.health.checked_errors" : "desktop.health.checked", health.lastJudged, { cost: cost(health.lastCostUsd), errors }), { error: errors > 0 });
  }, [health, snack]);
  // An item the Overview opened: its kind's pane shows it.
  const [jump, setJump] = useState<{ kind: StudioKind; intent: ItemIntent }>();
  const openItem = (k: HealthKind, name: string, path: string) => {
    const to = studioKindOf[k] as StudioKind;
    setJump({ kind: to, intent: newIntent("show", k === "agent" ? path : name) });
    choose(to);
  };
  const intentFor = (k: StudioKind) => (jump?.kind === k ? jump.intent : initial.kind === k ? initial.intent : undefined);
  // Bumped to list the shown kind again (its pane starts over, unless it
  // holds unsaved changes).
  const [listed, setListed] = useState(0);
  // Reload: the agent files read again (agents, skills, instructions,
  // rules) and the shown kind listed again: the one refresh in Studio.
  const reload = async () => {
    if (!dirty) setListed((n) => n + 1);
    try {
      const r = await workspaces.reloadAgentFiles({ workspace: dir });
      snack(reloadSummary(r));
      if (r.problems.length) snack(`${t("reload.problems")} ${r.problems.join("; ")}`, { error: true });
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  return (
    <Drawer
      title={t("desktop.studio")}
      icon={mdiBookOpenPageVariantOutline}
      onClose={onClose}
      className={`settings-drawer studio-drawer ${compact ? "is-compact" : ""}`}
      dirty={dirty}
      closeRequest={closeRequest}
      headerActions={<IconButton icon={mdiRefresh} label={t("desktop.menu.agents.reload")} onClick={() => void reload()} />}
    >
      <div className="settings">
        <div className="settings-nav list" role="tablist">
          {studioGroups.map((group, i) => [
            i > 0 && <span key={`line-${i}`} className="studio-nav-line" aria-hidden="true" />,
            ...group.map((k) => (
              <button key={k} role="tab" aria-selected={k === kind} data-autofocus={k === kind ? "" : undefined} className={`list-item ${k === kind ? "active" : ""}`} title={t(`desktop.studio.${k}`)} onClick={() => choose(k)}>
                <Icon path={studioIcons[k]} />
                <span className="settings-nav-label">{t(`desktop.studio.${k}`)}</span>
              </button>
            )),
          ])}
        </div>
        <GradesContext.Provider value={grades}>
        <div className="settings-pane" key={listed}>
          {kind === "overview" && <OverviewPane workspace={dir} health={health} onOpen={openItem} onClose={onClose} onStarted={refreshHealth} />}
          {kind === "agents" && (
            <AgentEditor
              workspace={dir}
              scope={AgentScope.WORKSPACE}
              slide
              intent={intentFor("agents")}
              onDirty={setDirty}
              onEvals={(agent) => {
                setEvalsFor(agent);
                choose("evals");
              }}
            />
          )}
          {kind === "skills" && <SkillsPane workspace={dir} onDirty={setDirty} />}
          {kind === "rules" && <RulesPane workspace={dir} onDirty={setDirty} onGuardrails={() => choose("guardrails")} />}
          {kind === "workers" && <WorkersPane dir={dir} intent={intentFor("workers")} onLeave={onClose} onDirty={setDirty} />}
          {kind === "secrets" && (
            <div className="studio-secrets">
              <div className="row side-head">
                <span className="t-title-sm spacer">{t("desktop.studio.secrets")}</span>
              </div>
              <div className="studio-secrets-body">
                <SecretsEditor workspace={dir} />
              </div>
            </div>
          )}
          {kind === "connectors" && <ConnectorsPane workspace={dir} onDirty={setDirty} />}
          {kind === "guardrails" && <GuardrailsPane workspace={dir} onDirty={setDirty} onRules={() => choose("rules")} />}
          {kind === "evals" && <EvalsPane workspace={dir} onDirty={setDirty} forAgent={evalsFor} onHandled={() => setEvalsFor("")} />}
          {kind === "workflows" && <WorkflowsPane workspace={dir} onDirty={setDirty} onCanvas={setCompact} intent={intentFor("workflows")} run={initial.run} />}
        </div>
        </GradesContext.Provider>
      </div>
    </Drawer>
  );
}
