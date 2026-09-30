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

// The editor's CodeMirror setup (spec_files_029 §6): the window's colours,
// languages loaded when first needed, completion from the language and
// from the file's words, and the editor's keys.
import { autocompletion, closeBrackets, closeBracketsKeymap, completeAnyWord, completionKeymap } from "@codemirror/autocomplete";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { bracketMatching, foldGutter, foldKeymap, HighlightStyle, indentOnInput, indentUnit, LanguageDescription, syntaxHighlighting } from "@codemirror/language";
import { languages } from "@codemirror/language-data";
import { gotoLine, highlightSelectionMatches, searchKeymap } from "@codemirror/search";
import { Compartment, EditorSelection, EditorState, type Extension } from "@codemirror/state";
import {
  crosshairCursor,
  drawSelection,
  dropCursor,
  EditorView,
  highlightActiveLine,
  highlightActiveLineGutter,
  highlightSpecialChars,
  keymap,
  lineNumbers,
  rectangularSelection,
} from "@codemirror/view";
import { tags } from "@lezer/highlight";
import { detectIndent } from "./indent";

// The conversation's code colours (app.css --hl-*), so both read alike.
const highlightStyle = HighlightStyle.define([
  { tag: [tags.keyword, tags.controlKeyword, tags.operatorKeyword, tags.moduleKeyword, tags.bool, tags.null, tags.atom], color: "var(--hl-keyword)" },
  { tag: [tags.string, tags.special(tags.string), tags.regexp, tags.character], color: "var(--hl-string)" },
  { tag: [tags.number, tags.integer, tags.float], color: "var(--hl-number)" },
  { tag: [tags.comment, tags.lineComment, tags.blockComment, tags.docComment], color: "var(--hl-comment)", fontStyle: "italic" },
  { tag: [tags.function(tags.variableName), tags.function(tags.propertyName), tags.definition(tags.function(tags.variableName)), tags.heading], color: "var(--hl-title)" },
  { tag: [tags.typeName, tags.className, tags.namespace, tags.definition(tags.typeName)], color: "var(--hl-type)" },
  { tag: [tags.propertyName, tags.attributeName, tags.labelName], color: "var(--hl-attr)" },
  { tag: [tags.meta, tags.tagName, tags.processingInstruction, tags.annotation], color: "var(--hl-meta)" },
  { tag: tags.emphasis, fontStyle: "italic" },
  { tag: tags.strong, fontWeight: "600" },
  { tag: tags.link, textDecoration: "underline" },
  { tag: tags.invalid, color: "var(--md-error)" },
]);

const theme = EditorView.theme({
  "&": { height: "100%", backgroundColor: "var(--md-surface)", color: "var(--md-on-surface)", fontSize: "13px" },
  "&.cm-focused": { outline: "none" },
  ".cm-scroller": { fontFamily: "var(--md-mono)", lineHeight: "20px" },
  ".cm-content": { caretColor: "var(--md-primary)" },
  ".cm-cursor, .cm-dropCursor": { borderLeftColor: "var(--md-primary)" },
  "&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground, ::selection": {
    backgroundColor: "color-mix(in srgb, var(--md-primary) 22%, transparent)",
  },
  ".cm-gutters": { backgroundColor: "var(--md-surface)", color: "var(--md-on-surface-variant)", border: "none" },
  ".cm-activeLine": { backgroundColor: "color-mix(in srgb, var(--md-on-surface) 4%, transparent)" },
  ".cm-activeLineGutter": { backgroundColor: "transparent", color: "var(--md-on-surface)" },
  ".cm-selectionMatch": { backgroundColor: "color-mix(in srgb, var(--md-tertiary) 18%, transparent)" },
  ".cm-matchingBracket": { backgroundColor: "color-mix(in srgb, var(--md-primary) 18%, transparent)", outline: "none" },
  ".cm-searchMatch": { backgroundColor: "color-mix(in srgb, var(--md-warning, #e8a33d) 35%, transparent)" },
  ".cm-panels": { backgroundColor: "var(--md-surface-container)", color: "var(--md-on-surface)" },
  ".cm-panels.cm-panels-top": { borderBottom: "1px solid var(--md-outline-variant)" },
  ".cm-panels.cm-panels-bottom": { borderTop: "1px solid var(--md-outline-variant)" },
  ".cm-panel input, .cm-panel button": { font: "inherit" },
  ".cm-tooltip": { backgroundColor: "var(--md-surface-container-high)", color: "var(--md-on-surface)", border: "1px solid var(--md-outline-variant)", borderRadius: "8px" },
  ".cm-tooltip-autocomplete > ul > li[aria-selected]": { backgroundColor: "var(--md-secondary-container)", color: "var(--md-on-secondary-container)" },
  ".cm-foldPlaceholder": { backgroundColor: "var(--md-surface-container-high)", border: "none", color: "var(--md-on-surface-variant)" },
});

