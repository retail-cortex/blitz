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

// Completing {secrets:…} as it's typed (spec_secrets_039 SEC-09): the
// token before the caret, the names that match it, and the scoped form a
// choice puts in. The composer, Studio's text boxes (SuggestTextarea)
// and CodeMirror (secretCompletion) share it; values are never fetched.
import { config } from "./api";

/** A secret's name and scope, as listed. */
export interface SecretName {
  key: string;
  scope: "global" | "workspace";
}

/** The {secrets:… being typed: where it starts, and what follows "{secrets:". */
export interface SecretToken {
  start: number;
  end: number;
  query: string;
}

/** The token the caret is in, or null. */
export function secretTokenAt(text: string, caret: number): SecretToken | null {
  const m = /\{secrets:([A-Za-z0-9_.:-]*)$/.exec(text.slice(0, caret));
  return m ? { start: m.index, end: caret, query: m[1] } : null;
}

/** A completion: what's shown, its note, and what goes in (an open scope keeps completing). */
export interface SecretOption {
  label: string;
  detail: string;
  insert: string;
  /** A scope: choosing it goes on to that scope's names. */
  scope?: boolean;
}

/**
 * What the query matches: the scopes while it names none (global:, and
 * workspace: when there's a workspace), then names in either scope;
 * after "global:" or "workspace:", that scope's names. A name goes in
 * scoped, {secrets:global:key} or {secrets:workspace:key}.
 */
export function secretOptions(query: string, names: readonly SecretName[], workspace: boolean, labels: { global: string; workspace: string }): SecretOption[] {
  const [scopePart, rest] = query.includes(":") ? [query.slice(0, query.indexOf(":")), query.slice(query.indexOf(":") + 1)] : ["", query];
  const named = (n: SecretName): SecretOption => ({ label: `${n.scope}:${n.key}`, detail: labels[n.scope], insert: `{secrets:${n.scope}:${n.key}}` });
  if (scopePart) {
    if (scopePart !== "global" && !(workspace && scopePart === "workspace")) return [];
    return names.filter((n) => n.scope === scopePart && n.key.startsWith(rest)).map(named);
  }
  const scopes: SecretOption[] = (workspace ? (["global", "workspace"] as const) : (["global"] as const))
    .filter((s) => s.startsWith(rest))
    .map((s) => ({ label: `${s}:`, detail: labels[s], insert: `{secrets:${s}:`, scope: true }));
  return [...scopes, ...names.filter((n) => n.key.startsWith(rest) && (workspace || n.scope === "global")).map(named)];
}

/**
 * Puts a choice in the token's place, taking the rest of the name being
 * typed and a closing brace with it; returns the text and the caret.
 */
export function insertSecret(text: string, token: SecretToken, insert: string): { text: string; caret: number } {
  let after = text.slice(token.end);
  if (!insert.endsWith(":")) after = after.replace(/^[A-Za-z0-9_.:-]*\}?/, "");
  return { text: text.slice(0, token.start) + insert + after, caret: token.start + insert.length };
}

/**
 * The secrets text names that aren't there: {secrets:global:x} without a
 * global x, {secrets:workspace:x} without the workspace's, {secrets:x}
 * without either. Shown as a warning: such a name stays as written.
 */
export function missingSecrets(text: string, names: readonly SecretName[]): string[] {
  const out: string[] = [];
  for (const m of text.matchAll(/\{secrets:(?:(global|workspace):)?([A-Za-z0-9_.-]{1,64})\}/g)) {
    const [whole, scope, key] = m;
    const there = names.some((n) => n.key === key && (!scope || n.scope === scope));
    if (!there && !out.includes(whole)) out.push(whole);
  }
  return out;
}

// The names, by workspace ("" global only), kept for a little while: every
// field asks, and they change only in Settings or Studio.
const cache = new Map<string, { at: number; names: Promise<SecretName[]> }>();
const keepFor = 30_000;

/** The secrets a workspace (or "", none) can name: the global ones and its own. */
export function secretNames(workspace: string): Promise<SecretName[]> {
  const hit = cache.get(workspace);
  if (hit && Date.now() - hit.at < keepFor) return hit.names;
  const names = (async () => {
    const list = async (ws: string, scope: SecretName["scope"]) => (await config.listSecrets({ workspace: ws })).secrets.map((s) => ({ key: s.key, scope }));
    const [global, own] = await Promise.all([list("", "global"), workspace ? list(workspace, "workspace") : Promise.resolve([])]);
    return [...global, ...own];
  })().catch(() => [] as SecretName[]);
  cache.set(workspace, { at: Date.now(), names });
  return names;
}

/** Forgets the names (a secret was added or deleted). */
export function forgetSecretNames() {
  cache.clear();
}
