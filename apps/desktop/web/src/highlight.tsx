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

// Syntax highlighting for code blocks and diffs: highlight.js grammars
// through lowlight, which returns an element tree (never an HTML string)
// that becomes React elements.
import { Fragment, jsx, jsxs } from "react/jsx-runtime";
import type { ReactNode } from "react";
import { toJsxRuntime } from "hast-util-to-jsx-runtime";
import { common, createLowlight } from "lowlight";

const low = createLowlight(common);

/** Languages by file extension, for diffs. */
const byExtension: Record<string, string> = {
  go: "go", ts: "typescript", tsx: "typescript", js: "javascript", jsx: "javascript", mjs: "javascript", cjs: "javascript",
  py: "python", rb: "ruby", rs: "rust", java: "java", kt: "kotlin", swift: "swift", c: "c", h: "c", cc: "cpp", cpp: "cpp", hpp: "cpp",
  cs: "csharp", php: "php", sh: "bash", bash: "bash", zsh: "bash", json: "json", yaml: "yaml", yml: "yaml", toml: "ini", ini: "ini",
  md: "markdown", css: "css", scss: "scss", less: "less", html: "xml", xml: "xml", svg: "xml", sql: "sql", proto: "protobuf",
  lua: "lua", pl: "perl", r: "r", graphql: "graphql", mk: "makefile", diff: "diff",
};

/** Other names people give a fence's language. */
const aliases: Record<string, string> = { sh: "bash", shell: "bash", zsh: "bash", console: "bash", ts: "typescript", js: "javascript", py: "python", yml: "yaml", toml: "ini", golang: "go", html: "xml", proto: "protobuf" };

/** The grammar for a fence's language or a file's name, if known. */
export function languageFor(langOrPath: string): string | undefined {
  const l = langOrPath.toLowerCase();
  const name = aliases[l] ?? l;
  if (low.registered(name)) return name;
  const base = l.split("/").pop() ?? l;
  if (base === "makefile" || base === "gnumakefile") return "makefile";
  if (base === "dockerfile") return low.registered("dockerfile") ? "dockerfile" : undefined;
  const ext = base.includes(".") ? base.split(".").pop()! : "";
  const byExt = byExtension[ext];
  return byExt && low.registered(byExt) ? byExt : undefined;
}

/** code highlighted as lang (plain when the language is unknown or the code is huge). */
export function highlight(code: string, lang: string | undefined): ReactNode {
  if (!lang || code.length > 100_000) return code;
  try {
    return toJsxRuntime(low.highlight(lang, code), { Fragment, jsx, jsxs });
  } catch {
    return code;
  }
}
