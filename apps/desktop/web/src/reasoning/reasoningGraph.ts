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

import type { Edge, Node } from "@xyflow/react";
import type { JsonObject } from "@bufbuild/protobuf";
import type { ReasoningNode } from "../gen/blitz/v1/session_pb";

/*
 * The reasoning graph's layout (spec_066_reasoning RSN-10): a turn reads
 * top to bottom as the chat does. Its model calls make a spine; each
 * call's steps of reasoning stand to its left, and the tools it chose,
 * the sub-agents it asked and its words to its right. An open sub-agent
 * draws its own spine further right, under it.
 */

/** A card's kind: a node's, or the live one while a call is thinking. */
export type CardKind = "turn" | "call" | "thought" | "tool" | "agent" | "text" | "live";

/** Each kind's card size, fixed so the layout needn't measure. */
export const cardSize: Record<CardKind, { w: number; h: number }> = {
  turn: { w: 360, h: 100 },
  call: { w: 280, h: 76 },
  thought: { w: 280, h: 84 },
  tool: { w: 260, h: 64 },
  agent: { w: 260, h: 76 },
  text: { w: 280, h: 84 },
  live: { w: 260, h: 76 },
};

const gapX = 56;
const gapY = 14;
const rowGap = 36;

/** What's drawn: which kinds, and the live call's text while one thinks. */
export interface GraphView {
  thoughts: boolean;
  tools: boolean;
  words: boolean;
  /** Nodes the person opened or folded, against each kind's default (a call's steps open, a sub-agent's calls folded). */
  toggled: ReadonlySet<string>;
  /** The live call's streamed reasoning, or "" while it works without it; undefined when nothing runs. */
  live?: string;
}

/** What a card shows. */
export type CardData = {
  kind: CardKind;
  node?: ReasoningNode;
  /** Its steps of reasoning (a call) or its calls (a sub-agent), drawn or not. */
  inside: number;
  open: boolean;
  /** The live call's text. */
  live?: string;
  /** Its turn's number, from 1 (the turn's card). */
  turn?: number;
};

/** A laid-out graph. */
export interface Laid {
  nodes: Node<CardData>[];
  edges: Edge[];
}

/** The live card's ID. */
export const liveId = "live";

/** Whether n is open: a call's steps start open, a sub-agent's calls folded. */
export function isOpen(n: ReasoningNode, toggled: ReadonlySet<string>): boolean {
  return (n.kind === "agent" ? false : true) !== toggled.has(n.id);
}

