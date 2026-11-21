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

import { create, type MessageInitShape } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { fakeReasoning } from "../dev/fakeReasoning";
import { ReasoningNodeSchema } from "../gen/blitz/v1/session_pb";
import { argSummary, isOpen, layoutReasoning, liveId, liveTail, readingOrder, type GraphView, type Laid } from "./reasoningGraph";

const graph = fakeReasoning(1, 0).nodes;
const view = (v: Partial<GraphView> = {}): GraphView => ({ thoughts: true, tools: true, words: true, toggled: new Set(), ...v });
const node = (n: MessageInitShape<typeof ReasoningNodeSchema>) => create(ReasoningNodeSchema, n);

function overlaps(laid: Laid): string[] {
  const out: string[] = [];
  laid.nodes.forEach((a, i) =>
    laid.nodes.slice(i + 1).forEach((b) => {
      const apart = a.position.x + a.width! <= b.position.x || b.position.x + b.width! <= a.position.x || a.position.y + a.height! <= b.position.y || b.position.y + b.height! <= a.position.y;
      if (!apart) out.push(`${a.id}×${b.id}`);
    }),
  );
  return out;
}

describe("layoutReasoning", () => {
  const agent = graph.find((n) => n.kind === "agent")!;
  const firstCall = graph.find((n) => n.kind === "call")!;
  // Every combination of what's shown and what's open draws without overlaps.
  const combos: [string, GraphView][] = [];
  for (const thoughts of [true, false])
    for (const tools of [true, false])
      for (const words of [true, false])
        for (const open of [[], [agent.id], [firstCall.id], [agent.id, firstCall.id]])
          for (const live of [undefined, "", "Looking at the tests"]) combos.push([`t${+thoughts} k${+tools} w${+words} open=${open.join("+") || "-"} live=${live ?? "-"}`, view({ thoughts, tools, words, toggled: new Set(open), live })]);
  it.each(combos)("no cards overlap: %s", (_, v) => {
    expect(overlaps(layoutReasoning(graph, v, 1))).toEqual([]);
  });

  it("reads top to bottom: the turn, then its calls in order on one spine", () => {
    const laid = layoutReasoning(graph, view(), 1);
    const calls = laid.nodes.filter((n) => n.data.kind === "call" && !graph.find((g) => g.id === n.id)!.author.startsWith("reviewer"));
    const turn = laid.nodes.find((n) => n.data.kind === "turn")!;
    expect(turn.position.y).toBe(0);
    expect(new Set(calls.map((c) => c.position.x)).size).toBe(1);
    expect(calls.map((c) => c.position.y)).toEqual([...calls.map((c) => c.position.y)].sort((a, b) => a - b));
    expect(laid.edges.filter((e) => e.className?.includes("is-next"))).toHaveLength(calls.length);
  });

  it("puts reasoning left of its call and tools right", () => {
    const laid = layoutReasoning(graph, view(), 1);
    const at = (id: string) => laid.nodes.find((n) => n.id === id)!.position;
    expect(at("n2").x).toBeLessThan(at("n1").x);
    expect(at("n4").x).toBeGreaterThan(at("n1").x);
    expect(laid.edges.find((e) => e.target === "n9")?.className).toContain("is-failed");
  });

  it("folds a sub-agent's calls until opened, then nests them", () => {
    const shut = layoutReasoning(graph, view(), 1);
    expect(shut.nodes.find((n) => n.id === agent.id)?.data).toMatchObject({ open: false, inside: 2 });
    expect(shut.nodes.some((n) => n.id === "n17")).toBe(false);
    const open = layoutReasoning(graph, view({ toggled: new Set([agent.id]) }), 1);
    expect(open.nodes.find((n) => n.id === "n17")!.position.x).toBeGreaterThan(open.nodes.find((n) => n.id === agent.id)!.position.x);
  });

  it("folds a call's reasoning, and hides kinds the view leaves out", () => {
    expect(layoutReasoning(graph, view({ toggled: new Set([firstCall.id]) }), 1).nodes.some((n) => n.id === "n2")).toBe(false);
    const bare = layoutReasoning(graph, view({ thoughts: false, tools: false, words: false }), 1);
    expect(new Set(bare.nodes.map((n) => n.data.kind))).toEqual(new Set(["turn", "call"]));
  });

  it("adds the live card after the last call while one thinks", () => {
    const laid = layoutReasoning(graph, view({ live: "Reading the diff" }), 1);
    const live = laid.nodes.find((n) => n.id === liveId)!;
    expect(live.data.live).toBe("Reading the diff");
    expect(live.position.y).toBe(Math.max(...laid.nodes.map((n) => n.position.y)));
    expect(laid.edges.find((e) => e.target === liveId)?.animated).toBe(true);
    const onlyTurn = layoutReasoning([node({ id: "n0", kind: "turn" })], view({ live: "" }), 1);
    expect(onlyTurn.edges[0]).toMatchObject({ source: "n0", target: liveId });
  });

  it("draws nothing without a turn", () => {
    expect(layoutReasoning([], view(), 1)).toEqual({ nodes: [], edges: [] });
  });

  it("orders cards for the keyboard as they read", () => {
    const order = readingOrder(layoutReasoning(graph, view(), 1));
    expect(order[0]).toBe("n0");
    expect(order.indexOf("n2")).toBeLessThan(order.indexOf("n6"));
  });
});

describe("helpers", () => {
  it.each([
    ["a path first", { path: "a.go", content: "x" }, "a.go"],
    ["a command", { command: "go   test\n./..." }, "go test ./..."],
    ["the first text otherwise", { n: 3, other: "hello" }, "hello"],
    ["nothing", undefined, ""],
    ["long text cut", { query: "x".repeat(80) }, `${"x".repeat(59)}…`],
  ])("argSummary: %s", (_, args, want) => {
    expect(argSummary(args as never)).toBe(want);
  });
  it("liveTail keeps the end", () => {
    expect(liveTail("a ".repeat(200), 10)).toHaveLength(10);
    expect(liveTail("short")).toBe("short");
  });
  it("isOpen: calls open, sub-agents folded, toggled flips them", () => {
    expect(isOpen(node({ id: "a", kind: "call" }), new Set())).toBe(true);
    expect(isOpen(node({ id: "a", kind: "agent" }), new Set())).toBe(false);
    expect(isOpen(node({ id: "a", kind: "agent" }), new Set(["a"]))).toBe(true);
  });
});
