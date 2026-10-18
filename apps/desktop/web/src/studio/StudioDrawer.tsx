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
import { mdiBookOpenPageVariantOutline, mdiCalendarClock, mdiRefresh, mdiFlaskOutline, mdiKeyVariant, mdiPuzzleOutline, mdiRobotOutline, mdiScaleBalance, mdiShieldLockOutline, mdiSitemapOutline } from "@mdi/js";
import { AgentEditor } from "../AgentEditor";
import { workspaces } from "../api";
import { message } from "../errors";
import type { ItemIntent } from "../intent";
import { reloadSummary } from "../reload";
import { AgentScope } from "../gen/blitz/v1/workspace_pb";
import { t } from "../i18n";
import { SecretsEditor } from "../SecretsEditor";
import { Icon, IconButton, useSnackbar } from "../ui/controls";
import { Drawer } from "../ui/Drawer";
import { EvalsPane } from "./EvalsPane";
import { GuardrailsPane } from "./GuardrailsPane";
import { RulesPane } from "./RulesPane";
import { SkillsPane } from "./SkillsPane";
import { WorkersPane } from "./WorkersPane";
import { WorkflowsPane } from "./WorkflowsPane";

/**
 * The kinds Studio holds (spec_studio_040), in the order an agent is
 * built: the agent and the evals that prove it, what it follows (skills,
 * rules, guardrails), what runs (workflows, workers), and secrets.
 */
export const studioKinds = ["agents", "evals", "skills", "rules", "guardrails", "workflows", "workers", "secrets"] as const;
/** A kind of item in Studio. */
export type StudioKind = (typeof studioKinds)[number];

/** What Studio opens on: a kind, and for workers what to show (a worker, a new one). */
export interface StudioOpen {
  kind: StudioKind;
  intent?: ItemIntent;
}

/** Each kind's icon. */
export const studioIcons: Record<StudioKind, string> = {
  agents: mdiRobotOutline,
  skills: mdiPuzzleOutline,
  workflows: mdiSitemapOutline,
  workers: mdiCalendarClock,
  evals: mdiFlaskOutline,
  rules: mdiScaleBalance,
  guardrails: mdiShieldLockOutline,
  secrets: mdiKeyVariant,
};

/**
 * The workspace's Studio (spec_studio_040): what its .agents folder
 * holds, kind by kind, each listed and edited here: agents, skills,
 * workflows, evals, rules, guardrails, and its own secrets.
 */
export function StudioDrawer({ dir, initial, onClose, onWorkers }: { dir: string; initial: StudioOpen; onClose: () => void; onWorkers?: () => void }) {
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
  const choose = (k: StudioKind) => {
    setDirty(false);
    setKind(k);
  };
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
      className="settings-drawer studio-drawer"
      dirty={dirty}
      headerActions={<IconButton icon={mdiRefresh} label={t("desktop.menu.agents.reload")} onClick={() => void reload()} />}
    >
      <div className="settings">
        <div className="settings-nav list" role="tablist">
          {studioKinds.map((k) => (
            <button key={k} role="tab" aria-selected={k === kind} data-autofocus={k === kind ? "" : undefined} className={`list-item ${k === kind ? "active" : ""}`} title={t(`desktop.studio.${k}`)} onClick={() => choose(k)}>
              <Icon path={studioIcons[k]} />
              <span className="settings-nav-label">{t(`desktop.studio.${k}`)}</span>
            </button>
          ))}
        </div>
        <div className="settings-pane" key={listed}>
          {kind === "agents" && (
            <AgentEditor
              workspace={dir}
              scope={AgentScope.WORKSPACE}
              slide
              onDirty={setDirty}
              onEvals={(agent) => {
                setEvalsFor(agent);
                choose("evals");
              }}
            />
          )}
          {kind === "skills" && <SkillsPane workspace={dir} onDirty={setDirty} />}
          {kind === "rules" && <RulesPane workspace={dir} onDirty={setDirty} onGuardrails={() => choose("guardrails")} />}
          {kind === "workers" && <WorkersPane dir={dir} intent={initial.kind === "workers" ? initial.intent : undefined} onLeave={onClose} onDirty={setDirty} />}
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
          {kind === "guardrails" && <GuardrailsPane workspace={dir} onDirty={setDirty} onRules={() => choose("rules")} />}
          {kind === "evals" && <EvalsPane workspace={dir} onDirty={setDirty} forAgent={evalsFor} onHandled={() => setEvalsFor("")} />}
          {kind === "workflows" && <WorkflowsPane workspace={dir} onDirty={setDirty} />}
        </div>
      </div>
    </Drawer>
  );
}
