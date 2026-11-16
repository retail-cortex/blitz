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

import { configChanged, openSettings } from "../events";
import { t } from "../i18n";
import { WorkspaceSettingsForm } from "../WorkspaceSettingsForm";
import { useWorkspaceSettings } from "../workspaceSettings";

/**
 * Studio › Settings (spec_040_studio STU-12): the workspace's own
 * settings (its name and colour, agent, model, approvals, budget,
 * browser, language servers…), which were Settings › Workspaces until
 * Settings became every workspace's. What changes here is told to the
 * window (configChanged), so the status bar and the chat see it; the
 * settings file opens in Settings, on this workspace.
 */
export function SettingsPane({ workspace }: { workspace: string }) {
  const { settings, settingsError, refreshSettings } = useWorkspaceSettings(workspace);
  return (
    <div className="studio-settings">
      <div className="row side-head">
        <span className="t-title-sm spacer">{t("desktop.studio.settings")}</span>
      </div>
      <p className="t-body-sm muted">{t("desktop.studio.settings_intro")}</p>
      <div className="ws-settings-embedded">
        <WorkspaceSettingsForm
          dir={workspace}
          settings={settings}
          error={settingsError}
          onChanged={() => {
            void refreshSettings();
            configChanged({ dir: workspace });
          }}
          onSettingsFile={() => openSettings({ section: "file", scope: workspace })}
        />
      </div>
    </div>
  );
}
