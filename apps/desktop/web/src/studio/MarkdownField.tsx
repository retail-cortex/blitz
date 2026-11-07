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

import { lazy, Suspense, useId, useState, type ReactNode } from "react";
import { Segmented } from "@blitz/webui";
import { t } from "../i18n";
import { SuggestTextarea } from "../SecretSuggest";

const VisualEditor = lazy(() => import("../files/visual/VisualEditor").then((m) => ({ default: m.VisualEditor })));

/** How a Markdown field is shown: rendered, or its text. */
export type MarkdownView = "visual" | "source";

// The view last chosen, for the next field opened: one choice holds
// across Studio until the window reloads.
let lastView: MarkdownView = "visual";

const noop = () => {};

/**
 * A Markdown field in a Studio form (an agent's instructions, a skill's,
 * a rule, a guardrail, a job's prompt, a workflow step's, a spec; spec_040
 * STU-05a): Visual edits it rendered, as the editor's Visual (spec_037),
 * Source as text with secret suggestions. Tools (Redraft) sit under it,
 * on the right.
 */
export function MarkdownField({
  label,
  supporting,
  error,
  value,
  onValue,
  dir,
  path = "prompt.md",
  workspace,
  placeholder,
  rows = 8,
  height,
  disabled,
  tools,
  onSave,
}: {
  label: string;
  supporting?: ReactNode;
  error?: string;
  value: string;
  onValue: (v: string) => void;
  /** The workspace's folder: links and pictures are relative to path in it. */
  dir: string;
  path?: string;
  /** The workspace whose secrets Source suggests ("" for none). */
  workspace: string;
  placeholder?: string;
  rows?: number;
  /** Visual's height, in pixels (else from rows). */
  height?: number;
  disabled?: boolean;
  /** Under the field, on the right: Redraft and the like. */
  tools?: ReactNode;
  /** ⌘S in Visual. */
  onSave?: () => void;
}) {
  const id = useId();
  const [view, setView] = useState<MarkdownView>(lastView);
  const visual = view === "visual" && !disabled && !!dir;
  return (
    <div className="field markdown-field">
      <div className="row markdown-field-head">
        <label htmlFor={id} className="spacer">
          {label}
        </label>
        {!disabled && !!dir && (
          <Segmented<MarkdownView>
            small
            label={t("desktop.files.view_as")}
            value={view}
            options={[
              { value: "visual", label: t("desktop.files.visual") },
              { value: "source", label: t("desktop.files.source") },
            ]}
            onChange={(v) => {
              lastView = v;
              setView(v);
            }}
          />
        )}
      </div>
      {visual ? (
        <div id={id} className="preview preview-markdown visual-host markdown-field-visual" style={{ height: height ?? Math.max(200, rows * 26 + 64), ["--placeholder" as string]: JSON.stringify(placeholder ?? "") }}>
          <Suspense fallback={null}>
            <VisualEditor dir={dir} path={path} text={value} onChange={onValue} onSave={onSave ?? noop} autoFocus={false} />
          </Suspense>
        </div>
      ) : (
        <SuggestTextarea id={id} className="input agent-prompt" value={value} rows={rows} spellCheck={false} placeholder={placeholder} disabled={disabled} onValue={onValue} workspace={workspace} />
      )}
      {error ? <span className="supporting error-text">{error}</span> : supporting && <span className="supporting">{supporting}</span>}
      {tools && <div className="row markdown-field-tools">{tools}</div>}
    </div>
  );
}
