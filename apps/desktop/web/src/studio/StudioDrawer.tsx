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
import { mdiBookOpenPageVariantOutline, mdiCheckDecagramOutline, mdiFlaskOutline, mdiKeyVariant, mdiPuzzleOutline, mdiRobotOutline, mdiScaleBalance, mdiShieldLockOutline, mdiSitemapOutline } from "@mdi/js";
import { AgentEditor } from "../AgentEditor";
import { AgentScope } from "../gen/blitz/v1/workspace_pb";
import { t } from "../i18n";
import { SecretsEditor } from "../SecretsEditor";
import { Icon } from "../ui/controls";
import { Drawer } from "../ui/Drawer";

/** The kinds Studio holds (spec_studio_040), in the menu's order. */
export const studioKinds = ["agents", "skills", "workflows", "evals", "rules", "guardrails", "secrets"] as const;
/** A kind of item in Studio. */
export type StudioKind = (typeof studioKinds)[number];

/** Each kind's icon. */
export const studioIcons: Record<StudioKind, string> = {
  agents: mdiRobotOutline,
  skills: mdiPuzzleOutline,
  workflows: mdiSitemapOutline,
  evals: mdiFlaskOutline,
  rules: mdiScaleBalance,
  guardrails: mdiShieldLockOutline,
  secrets: mdiKeyVariant,
};

/** Where each kind's files are, in the workspace (secrets are in the keychain). */
const folders: Partial<Record<StudioKind, string>> = {
  skills: ".agents/skills/<name>/SKILL.md",
  workflows: ".agents/workflows/<name>.flow.yaml",
  evals: ".agents/evals/<name>.md",
  rules: ".agents/rules/<name>.md",
  guardrails: ".agents/guardrails/<name>.md",
};

/**
 * The workspace's Studio (spec_studio_040): what its .agents folder
 * holds, kind by kind, each listed and edited here: agents, skills,
 * workflows, evals, rules, guardrails, and its own secrets.
 */
export function StudioDrawer({ dir, initial, onClose }: { dir: string; initial: StudioKind; onClose: () => void }) {
  const [kind, setKind] = useState<StudioKind>(initial);
  return (
    <Drawer title={t("desktop.studio")} icon={mdiBookOpenPageVariantOutline} onClose={onClose} className="settings-drawer studio-drawer">
      <div className="settings">
        <div className="settings-nav list" role="tablist">
          {studioKinds.map((k) => (
            <button key={k} role="tab" aria-selected={k === kind} data-autofocus={k === kind ? "" : undefined} className={`list-item ${k === kind ? "active" : ""}`} title={t(`desktop.studio.${k}`)} onClick={() => setKind(k)}>
              <Icon path={studioIcons[k]} />
              <span className="settings-nav-label">{t(`desktop.studio.${k}`)}</span>
            </button>
          ))}
        </div>
        <div className="settings-pane">
          {kind === "agents" && <AgentEditor workspace={dir} scope={AgentScope.WORKSPACE} slide />}
          {kind === "secrets" && <SecretsEditor workspace={dir} />}
          {folders[kind] && <Coming kind={kind} />}
        </div>
      </div>
    </Drawer>
  );
}

// A kind whose editor comes in a later step: what it is, and where its
// files go.
function Coming({ kind }: { kind: StudioKind }) {
  return (
    <div className="stack studio-coming" style={{ gap: 8 }}>
      <p className="t-body">{t(`desktop.studio.${kind}.about`)}</p>
      <p className="t-body-sm muted">
        <Icon path={mdiCheckDecagramOutline} size="sm" /> {t("desktop.studio.coming", { path: folders[kind] ?? "" })}
      </p>
    </div>
  );
}
