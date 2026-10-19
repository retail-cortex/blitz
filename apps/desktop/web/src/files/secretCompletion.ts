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

// Completing {secrets:…} in CodeMirror (the settings file, the Files
// editor): the shared rules of secretSuggest.ts as a completion source.
import type { CompletionContext, CompletionResult } from "@codemirror/autocomplete";
import { EditorState, type Extension } from "@codemirror/state";
import { t } from "../i18n";
import { secretNames, secretOptions } from "../secretComplete";

/** {secrets:…} completion for an editor in workspace ("" none: global names only). */
export function secretCompletion(workspace: string): Extension {
  const source = async (ctx: CompletionContext): Promise<CompletionResult | null> => {
    const m = ctx.matchBefore(/\{secrets:[A-Za-z0-9_.:-]*/);
    if (!m) return null;
    const names = await secretNames(workspace);
    const options = secretOptions(m.text.slice("{secrets:".length), names, !!workspace, { global: t("desktop.secrets.scope.global"), workspace: t("desktop.secrets.scope.workspace") });
    if (!options.length) return null;
    return {
      from: m.from,
      options: options.map((o) => ({ label: o.insert, displayLabel: o.label, detail: o.detail, type: "variable", apply: o.insert })),
      filter: false,
    };
  };
  return EditorState.languageData.of(() => [{ autocomplete: source }]);
}
