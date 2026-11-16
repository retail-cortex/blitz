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
import { mdiCogOutline, mdiRobotOutline } from "@mdi/js";
import { workspaces } from "../api";
import { message } from "../errors";
import { configChanged, openSettings } from "../events";
import { AgentScope, type AgentFile } from "../gen/blitz/v1/workspace_pb";
import { t } from "../i18n";
import { Icon, IconButton, Switch, useSnackbar } from "@blitz/webui";

/**
 * Studio › Resources › Agents' global agents (spec_040_studio STU-15):
 * the user's own agents, every workspace's, listed under this
 * workspace's, each with a switch for this workspace alone (the person's
 * choice, never the project's) and the gear to edit them in Settings,
 * where they're made and changed. Shows nothing when there are none.
 */
export function GlobalAgents({ workspace }: { workspace: string }) {
  const snack = useSnackbar();
  const [files, setFiles] = useState<AgentFile[]>([]);
  const [busy, setBusy] = useState("");
  const load = useCallback(async () => {
    try {
      const r = await workspaces.listAgentFiles({ workspace, scope: AgentScope.USER });
      setFiles(r.files.filter((f) => f.agent && !f.problem));
    } catch {
      setFiles([]);
    }
  }, [workspace]);
  useEffect(() => {
    void load();
  }, [load]);
  if (files.length === 0) return null;
  const set = async (name: string, shown: string, on: boolean) => {
    setBusy(name);
    try {
      await workspaces.setAgentEnabled({ workspace, name, enabled: on });
      configChanged({ dir: workspace });
      snack(t(on ? "desktop.agents.global_on" : "desktop.agents.global_off", { name: shown }));
      await load();
    } catch (e) {
      snack(message(e), { error: true });
    } finally {
      setBusy("");
    }
  };
  return (
    <section className="stack global-agents" style={{ gap: 4 }}>
      <div className="row">
        <span className="t-title-sm spacer">{t("desktop.agents.global_title")}</span>
        <IconButton icon={mdiCogOutline} label={t("desktop.agents.global_edit")} small onClick={() => openSettings({ section: "agents" })} />
      </div>
      <span className="t-body-sm muted">{t("desktop.agents.global_hint")}</span>
      <div className="list">
        {files.map((f) => {
          const a = f.agent!;
          return (
            <div key={f.path} className={`list-item static-item ${f.offHere ? "is-off" : ""}`}>
              <Icon path={mdiRobotOutline} />
              <span className="lines">
                <span className="ellipsis">{a.displayName || a.name}</span>
                <small className="ellipsis">{f.offHere ? t("desktop.agents.global_off_here") : a.description}</small>
              </span>
              <span className="trailing">
                <Switch label={t("desktop.agents.global_switch", { name: a.displayName || a.name })} checked={!f.offHere} disabled={busy === a.name} onChange={(on) => void set(a.name, a.displayName || a.name, on)} />
              </span>
            </div>
          );
        })}
      </div>
    </section>
  );
}
