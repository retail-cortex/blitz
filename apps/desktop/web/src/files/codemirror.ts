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
import { gotoLine, highlightSelectionMatches, search, searchKeymap } from "@codemirror/search";
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
  tooltips,
  type KeyBinding,
} from "@codemirror/view";
import { tags } from "@lezer/highlight";
import { detectIndent } from "./indent";
import { t } from "../i18n";

// The conversation's code colours (app.css --hl-*), so both read alike.
const highlightStyle = HighlightStyle.define([
  { tag: [tags.keyword, tags.controlKeyword, tags.operatorKeyword, tags.moduleKeyword, tags.bool, tags.null, tags.atom], color: "var(--hl-keyword)" },
  { tag: [tags.string, tags.special(tags.string), tags.regexp, tags.character], color: "var(--hl-string)" },
  { tag: [tags.number, tags.integer, tags.float], color: "var(--hl-number)" },
  { tag: [tags.comment, tags.lineComment, tags.blockComment, tags.docComment], color: "var(--hl-comment)", fontStyle: "italic" },
  // HTML's and XML's tag names in the title colour, as most editors show them.
  { tag: [tags.function(tags.variableName), tags.function(tags.propertyName), tags.definition(tags.function(tags.variableName)), tags.heading, tags.tagName], color: "var(--hl-title)" },
  { tag: [tags.typeName, tags.className, tags.namespace, tags.definition(tags.typeName)], color: "var(--hl-type)" },
  { tag: [tags.propertyName, tags.attributeName, tags.labelName], color: "var(--hl-attr)" },
  { tag: [tags.meta, tags.angleBracket, tags.processingInstruction, tags.annotation, tags.documentMeta], color: "var(--hl-meta)" },
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
  ".cm-searchMatch": { backgroundColor: "var(--md-tertiary-container)", color: "var(--md-on-tertiary-container)" },
  ".cm-panels": { backgroundColor: "var(--md-surface-container)", color: "var(--md-on-surface)" },
  ".cm-panels.cm-panels-top": { borderBottom: "1px solid var(--md-outline-variant)" },
  ".cm-panels.cm-panels-bottom": { borderTop: "1px solid var(--md-outline-variant)" },
  ".cm-panel input, .cm-panel button": { font: "inherit" },
  // Find (FIL-72), as the preview's find bar looks.
  ".cm-panel.cm-search": { padding: "6px 30px 6px 14px", display: "flex", flexWrap: "wrap", alignItems: "center", gap: "6px" },
  ".cm-panel.cm-search br": { flexBasis: "100%", height: 0 },
  ".cm-panel.cm-search .cm-textfield": {
    margin: 0,
    width: "240px",
    color: "var(--md-on-surface)",
    backgroundColor: "var(--md-surface)",
    border: "1px solid var(--md-outline-variant)",
    borderRadius: "6px",
    padding: "4px 8px",
    outline: "none",
  },
  ".cm-panel.cm-search .cm-textfield:focus": { borderColor: "var(--md-primary)" },
  ".cm-panel.cm-search .cm-button": {
    margin: 0,
    backgroundImage: "none",
    backgroundColor: "transparent",
    color: "var(--md-primary)",
    border: "1px solid var(--md-outline-variant)",
    borderRadius: "999px",
    padding: "2px 12px",
    fontSize: "12px",
  },
  ".cm-panel.cm-search .cm-button:hover": { backgroundColor: "color-mix(in srgb, var(--md-primary) 8%, transparent)" },
  ".cm-panel.cm-search label": { display: "inline-flex", alignItems: "center", gap: "4px", fontSize: "12px", color: "var(--md-on-surface-variant)", margin: 0 },
  ".cm-panel.cm-search [name=close]": { top: "8px", right: "10px", fontSize: "18px", color: "var(--md-on-surface-variant)" },
  ".cm-searchMatch.cm-searchMatch-selected": { backgroundColor: "var(--md-tertiary)", color: "var(--md-on-tertiary)" },
  ".cm-tooltip": { backgroundColor: "var(--md-surface-container-high)", color: "var(--md-on-surface)", border: "1px solid var(--md-outline-variant)", borderRadius: "8px" },
  ".cm-tooltip-autocomplete > ul > li[aria-selected]": { backgroundColor: "var(--md-secondary-container)", color: "var(--md-on-secondary-container)" },
  ".cm-foldPlaceholder": { backgroundColor: "var(--md-surface-container-high)", border: "none", color: "var(--md-on-surface-variant)" },
  // Problems from a language server (spec_visual_editor_037 VE-35), in the
  // window's colours rather than CodeMirror's.
  ".cm-lintRange-error": { backgroundImage: "none", textDecoration: "underline wavy var(--md-error)", textDecorationSkipInk: "none", textUnderlineOffset: "3px" },
  ".cm-lintRange-warning": { backgroundImage: "none", textDecoration: "underline wavy var(--md-warning)", textDecorationSkipInk: "none", textUnderlineOffset: "3px" },
  ".cm-lintRange-info, .cm-lintRange-hint": { backgroundImage: "none", textDecoration: "underline dotted var(--md-on-surface-variant)", textUnderlineOffset: "3px" },
  ".cm-tooltip-lint": { padding: "4px 0" },
  ".cm-diagnostic": { padding: "4px 12px", fontFamily: "var(--md-font)", fontSize: "13px", borderLeft: "3px solid transparent" },
  ".cm-diagnostic-error": { borderLeftColor: "var(--md-error)" },
  ".cm-diagnostic-warning": { borderLeftColor: "var(--md-warning)" },
  ".cm-diagnostic-info, .cm-diagnostic-hint": { borderLeftColor: "var(--md-outline)" },
  ".cm-diagnosticSource": { color: "var(--md-on-surface-variant)", opacity: 1, fontSize: "12px" },
  ".cm-gutter-lint": { width: "12px" },
  ".cm-gutter-lint .cm-gutterElement": { padding: "0 2px" },
  ".cm-lint-marker": { width: "8px", height: "8px", borderRadius: "50%", content: "none", marginTop: "6px" },
  ".cm-lint-marker-error": { backgroundColor: "var(--md-error)" },
  ".cm-lint-marker-warning": { backgroundColor: "var(--md-warning)" },
  ".cm-lint-marker-info": { backgroundColor: "var(--md-outline)" },
  // Hover and completion documentation, as Markdown.
  ".cm-lsp-hover": { maxWidth: "560px", maxHeight: "320px", overflow: "auto", padding: "8px 12px", fontSize: "13px" },
  ".cm-lsp-hover .markdown > :first-child, .cm-completionInfo .markdown > :first-child": { marginTop: 0 },
  ".cm-lsp-hover .markdown > :last-child, .cm-completionInfo .markdown > :last-child": { marginBottom: 0 },
  ".cm-lsp-hover pre, .cm-completionInfo pre": { margin: "4px 0", whiteSpace: "pre-wrap" },
  // A signature is code, not a code block: no head, no copy button, no card.
  ".cm-lsp-hover .code-head, .cm-completionInfo .code-head": { display: "none" },
  ".cm-lsp-hover .code-block, .cm-completionInfo .code-block": { border: "none", background: "none", margin: "0 0 6px", borderRadius: 0 },
  ".cm-lsp-hover .code-block pre, .cm-completionInfo .code-block pre": { padding: 0, background: "none" },
  ".cm-lsp-hover hr, .cm-completionInfo hr": { margin: "6px 0" },
  ".cm-completionInfo": { padding: "8px 12px", maxWidth: "420px", fontSize: "13px" },
  ".cm-completionDetail": { color: "var(--md-on-surface-variant)", fontStyle: "normal", marginLeft: "8px" },
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
    // Markdown highlights its fenced code too, in the fence's language.
    if (desc.name === "Markdown") {
      const { markdown, markdownLanguage } = await import("@codemirror/lang-markdown");
      return markdown({ base: markdownLanguage, codeLanguages: languages });
    }
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
    // Tooltips (completion and its documentation, hover, problems) on the
    // page, so the editor pane's edge doesn't cut them off.
    tooltips({ parent: document.body }),
    // Words from the file, beside what the language itself offers.
    words ? EditorState.languageData.of(() => [{ autocomplete: completeAnyWord }]) : [],
    rectangularSelection(),
    crosshairCursor(),
    highlightActiveLine(),
    highlightSelectionMatches(),
    search({ top: true }),
    EditorState.phrases.of(findPhrases()),
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

/**
 * What a code editor inside a document needs (the visual editor's code
 * blocks): the file editor's look, keys and completion, without line
 * numbers, history (the document's undoes it) or search.
 */
export function embeddedCode(lang: Extension, keys: KeyBinding[]): Extension {
  return [
    highlightSpecialChars(),
    drawSelection(),
    EditorState.allowMultipleSelections.of(true),
    indentOnInput(),
    syntaxHighlighting(highlightStyle, { fallback: true }),
    bracketMatching(),
    closeBrackets(),
    autocompletion({ activateOnTyping: true }),
    EditorState.languageData.of(() => [{ autocomplete: completeAnyWord }]),
    tooltips({ parent: document.body }),
    keymap.of([...keys, ...closeBracketsKeymap, ...completionKeymap, ...defaultKeymap, indentWithTab]),
    lang,
    theme,
  ];
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

// CodeMirror's find panel's words, from the catalogs.
function findPhrases(): Record<string, string> {
  return {
    Find: t("desktop.files.cm.find"),
    Replace: t("desktop.files.cm.replace"),
    next: t("desktop.files.cm.next"),
    previous: t("desktop.files.cm.previous"),
    all: t("desktop.files.cm.all"),
    "match case": t("desktop.files.cm.match_case"),
    regexp: t("desktop.files.cm.regexp"),
    "by word": t("desktop.files.cm.by_word"),
    replace: t("desktop.files.cm.replace_one"),
    "replace all": t("desktop.files.cm.replace_all"),
    close: t("desktop.files.cm.close"),
  };
}
