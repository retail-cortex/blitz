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

import { mdiCheck, mdiContentCopy } from "@mdi/js";
import { colorLabel, colorNames, workspaceColor } from "./palette";
import { baseName, type WorkspacePrefs } from "./prefs";
import { useApp } from "./state";
import { t } from "./i18n";
import { Field, Icon, IconButton, useSnackbar } from "./ui/controls";

/** How a workspace is shown: its name, description and colour. */
export interface WorkspaceLook {
  name: string;
  description: string;
  color: string;
}

/** A workspace's look as it's kept (unset fields empty, the colour blue). */
export const lookOf = (ws: WorkspacePrefs): WorkspaceLook => ({ name: ws.name ?? "", description: ws.description ?? "", color: ws.color ?? "blue" });

/**
 * The fields for how a workspace is shown (name, description, colour) and
 * its folder, to copy: the Workspace card of a workspace's settings (the
 * panel, and Settings › Workspaces). onBlur says a text field was left (the
 * panel saves then; a colour is saved as it's picked).
 */
export function WorkspaceFields({ ws, value, onChange, onBlur, compact }: { ws: WorkspacePrefs; value: WorkspaceLook; onChange: (v: WorkspaceLook) => void; onBlur?: () => void; compact?: boolean }) {
  const { theme } = useApp();
  const snack = useSnackbar();
  return (
    <>
      <Field label={t("desktop.wsd.name")} supporting={t("desktop.wsd.name_help", { name: baseName(ws.dir) })}>
        {(id) => <input id={id} className="input" value={value.name} placeholder={baseName(ws.dir)} maxLength={60} onChange={(e) => onChange({ ...value, name: e.target.value })} onBlur={onBlur} />}
      </Field>
      <Field label={t("desktop.wsd.description")} supporting={t("desktop.wsd.description_help")}>
        {(id) => <textarea id={id} className="input" rows={compact ? 2 : 3} maxLength={400} value={value.description} onChange={(e) => onChange({ ...value, description: e.target.value })} onBlur={onBlur} />}
      </Field>
      <div className="field">
        <label>{t("desktop.wsd.colour")}</label>
        <div className="swatches" role="radiogroup" aria-label={t("desktop.wsd.colour")}>
          {colorNames.map((c) => (
            <button
              key={c}
              type="button"
              role="radio"
              aria-checked={c === value.color}
              aria-label={colorLabel(c)}
              title={colorLabel(c)}
              className="swatch"
              style={{ background: workspaceColor(c, theme) }}
              onClick={() => onChange({ ...value, color: c })}
            >
              {c === value.color && <Icon path={mdiCheck} size="sm" />}
            </button>
          ))}
        </div>
      </div>
      <div className="field">
        <label>{t("desktop.wsd.folder")}</label>
        <div className="row">
          <code className="ellipsis muted" title={ws.dir}>
            {ws.dir}
          </code>
          <IconButton icon={mdiContentCopy} label={t("desktop.wsd.copy_path")} small onClick={() => navigator.clipboard?.writeText(ws.dir).then(() => snack(t("desktop.wsd.path_copied")))} />
        </div>
      </div>
    </>
  );
}