/** Lays out one turn's nodes (GetReasoning's, in order) as view asks. */
export function layoutReasoning(list: ReasoningNode[], view: GraphView, turnNumber: number): Laid {
  const kids = new Map<string, ReasoningNode[]>();
  for (const n of list) {
    const k = kids.get(n.parent) ?? [];
    k.push(n);
    kids.set(n.parent, k);
  }
  const nodes: Node<CardData>[] = [];
  const edges: Edge[] = [];
  const childrenOf = (id: string) => kids.get(id) ?? [];
  const place = (kind: CardKind, x: number, y: number, data: Omit<CardData, "kind">, id?: string) => {
    const s = cardSize[kind];
    nodes.push({ id: id ?? data.node!.id, type: "card", position: { x, y }, data: { kind, ...data }, width: s.w, height: s.h, draggable: false, connectable: false });
  };
  // The spine's links step straight down; links to the sides curve.
  const link = (from: string, to: string, kind: string, sourceHandle: string, targetHandle: string, extra: Partial<Edge> = {}) =>
    edges.push({ id: `${from}-${to}`, source: from, target: to, sourceHandle, targetHandle, type: sourceHandle === "b" ? "smoothstep" : "default", className: `rsn-link is-${kind}`, focusable: false, ...extra });

  // A spine of calls from (x, y): returns its bottom and its last call.
  const spine = (parent: string, x: number, y: number, depth: number): { bottom: number; last?: string } => {
    let prev = "";
    let bottom = y;
    for (const c of childrenOf(parent).filter((n) => n.kind === "call")) {
      const inner = childrenOf(c.id);
      const steps = inner.filter((n) => n.kind === "thought");
      const open = isOpen(c, view.toggled);
      place("call", x, y, { node: c, inside: steps.length, open });
      if (prev) link(prev, c.id, "next", "b", "t");
      prev = c.id;
      let left = y;
      if (open && view.thoughts) {
        const lx = x - gapX - cardSize.thought.w;
        for (const s of steps) {
          place("thought", lx, left, { node: s, inside: 0, open: false });
          link(c.id, s.id, "thought", "l-out", "r-in");
          left += cardSize.thought.h + gapY;
        }
      }
      let right = y;
      const rx = x + cardSize.call.w + gapX;
      for (const r of inner) {
        const kind = r.kind as CardKind;
        if (kind === "thought" || (kind === "text" ? !view.words : !view.tools) || !(kind in cardSize)) continue;
        const calls = kind === "agent" ? childrenOf(r.id).filter((n) => n.kind === "call") : [];
        const agentOpen = kind === "agent" && isOpen(r, view.toggled) && depth < 3;
        place(kind, rx, right, { node: r, inside: calls.length, open: agentOpen });
        link(c.id, r.id, r.status === "failed" ? "failed" : kind, "r-out", "l-in");
        let end = right + cardSize[kind].h;
        if (agentOpen && calls.length) {
          const nx = rx + cardSize.agent.w + gapX + cardSize.thought.w + gapX;
          const ny = right + cardSize.agent.h / 2 + 28;
          const sub = spine(r.id, nx, ny, depth + 1);
          link(r.id, calls[0].id, "agent", "r-out", "t");
          end = Math.max(end, sub.bottom);
        }
        right = end + gapY;
      }
      bottom = Math.max(y + cardSize.call.h, left - gapY, right - gapY);
      y = bottom + rowGap;
    }
    return { bottom, last: prev || undefined };
  };

  const turn = list.find((n) => n.kind === "turn");
  if (!turn) return { nodes, edges };
  const x = cardSize.thought.w + gapX;
  place("turn", x + cardSize.call.w / 2 - cardSize.turn.w / 2, 0, { node: turn, inside: 0, open: true, turn: turnNumber });
  const first = childrenOf(turn.id).find((n) => n.kind === "call");
  if (first) link(turn.id, first.id, "next", "b", "t");
  const { bottom, last } = spine(turn.id, x, cardSize.turn.h + rowGap, 0);
  if (view.live !== undefined) {
    place("live", x, first ? bottom + rowGap : cardSize.turn.h + rowGap, { inside: 0, open: false, live: view.live }, liveId);
    link(last ?? turn.id, liveId, "live", "b", "t", { animated: true });
  }
  return { nodes, edges };
}

/** The node IDs in reading order (keyboard moves through them). */
export function readingOrder(laid: Laid): string[] {
  return [...laid.nodes].sort((a, b) => a.position.y - b.position.y || a.position.x - b.position.x).map((n) => n.id);
}

const argKeys = ["path", "file_path", "command", "query", "pattern", "url", "agent_name", "name", "directory", "sql"];

/** A tool call's arguments in a few words: its path, command or query, or its first text. */
export function argSummary(args: JsonObject | undefined, max = 60): string {
  if (!args) return "";
  const pick = argKeys.map((k) => args[k]).find((v) => typeof v === "string" && v.trim()) ?? Object.values(args).find((v) => typeof v === "string" && v.trim());
  const s = typeof pick === "string" ? pick.replace(/\s+/g, " ").trim() : "";
  return s.length > max ? `${s.slice(0, max - 1)}…` : s;
}

/** The last few lines of the live call's text, for its card. */
export function liveTail(text: string, max = 160): string {
  const s = text.replace(/\s+/g, " ").trim();
  return s.length > max ? `…${s.slice(s.length - max + 1)}` : s;
}

