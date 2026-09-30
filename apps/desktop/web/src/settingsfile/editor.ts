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
// found, marked on their lines and in the gutter, the settings reference
// on hover, completion from it, and the setting at the cursor.
import { insertCompletionText, startCompletion, type Completion, type CompletionContext, type CompletionResult } from "@codemirror/autocomplete";
import { EditorState, RangeSet, StateEffect, StateField, type Extension, type Range } from "@codemirror/state";
import { Decoration, EditorView, gutter, GutterMarker, hoverTooltip, type DecorationSet } from "@codemirror/view";
import { findSetting, keyAt, settingsCompletions, type SettingRef } from "./toml";

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

// A problem's mark in the gutter: a dot, red for an error, with the
// messages on hover.
class ProblemMarker extends GutterMarker {
  constructor(
    readonly error: boolean,
    readonly message: string,
  ) {
    super();
  }
  eq(other: ProblemMarker) {
    return other.error === this.error && other.message === this.message;
  }
  toDOM() {
    const dom = document.createElement("span");
    dom.className = `cm-problem-dot ${this.error ? "error" : "warn"}`;
    dom.title = this.message;
    dom.textContent = this.error ? "✕" : "!";
    return dom;
  }
}

const problemMarkers = StateField.define<RangeSet<GutterMarker>>({
  create: () => RangeSet.empty,
  update(marks, tr) {
    marks = marks.map(tr.changes);
    for (const e of tr.effects) {
      if (!e.is(setProblems)) continue;
      const byLine = new Map<number, LineProblem[]>();
      for (const p of e.value) if (p.line > 0 && p.line <= tr.state.doc.lines) byLine.set(p.line, [...(byLine.get(p.line) ?? []), p]);
      marks = RangeSet.of(
        [...byLine.entries()]
          .sort(([a], [b]) => a - b)
          .map(([line, ps]) =>
            new ProblemMarker(
              ps.some((p) => p.error),
              ps.map((p) => p.message).join("\n"),
            ).range(tr.state.doc.line(line).from),
          ),
      );
    }
    return marks;
  },
});

const theme = EditorView.baseTheme({
  ".cm-problem-gutter": { width: "18px" },
  ".cm-problem-dot": { display: "inline-block", width: "14px", textAlign: "center", font: "700 11px/1.6 var(--md-font)", cursor: "default" },
  ".cm-problem-dot.error": { color: "var(--md-error)" },
  ".cm-problem-dot.warn": { color: "var(--md-warning, #e8a33d)" },
  ".cm-completionInfo.cm-setting-tip": { padding: "8px 10px" },
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

/** The reference's setting a position names (a key or a table's name), if any. */
export function settingAt(state: EditorState, pos: number, reference: SettingRef[]): SettingRef | undefined {
  const line = state.doc.lineAt(pos);
  const span = keyAt(state.doc.toString(), line.number);
  return span ? findSetting(reference, span.path) : undefined;
}

/** Completion from the reference: tables, the table's keys, and values. */
export function settingsCompletionSource(reference: () => SettingRef[], labels: { type: string; default: string }) {
  return (ctx: CompletionContext): CompletionResult | null => {
    const line = ctx.state.doc.lineAt(ctx.pos);
    const before = line.text.slice(0, ctx.pos - line.from);
    const found = settingsCompletions(ctx.state.doc.toString(), line.number, before, reference());
    if (!found || (!found.options.length && !ctx.explicit)) return null;
    // Keys and tables as soon as a line starts; values once asked or typed.
    if (!ctx.explicit && found.from === before.length && !/[=[]\s*"?$/.test(before)) return null;
    return {
      from: line.from + found.from,
      validFor: /^[\w."-]*$/,
      options: found.options.map((o) => ({
        label: o.label,
        // A key goes on to its values.
        apply: o.kind === "key" ? (view: EditorView, _: Completion, from: number, to: number) => (view.dispatch(insertCompletionText(view.state, o.apply, from, to)), startCompletion(view)) : o.apply,
        type: o.kind === "table" ? "namespace" : o.kind === "key" ? "property" : "constant",
        detail: o.setting ? `${o.setting.type}${o.setting.default && o.kind !== "value" ? ` = ${o.setting.default}` : ""}` : undefined,
        info: o.setting ? () => settingTip(o.setting!, labels) : undefined,
        boost: o.kind === "value" && o.label === o.setting?.default ? 1 : 0,
      })),
    };
  };
}

/**
 * The extensions: problems on their lines and in the gutter, on hovering a
 * key or a table's name what the reference says about it, completion from
 * the reference, and onCursor told the setting at the cursor (reference()
 * is read each time, so it can load after the editor).
 */
export function settingsExtensions(reference: () => SettingRef[], labels: { type: string; default: string }, onCursor?: (s: SettingRef | undefined) => void): Extension {
  // One source: completion knows its results by the source that gave them.
  const complete = settingsCompletionSource(reference, labels);
  return [
    problemLines,
    problemMarkers,
    gutter({ class: "cm-problem-gutter", markers: (v) => v.state.field(problemMarkers) }),
    theme,
    EditorState.languageData.of(() => [{ autocomplete: complete }]),
    EditorView.updateListener.of((u) => {
      if (onCursor && (u.selectionSet || u.docChanged)) onCursor(settingAt(u.state, u.state.selection.main.head, reference()));
    }),
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
