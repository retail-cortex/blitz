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
import { GetSuggestionsResponseSchema, SuggestionKind } from "./gen/blitz/v1/workspace_pb";
import { maxTiles, pollAttempts, shouldPoll, welcomeTiles } from "./suggestions";

const res = (init: MessageInitShape<typeof GetSuggestionsResponseSchema>) => create(GetSuggestionsResponseSchema, init);
const tiles = (init: MessageInitShape<typeof GetSuggestionsResponseSchema> | undefined) => welcomeTiles(init && res(init)).map((x) => [x.key.replace(/-\d+$/, ""), x.text, x.action]);

describe("welcome tiles", () => {
  const canned = ["explain", "bug", "tests", "improve"];

  it.each([
    ["no response yet", undefined],
    ["a failed or empty one", {}],
  ])("%s: the canned tiles, into the composer", (_, init) => {
    const got = tiles(init);
    expect(got.map(([k]) => k)).toEqual(canned);
    expect(got[0][2]).toEqual({ type: "draft", text: "Explain how this project is organised, and how to build and test it." });
  });

  it.each([
    [
      "the latest conversation, opened",
      { kind: SuggestionKind.CONTINUE, title: "Fix the coupon rounding", sessionId: "s1" },
      ["2", "Continue “Fix the coupon rounding”", { type: "session", id: "s1" }],
    ],
    ["an untitled one", { kind: SuggestionKind.CONTINUE, sessionId: "s2" }, ["2", "Continue “(untitled)”", { type: "session", id: "s2" }]],
    [
      "uncommitted changes, counted",
      { kind: SuggestionKind.CHANGES, count: 3 },
      ["3", "Review my 3 uncommitted changes and propose a commit message.", { type: "send", prompt: "Review my 3 uncommitted changes and propose a commit message." }],
    ],
    [
      "one change",
      { kind: SuggestionKind.CHANGES, count: 1 },
      ["3", "Review my uncommitted change and propose a commit message.", { type: "send", prompt: "Review my uncommitted change and propose a commit message." }],
    ],
    [
      "a failed worker, with why",
      { kind: SuggestionKind.WORKER_FAILED, worker: "deps", detail: "maximum turns reached" },
      ["4", "Find out why the worker deps failed", { type: "send", prompt: "Find out why the worker deps failed (maximum turns reached), and fix what you can." }],
    ],
    ["a failed worker, without", { kind: SuggestionKind.WORKER_FAILED, worker: "deps" }, ["4", "Find out why the worker deps failed", { type: "send", prompt: "Find out why the worker deps failed" }]],
    ["a model's idea", { kind: SuggestionKind.IDEA, title: " Finish the rounding tests ", prompt: "Add the tests." }, ["5", "Finish the rounding tests", { type: "send", prompt: "Add the tests." }]],
  ])("%s", (_, s, want) => {
    expect(tiles({ suggestions: [s] })).toEqual([want]);
  });

  it.each([
    ["a conversation without its ID", { kind: SuggestionKind.CONTINUE, title: "x" }],
    ["no changes", { kind: SuggestionKind.CHANGES, count: 0 }],
    ["a worker without its name", { kind: SuggestionKind.WORKER_FAILED }],
    ["an idea without a prompt", { kind: SuggestionKind.IDEA, title: "x" }],
    ["an unknown kind", { kind: SuggestionKind.UNSPECIFIED, title: "x", prompt: "y" }],
  ])("%s is left out (the canned tiles instead)", (_, s) => {
    expect(tiles({ suggestions: [s] }).map(([k]) => k)).toEqual(canned);
  });

  it.each([
    ["harness_missing", { harnessMissing: true, suggestions: [{ kind: SuggestionKind.IDEA, title: "a", prompt: "b" }] }],
    ["a setup tile", { suggestions: [{ kind: SuggestionKind.IDEA, title: "a", prompt: "b" }, { kind: SuggestionKind.SETUP }] }],
  ])("the setup comes first, once, highlighted: %s", (_, init) => {
    const got = welcomeTiles(res(init));
    expect(got.map((x) => x.action.type)).toEqual(["setup", "send"]);
    expect(got[0].highlight).toBe(true);
    expect(got[0].detail).toContain(".agents/AGENT.md");
  });

  it("the setup alone comes with the canned tiles, at most four", () => {
    expect(welcomeTiles(res({ harnessMissing: true })).map((x) => x.key)).toEqual(["setup", "explain", "bug", "tests"]);
  });

  it("keeps the service's order and stops at four", () => {
    const ideas = Array.from({ length: 6 }, (_, i) => ({ kind: SuggestionKind.IDEA, title: `idea ${i}`, prompt: "p" }));
    const got = welcomeTiles(res({ suggestions: ideas }));
    expect(got).toHaveLength(maxTiles);
    expect(got.map((x) => x.text)).toEqual(["idea 0", "idea 1", "idea 2", "idea 3"]);
  });
});

describe("polling", () => {
  it.each([
    [undefined, 0, false],
    [{ pending: false }, 0, false],
    [{ pending: true }, 0, true],
    [{ pending: true }, pollAttempts - 1, true],
    [{ pending: true }, pollAttempts, false],
  ])("%j after %i attempts: %s", (init, attempts, want) => {
    expect(shouldPoll(init && res(init), attempts)).toBe(want);
  });
});
