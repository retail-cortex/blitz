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

import { create } from "@bufbuild/protobuf";
import { Window } from "happy-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { fakeReasoning } from "../dev/fakeReasoning";
import { ReasoningNodeSchema } from "../gen/blitz/v1/session_pb";
import { UsageSchema } from "../gen/blitz/v1/turn_pb";
import { liveThought, showPrompt } from "./chatLink";
import { costOf, duration, tokenLine } from "./ReasoningCards";
import { facts, fullText, withoutHeading } from "./ReasoningDetail";
import { preview } from "./ReasoningCards";

describe("the cards' numbers", () => {
  it.each([
    [0, ""],
    [820, "820 ms"],
    [4200, "4.2 s"],
    [42_000, "42 s"],
    [190_000, "3m 10s"],
  ])("duration(%s) is %s", (ms, want) => {
    expect(duration(BigInt(ms))).toBe(want);
  });
  it("tokenLine names reasoning tokens only when there are some", () => {
    expect(tokenLine(create(UsageSchema, { input: 9288n, output: 631n }))).not.toContain("reasoning");
    expect(tokenLine(create(UsageSchema, { input: 9288n, output: 631n, reasoning: 410n }))).toMatch(/410/);
    expect(tokenLine(undefined)).toBe("");
  });
  it("costOf is empty without a call", () => {
    expect(costOf(create(UsageSchema, {}))).toBe("");
    expect(costOf(create(UsageSchema, { calls: 1, costUsd: 0.0121, priced: true }))).toBe("$0.012");
  });
});

describe("text shown twice, shown once", () => {
  it("withoutHeading drops the heading the title shows", () => {
    expect(withoutHeading("**Plan**\nDo it.", "Plan")).toBe("Do it.");
    expect(withoutHeading("No heading here.", "Plan")).toBe("No heading here.");
  });
  it("preview is plain text going on from its title", () => {
    expect(preview("**Plan**\nUse `retry` here.", "Plan")).toBe("Use retry here.");
    expect(preview("About 8.6 s: the sum.", "About 8.6 s")).toBe("the sum.");
    expect(preview("Other text", "Plan")).toBe("Other text");
  });
});

describe("the detail panel", () => {
  const nodes = fakeReasoning(1, 0).nodes;
  it("lists a call's model, provider, tokens, cost and time", () => {
    const rows = Object.fromEntries(facts(nodes.find((n) => n.kind === "call")!));
    expect(Object.values(rows)).toEqual(expect.arrayContaining(["gemini-3-pro", "google", "$0.012", "3.1 s"]));
  });
  it("copies a tool's call and result, and a thought's text", () => {
    const tool = nodes.find((n) => n.id === "n9")!;
    expect(fullText(tool)).toContain('"command": "go test ./pkg/upload/..."');
    expect(fullText(tool)).toContain("exit status 1");
    expect(fullText(create(ReasoningNodeSchema, { kind: "thought", title: "T", text: "body" }))).toBe("body");
  });
});

describe("the chat link", () => {
  afterEach(() => vi.useRealTimers());
  it("liveThought: the open thought while running, '' between, nothing idle", () => {
    expect(liveThought([{ kind: "thought", text: "hmm", open: true }], true)).toBe("hmm");
    expect(liveThought([{ kind: "thought", text: "hmm", open: false }], true)).toBe("");
    expect(liveThought([], true)).toBe("");
    expect(liveThought([{ kind: "thought", text: "hmm", open: true }], false)).toBeUndefined();
  });
  it("showPrompt scrolls to the turn's prompt and marks it a moment", () => {
    vi.useFakeTimers();
    const win = new Window();
    const scroller = win.document.createElement("div") as unknown as HTMLElement;
    scroller.innerHTML = '<div data-prompt></div><div class="steer"></div><div data-prompt id="second"></div>';
    const second = scroller.querySelector<HTMLElement>("#second")!;
    second.scrollIntoView = vi.fn();
    expect(showPrompt(scroller, 1)).toBe(true);
    expect(second.scrollIntoView).toHaveBeenCalled();
    expect(second.classList.contains("is-pointed")).toBe(true);
    vi.advanceTimersByTime(2000);
    expect(second.classList.contains("is-pointed")).toBe(false);
    expect(showPrompt(scroller, 5)).toBe(false);
    expect(showPrompt(null, 0)).toBe(false);
  });
});
