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

// Workspace search's rules, apart from the dialog (spec_search_035): which
// sources a search uses, what a hit opens, and which parts of a snippet
// to mark.

/** The sources workspace search knows, in order. */
export const searchSources = ["files", "documents", "chats", "notes"] as const;
/** One of them. */
export type SearchSource = (typeof searchSources)[number];
/** What a search uses until the user picks: files and PDFs. */
export const defaultSources: SearchSource[] = ["files", "documents"];

/** What a hit leads to: a file at a line, a chat, or nothing to open. */
export type HitTarget = { kind: "file"; path: string; line?: number } | { kind: "chat"; id: string } | { kind: "none" };

/**
 * Where a hit goes: files and documents (and plans, saved in the
 * workspace) open in the editor, a PDF at its start; a chat loads; a note
 * kept outside the workspace opens nothing.
 */
export function hitTarget(hit: { source: string; ref: string; line: number }): HitTarget {
  switch (hit.source) {
    case "files":
      return { kind: "file", path: hit.ref, line: hit.line || undefined };
    case "documents":
      return { kind: "file", path: hit.ref, line: hit.ref.toLowerCase().endsWith(".pdf") ? undefined : hit.line || undefined };
    case "chats":
      return { kind: "chat", id: hit.ref };
    case "notes":
      return hit.ref.startsWith(".blitz/plans/") ? { kind: "file", path: hit.ref, line: hit.line || undefined } : { kind: "none" };
  }
  return { kind: "none" };
}

/** The words and "quoted phrases" of a query, lowercased, longest first. */
export function queryTerms(query: string): string[] {
  const out: string[] = [];
  for (const m of query.matchAll(/"([^"]*)"|(\S+)/g)) {
    const t = (m[1] ?? m[2] ?? "").trim().toLowerCase();
    if (t) out.push(t);
  }
  return out.sort((a, b) => b.length - a.length);
}

/** A snippet cut into parts, those that match a term marked. */
export function markTerms(text: string, terms: string[]): { text: string; mark: boolean }[] {
  if (!terms.length || !text) return text ? [{ text, mark: false }] : [];
  const escaped = terms.map((t) => t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"));
  const re = new RegExp(`(${escaped.join("|")})`, "gi");
  const out: { text: string; mark: boolean }[] = [];
  let at = 0;
  for (const m of text.matchAll(re)) {
    const i = m.index ?? 0;
    if (i > at) out.push({ text: text.slice(at, i), mark: false });
    out.push({ text: m[0], mark: true });
    at = i + m[0].length;
  }
  if (at < text.length) out.push({ text: text.slice(at), mark: false });
  return out;
}

/** The sources after toggling one; the last one can't be turned off. */
export function toggleSource(on: SearchSource[], s: SearchSource): SearchSource[] {
  if (on.includes(s)) return on.length > 1 ? on.filter((x) => x !== s) : on;
  return searchSources.filter((x) => x === s || on.includes(x));
}

/**
 * The words to mark where a hit opens: the query's terms, a long word cut
 * as the index's stemmer does ("indexing" marks "indexed").
 */
export function findTerms(query: string): string[] {
  return [...new Set(queryTerms(query).map((t) => (!t.includes(" ") && [...t].length > 5 ? [...t].slice(0, -3).join("") : t)))].sort((a, b) => b.length - a.length);
}

/**
 * Where terms are in text, as [start, end) offsets, in order, apart; with
 * toWordEnd, each to the end of its word (a cut term marks all of
 * "rounding").
 */
export function matchSpans(text: string, terms: string[], toWordEnd = true): [number, number][] {
  const out: [number, number][] = [];
  let at = 0;
  for (const p of markTerms(text, terms)) {
    const start = at;
    at += p.text.length;
    if (!p.mark || (out.length && start < out[out.length - 1][1])) continue;
    let end = at;
    while (toWordEnd && end < text.length && /[\p{L}\p{N}_]/u.test(text[end])) end++;
    out.push([start, end]);
  }
  return out;
}

/**
 * Which of a document's blocks, by the source line each starts at (in
 * order), holds line: the last that starts at or before it (-1: none).
 */
export function blockAt(starts: number[], line: number): number {
  let at = -1;
  starts.forEach((s, i) => {
    if (s <= line) at = i;
  });
  return at;
}
