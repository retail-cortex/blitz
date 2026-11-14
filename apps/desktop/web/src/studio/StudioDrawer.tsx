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
import { mdiViewDashboardOutline, mdiTextBoxCheckOutline, mdiBookOpenPageVariantOutline, mdiCalendarClock, mdiRefresh, mdiFlaskOutline, mdiKeyVariant, mdiConnection, mdiPuzzleOutline, mdiRobotOutline, mdiScaleBalance, mdiShieldLockOutline, mdiSitemapOutline, mdiBookshelf, mdiTuneVariant } from "@mdi/js";
import { AgentEditor } from "../AgentEditor";
import { workspaces } from "../api";
import { message } from "../errors";
import { intent as newIntent, type ItemIntent } from "../intent";
import { reloadSummary } from "../reload";
import { AgentScope } from "../gen/blitz/v1/workspace_pb";
import { t, tn } from "../i18n";
import { McpSettings } from "../McpSettings";
import { SecretsEditor } from "../SecretsEditor";
import { Icon, IconButton, Segmented, Tabs, useSnackbar } from "@blitz/webui";
import { GlobalAgents } from "./GlobalAgents";
import { useAdvanced } from "../state";
import { studioResourceTabs, studioTabs, type StudioTab } from "../simpleMode";
import { SettingsPane } from "./SettingsPane";
import { Drawer } from "@blitz/webui";
import { useDiscardGuard } from "./paneParts";
import { ConnectorsPane } from "./ConnectorsPane";
import { EvalsPane } from "./EvalsPane";
import { GuardrailsPane } from "./GuardrailsPane";
import { RulesPane } from "./RulesPane";
import { SkillsPane } from "./SkillsPane";
import { SpecsPane } from "./SpecsPane";
import { WorkersPane } from "./WorkersPane";
import { WorkflowsPane } from "./WorkflowsPane";
import { GradesContext } from "./GradeBadge";
import { checkEnded, studioKindOf, useWorkspaceHealth, type HealthKind } from "./health";
import { cost } from "../workflowRuns";
import { OverviewPane } from "./OverviewPane";

/**
 * Studio's side list (spec_040_studio STU-02): the Overview of it all
 * (spec_044_health), the workspace's own Settings (STU-12) and its
 * specification (STU-90); its Resources, then what runs, simple to
 * complex (Jobs, Workflows); and what its agents follow (rules,
 * guardrails).
 */
const studioEntries = ["overview", "settings", "specs", "resources", "workers", "workflows", "rules", "guardrails"] as const;
/** An entry of Studio's side list. */
export type StudioEntry = (typeof studioEntries)[number];
/** What Studio can open on: an entry, one of Resources' tabs (agents, skills, connectors, secrets), or the evals (Agents' second tab). */
export type StudioKind = StudioEntry | StudioTab | "evals";
/** Whether s names a kind of Studio's. */
export const isStudioKind = (s: string): s is StudioKind => s === "evals" || (studioEntries as readonly string[]).includes(s) || (studioTabs as readonly string[]).includes(s);
const isResourceTab = (k: StudioKind): k is StudioTab => (studioTabs as readonly string[]).includes(k);
/** The Resources tab a kind shows in: its own, the evals Agents'. */
const resourceTabOf = (k: StudioKind): StudioTab | undefined => (k === "evals" ? "agents" : isResourceTab(k) ? k : undefined);
/** The side list's entry a kind shows under: Resources for its tabs (and the evals). */
export const studioEntryOf = (k: StudioKind): StudioEntry => (resourceTabOf(k) ? "resources" : (k as StudioEntry));
/** Studio's entries in groups, a line between each (the menu's and the side list's): the workspace; what it has and what runs; what it follows. */
export const studioGroups: readonly (readonly StudioEntry[])[] = [["overview", "settings", "specs"], ["resources", "workers", "workflows"], ["rules", "guardrails"]];

/**
 * The entries shown: all of them with advanced settings; a newcomer, who
 * opens Studio from the Workspaces menu's pencil or the status bar, gets
 * the workspace's Settings and its Resources (simpleMode.ts).
 */
export function studioEntriesShown(advanced: boolean): StudioEntry[] {
  return advanced ? [...studioEntries] : ["settings", "resources"];
}

/** The kind to show for one asked for: Resources is its first tab shown; one hidden, the workspace's Settings. */
export function studioKindShown(asked: StudioKind, advanced: boolean): StudioKind {
  const tabs = studioResourceTabs(advanced);
  if (asked === "resources") return tabs[0] ?? "settings";
  const tab = resourceTabOf(asked);
  if (tab) return tabs.includes(tab) ? asked : "settings";
  return studioEntriesShown(advanced).includes(asked as StudioEntry) ? asked : "settings";
}

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
  settings: mdiTuneVariant,
  specs: mdiTextBoxCheckOutline,
  resources: mdiBookshelf,
  agents: mdiRobotOutline,
  skills: mdiPuzzleOutline,
  evals: mdiFlaskOutline,
  workers: mdiCalendarClock,
  connectors: mdiConnection,
  secrets: mdiKeyVariant,
  workflows: mdiSitemapOutline,
  rules: mdiScaleBalance,
  guardrails: mdiShieldLockOutline,
};