/** What the editor calls back for. */
export interface EditorHooks {
  save: () => void;
  changed: (state: EditorState) => void;
}

const wrapping = new Compartment();

/** The language CodeMirror knows for a file name, if any. */
export function languageOf(path: string): LanguageDescription | null {
  const name = path.slice(path.lastIndexOf("/") + 1);
  return LanguageDescription.matchFilename(languages, name);
}

/** The file's language support (loaded on first use), or nothing. */
export async function languageSupport(path: string): Promise<Extension> {
  const desc = languageOf(path);
  if (!desc) return [];
  try {
    return await desc.load();
  } catch {
    return []; // plain text rather than no editor
  }
}

/**
 * A new editor state for a file's text, with its language (languageSupport)
 * and any extra extensions; words completes words from the file too.
 */
export function fileState(text: string, lang: Extension, hooks: EditorHooks, wrap = false, extra: Extension = [], words = true): EditorState {
  const extensions: Extension[] = [
    lineNumbers(),
    highlightActiveLineGutter(),
    highlightSpecialChars(),
    history(),
    foldGutter(),
    drawSelection(),
    dropCursor(),
    EditorState.allowMultipleSelections.of(true),
    indentOnInput(),
    indentUnit.of(detectIndent(text)),
    syntaxHighlighting(highlightStyle, { fallback: true }),
    bracketMatching(),
    closeBrackets(),
    autocompletion({ activateOnTyping: true }),
    // Words from the file, beside what the language itself offers.
    words ? EditorState.languageData.of(() => [{ autocomplete: completeAnyWord }]) : [],
    rectangularSelection(),
    crosshairCursor(),
    highlightActiveLine(),
    highlightSelectionMatches(),
    keymap.of([
      { key: "Mod-s", preventDefault: true, run: () => (hooks.save(), true) },
      { key: "Ctrl-g", run: gotoLine },
      ...closeBracketsKeymap,
      ...defaultKeymap,
      ...searchKeymap,
      ...historyKeymap,
      ...foldKeymap,
      ...completionKeymap,
      indentWithTab,
    ]),
    lang,
    extra,
    wrapping.of(wrap ? EditorView.lineWrapping : []),
    theme,
    EditorView.updateListener.of((u) => {
      if (u.docChanged) hooks.changed(u.state);
    }),
  ];
  return EditorState.create({ doc: text, extensions });
}

/** Turns soft wrapping on or off. */
export function setWrap(view: EditorView, wrap: boolean) {
  view.dispatch({ effects: wrapping.reconfigure(wrap ? EditorView.lineWrapping : []) });
}

/** Moves the cursor to line (and column), in the middle of the view. */
export function goToLine(view: EditorView, line: number, column = 1) {
  const doc = view.state.doc;
  const l = doc.line(Math.min(Math.max(1, line), doc.lines));
  const pos = Math.min(l.from + Math.max(0, column - 1), l.to);
  view.dispatch({ selection: EditorSelection.cursor(pos), effects: EditorView.scrollIntoView(pos, { y: "center" }) });
  view.focus();
}
