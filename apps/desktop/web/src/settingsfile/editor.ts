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

// The settings file editor's CodeMirror extensions: the problems a check
// found, marked on their lines, and the settings reference on hover.
import { StateEffect, StateField, type Extension, type Range } from "@codemirror/state";
import { Decoration, EditorView, hoverTooltip, type DecorationSet } from "@codemirror/view";
import { findSetting, keyAt, type SettingRef } from "./toml";

/** A problem a check found, at a line (0: the whole file). */
export interface LineProblem {
  line: number;
  error: boolean;
  message: string;
}

/** Shows these problems on their lines, replacing any shown. */
export const setProblems = StateEffect.define<LineProblem[]>();

const problemLines = StateField.define<DecorationSet>({
  create: () => Decoration.none,
  update(marks, tr) {
    marks = marks.map(tr.changes);
    for (const e of tr.effects) {
      if (!e.is(setProblems)) continue;
      const byLine = new Map<number, LineProblem[]>();
      for (const p of e.value) if (p.line > 0 && p.line <= tr.state.doc.lines) byLine.set(p.line, [...(byLine.get(p.line) ?? []), p]);
      const ranges: Range<Decoration>[] = [...byLine.entries()]
        .sort(([a], [b]) => a - b)
        .map(([line, ps]) =>
          Decoration.line({
            class: ps.some((p) => p.error) ? "cm-problem-error" : "cm-problem-warn",
            attributes: { title: ps.map((p) => p.message).join("\n") },
          }).range(tr.state.doc.line(line).from),
        );
      marks = Decoration.set(ranges);
    }
    return marks;
  },
  provide: (f) => EditorView.decorations.from(f),
});

const theme = EditorView.baseTheme({
  ".cm-problem-error": { backgroundColor: "color-mix(in srgb, var(--md-error) 14%, transparent)", boxShadow: "inset 3px 0 var(--md-error)" },
  ".cm-problem-warn": { backgroundColor: "color-mix(in srgb, var(--md-warning, #e8a33d) 16%, transparent)", boxShadow: "inset 3px 0 var(--md-warning, #e8a33d)" },
  ".cm-setting-tip": { padding: "8px 10px", maxWidth: "420px", font: "400 12px/17px var(--md-font)" },
  ".cm-setting-tip code": { fontFamily: "var(--md-mono)" },
  ".cm-setting-tip .muted": { color: "var(--md-on-surface-variant)" },
});

/** The setting's reference as a tooltip's content. */
export function settingTip(s: SettingRef, labels: { type: string; default: string }): HTMLElement {
  const dom = document.createElement("div");
  dom.className = "cm-setting-tip";
  const head = dom.appendChild(document.createElement("div"));
  head.appendChild(document.createElement("code")).textContent = s.key;
  const meta = head.appendChild(document.createElement("span"));
  meta.className = "muted";
  meta.textContent = ` · ${labels.type}: ${s.type}${s.default ? ` · ${labels.default}: ${s.default}` : ""}`;
  dom.appendChild(document.createElement("div")).textContent = s.doc;
  return dom;
}

/**
 * The extensions: problems on their lines, and on hovering a key or a
 * table's name, what the reference says about it (reference() is read at
 * each hover, so it can load after the editor).
 */
export function settingsExtensions(reference: () => SettingRef[], labels: { type: string; default: string }): Extension {
  return [
    problemLines,
    theme,
    hoverTooltip((view, pos) => {
      const line = view.state.doc.lineAt(pos);
      const span = keyAt(view.state.doc.toString(), line.number);
      if (!span || pos < line.from + span.from || pos > line.from + span.to) return null;
      const s = findSetting(reference(), span.path);
      if (!s) return null;
      return { pos: line.from + span.from, end: line.from + span.to, above: true, create: () => ({ dom: settingTip(s, labels) }) };
    }),
  ];
}