/**
 * The workspace's Studio (spec_040_studio): everything that is this
 * workspace's, where Settings is every workspace's: its settings, and
 * what its .agents folder holds, each listed and edited here. Resources
 * has Settings › Resources' tabs but Browser (profiles are every
 * workspace's): agents (with their evals), skills, connectors and secrets.
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
  const advanced = useAdvanced();
  const [requested, setKind] = useState<StudioKind>(initial.kind);
  const kind = studioKindShown(requested, advanced);
  const entry = studioEntryOf(kind);
  const tabs = studioResourceTabs(advanced);
  // Whether the kind's form holds unsaved changes: closing asks first.
  const [dirty, setDirty] = useState(false);
  // The agent Studio › Resources › Agents › Evals opens for (from an agent's form).
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
    setKind(studioKindShown(k, advanced));
  };
  // Picking another kind with a form's changes unsaved asks first.
  const { leave, discard } = useDiscardGuard(dirty);
  useEffect(() => onKind?.(kind), [kind, onKind]);
  // Only a request made while it's open: the count it opened with is old.
  const menuAsked = useRef(ask?.n ?? 0);
  useEffect(() => {
    if (!ask || ask.n === menuAsked.current) return;
    menuAsked.current = ask.n;
    if (studioKindShown(ask.kind, advanced) !== kind) choose(ask.kind);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ask?.n]);
  // Asked again for a workflow's run while open (the status bar, an
  // alert): back to Workflows, whose pane opens it.
  const asked = initial.intent?.n;
  useEffect(() => {
    if (asked && initial.kind === "workflows") setKind("workflows");
  }, [asked, initial.kind]);
  // The dashboard, followed while Studio is open: the Overview's, and
  // each list's grade badges (spec_044_health).
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
      headerActions={
<IconButton icon={mdiRefresh} label={t("desktop.menu.agents.reload")} onClick={() => void reload()} />
      }
    >
      <div className="settings">
        <div className="settings-nav list" role="tablist">
          {studioGroups.map((group, i) => {
            const shown = group.filter((e) => studioEntriesShown(advanced).includes(e));
            return [
              i > 0 && shown.length > 0 && advanced && <span key={`line-${i}`} className="studio-nav-line" aria-hidden="true" />,
              ...shown.map((e) => (
                <button key={e} role="tab" aria-selected={e === entry} data-autofocus={e === entry ? "" : undefined} className={`list-item ${e === entry ? "active" : ""}`} title={t(`desktop.studio.${e}`)} onClick={() => (e === entry ? undefined : leave(() => choose(e)))}>
                  <Icon path={studioIcons[e]} />
                  <span className="settings-nav-label">{t(`desktop.studio.${e}`)}</span>
                </button>
              )),
            ];
          })}
        </div>
        <GradesContext.Provider value={grades}>
        <div className={`settings-pane ${entry === "resources" ? "studio-resources" : ""}`} key={listed}>
          {/* Resources' tabs, the same as Settings › Resources'. */}
          {entry === "resources" && tabs.length > 1 && (
            <Tabs label={t("desktop.studio.resources")} value={resourceTabOf(kind) ?? "agents"} tabs={tabs.map((k) => ({ value: k, label: t(`desktop.studio.${k}`), icon: studioIcons[k] }))} onChange={(k) => leave(() => choose(k))} />
          )}
          {/* Agents' two tabs: the agents, and the evals that prove them (STU-13). */}
          {(kind === "agents" || kind === "evals") && (
            <div className="row agents-switch">
              <Segmented small label={t("desktop.studio.agents")} value={kind} options={[{ value: "agents", label: t("desktop.studio.agents"), icon: studioIcons.agents }, { value: "evals", label: t("desktop.studio.evals"), icon: studioIcons.evals }]} onChange={(k) => leave(() => choose(k))} />
            </div>
          )}
          {kind === "settings" && <SettingsPane workspace={dir} />}
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
              listFooter={<GlobalAgents workspace={dir} />}
            />
          )}
          {kind === "specs" && <SpecsPane workspace={dir} onDirty={setDirty} onLeave={onClose} />}
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
          {kind === "connectors" && <ConnectorsPane workspace={dir} onDirty={setDirty} extra={<McpSettings embedded workspace={dir} />} />}
          {kind === "guardrails" && <GuardrailsPane workspace={dir} onDirty={setDirty} onRules={() => choose("rules")} />}
          {kind === "evals" && <EvalsPane workspace={dir} onDirty={setDirty} forAgent={evalsFor} onHandled={() => setEvalsFor("")} />}
          {kind === "workflows" && <WorkflowsPane workspace={dir} onDirty={setDirty} onCanvas={setCompact} intent={intentFor("workflows")} run={initial.run} />}
        </div>
        </GradesContext.Provider>
      </div>
      {discard(t(`desktop.studio.${entry}`))}
    </Drawer>
  );
}
