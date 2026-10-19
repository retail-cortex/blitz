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

// The composer's session as the page shows it (spec_composer_045).

import type { ComposeSession, WorkflowFile } from "../gen/blitz/v1/workspace_pb";

/** The question the composer waits on, with its suggested answers, if it asks one. */
export function question(s: Pick<ComposeSession, "asking" | "turns"> | undefined): { text: string; options: string[] } | undefined {
  if (!s?.asking) return undefined;
  const last = s.turns[s.turns.length - 1];
  return last ? { text: last.text, options: last.options } : undefined;
}

/** A drafted workflow's steps in order of their links, each as its title (or type) and type. */
export function draftSteps(wf: Pick<WorkflowFile, "nodes" | "edges"> | undefined): { id: string; title: string; type: string }[] {
  if (!wf) return [];
  const fed = new Map<string, number>();
  for (const e of wf.edges) fed.set(e.to, (fed.get(e.to) ?? 0) + 1);
  const next = new Map<string, string[]>();
  for (const e of wf.edges) next.set(e.from, [...(next.get(e.from) ?? []), e.to]);
  // Breadth first from the steps nothing links to; the rest after, as listed.
  const order: string[] = [];
  const queue = wf.nodes.filter((n) => !fed.has(n.id)).map((n) => n.id);
  while (queue.length) {
    const id = queue.shift()!;
    if (order.includes(id)) continue;
    order.push(id);
    queue.push(...(next.get(id) ?? []));
  }
  for (const n of wf.nodes) if (!order.includes(n.id)) order.push(n.id);
  return order.map((id) => wf.nodes.find((n) => n.id === id)!).map((n) => ({ id: n.id, title: n.title || n.id, type: n.type }));
}
