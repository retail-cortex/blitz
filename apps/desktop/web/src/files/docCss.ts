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

// The user's CSS for Markdown previews (spec_files_029 FIL-57): one
// stylesheet per theme, over document.css, kept in the window's prefs.
import type { Prefs } from "../prefs";
import type { Theme } from "../theme";

/** The variables document.css styles previews with, and what each is for. */
export const docVariables: [name: string, about: string][] = [
  ["--doc-font", "body font"],
  ["--doc-heading-font", "heading font"],
  ["--doc-mono", "code font"],
  ["--doc-size", "body text size"],
  ["--doc-line-height", "line height"],
  ["--doc-measure", "widest line"],
  ["--doc-padding", "space around the document"],
  ["--doc-text", "text colour"],
  ["--doc-muted", "quotes, footnotes, small headings"],
  ["--doc-heading", "heading colour"],
  ["--doc-accent", "links, quote bar, checkboxes"],
  ["--doc-rule", "rules and borders"],
  ["--doc-background", "page (transparent: the editor's)"],
  ["--doc-code-bg", "inline code"],
  ["--doc-block-bg", "code blocks"],
  ["--doc-quote-bg", "block quotes"],
  ["--doc-table-head", "table header row"],
  ["--doc-table-stripe", "every other table row"],
  ["--doc-table-rule", "the lines between table rows"],
  ["--doc-diagram-bg", "Mermaid diagrams"],
  ["--doc-radius", "corner radius"],
];

/** What the editor starts with for a theme with no CSS yet: every variable, commented out. */
export function docCssTemplate(theme: Theme): string {
  const width = Math.max(...docVariables.map(([n]) => n.length));
  const lines = docVariables.map(([name, about]) => `  /* ${name}: ; ${" ".repeat(width - name.length)}${about} */`);
  return [
    `/* Markdown previews in the ${theme} theme. Set variables, or style`,
    `   .preview-markdown .markdown and what's in it. */`,
    `.preview-markdown {`,
    ...lines,
    `}`,
    ``,
  ].join("\n");
}

/** What to save for text typed in a theme's editor: nothing for the untouched template. */
export function docCssToSave(theme: Theme, text: string): string {
  return text.trim() === docCssTemplate(theme).trim() ? "" : text;
}

/** The user's CSS for the theme in effect. */
export function docCssFor(prefs: Pick<Prefs, "markdown_css_light" | "markdown_css_dark">, theme: Theme): string {
  return theme === "dark" ? prefs.markdown_css_dark : prefs.markdown_css_light;
}

/** Keeps the user's CSS for the theme in effect in the page, after the defaults. */
export function applyDocCss(css: string, doc: Document = document) {
  let el = doc.getElementById("user-doc-css") as HTMLStyleElement | null;
  if (!css) return el?.remove();
  if (!el) {
    el = doc.createElement("style");
    el.id = "user-doc-css";
    doc.head.appendChild(el);
  }
  if (el.textContent !== css) el.textContent = css;
}
