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
import { describe, expect, it } from "vitest";
import { TurnEventSchema, type TurnEvent } from "./gen/blitz/v1/turn_pb";
import { MessageSchema } from "./gen/blitz/v1/session_pb";
import { applyEvent, assignPromptIndices, failed, fromMessages, parseDiff, summarizeArgs, tasksOf, type Entry } from "./turns";

const text = (t: string, opts: { partial?: boolean; repeat?: boolean; thought?: boolean } = {}): TurnEvent =>
  create(TurnEventSchema, { author: "blitz", kind: { case: "text", value: { text: t, ...opts } } });
const call = (id: string, name: string, args = {}, partial = false): TurnEvent =>
  create(TurnEventSchema, { kind: { case: "toolCall", value: { id, name, args, partial } } });
const result = (id: string, name: string, res = {}): TurnEvent =>
  create(TurnEventSchema, { kind: { case: "toolResult", value: { id, name, result: res } } });
const finished = (message = ""): TurnEvent =>
  create(TurnEventSchema, {
    kind: { case: "finished", value: message ? { error: { reason: "RUN_FAILED", message } } : {} },
  });

const run = (events: TurnEvent[]) => events.reduce<Entry[]>(applyEvent, []);

describe("applyEvent", () => {
  it("shows streamed text once", () => {
    const got = run([text("Hel", { partial: true }), text("lo", { partial: true }), text("Hello", { repeat: true }), text(" again")]);
    expect(got).toEqual([
      { kind: "model", text: "Hello", author: "blitz", open: false },
      { kind: "model", text: " again", author: "blitz", open: false },
    ]);
  });

  it("keeps thoughts apart and leaves out streamed copies of tool calls", () => {
    const got = run([text("hmm", { thought: true }), call("1", "read_file", { path: "a" }, true), call("1", "read_file", { path: "a" })]);
    expect(got).toEqual([
      { kind: "thought", text: "hmm", open: false },
      { kind: "tool", id: "1", name: "read_file", args: { path: "a" } },
    ]);
  });

  it("matches results to their calls", () => {
    const got = run([call("1", "read_file"), call("2", "list_files"), result("1", "read_file", { content: "x" })]);
    expect(got[0]).toMatchObject({ id: "1", result: { content: "x" } });
    expect(got[1]).not.toHaveProperty("result");
  });

  it("ends a turn by closing open text and noting an error", () => {
    const got = run([text("partial", { partial: true }), finished("the run reached its cost limit")]);
    expect(got).toEqual([
      { kind: "model", text: "partial", author: "blitz", open: false },
      { kind: "notice", text: "the run reached its cost limit", tone: "error" },
    ]);
  });
});

describe("summaries", () => {
  it("summarizes arguments and spots failures", () => {
    expect(summarizeArgs({ path: "src/main.go" })).toBe("src/main.go");
    expect(summarizeArgs({ command: "x".repeat(100) })).toHaveLength(80);
    expect(failed({ error: "nope" })).toBe(true);
    expect(failed({ success: true })).toBe(false);
  });
});

const msg = (role: string, text: string, kind = "") => create(MessageSchema, { role, text, kind });

describe("thoughts and kinds", () => {
  it("collects streamed thinking once, apart from the answer", () => {
    const got = run([text("Let me ", { thought: true, partial: true }), text("look.", { thought: true, partial: true }), text("Let me look.", { thought: true }), text("Done")]);
    expect(got).toEqual([
      { kind: "thought", text: "Let me look.", open: false },
      { kind: "model", text: "Done", author: "blitz", open: false },
    ]);
  });

  it("adds a separator between runs to the answer, not as an entry", () => {
    const got = run([text("Plan approved."), text("\n\n"), text("done")]);
    expect(got.map((e) => ("text" in e ? e.text : ""))).toEqual(["Plan approved.\n\n", "done"]);
  });

  it("tells prompts from other user messages and numbers them", () => {
    const saved = [msg("user", "fix it"), msg("model", "ok"), msg("user", "also this", "steer"), msg("user", "(plan approved) go", "plan"), msg("user", "next")];
    const entries = fromMessages(saved);
    expect(entries.map((e) => (e.kind === "user" ? `${e.sub ?? "prompt"}:${e.index ?? "-"}` : e.kind))).toEqual(["prompt:0", "model", "steer:-", "plan:-", "prompt:4"]);
  });

  it("learns the index of prompts sent in this window", () => {
    const shown = [...fromMessages([msg("user", "a"), msg("model", "b")]), { kind: "user" as const, text: "c" }];
    const after = assignPromptIndices(shown, [msg("user", "a"), msg("model", "b"), msg("user", "c"), msg("model", "d")]);
    expect(after[2]).toMatchObject({ kind: "user", index: 2 });
    expect(assignPromptIndices(after, [msg("user", "a"), msg("model", "b"), msg("user", "c")])).toBe(after); // unchanged: same array
  });

  it("reads task lists from events", () => {
    const ev = create(TurnEventSchema, { kind: { case: "tasks", value: { items: [{ content: "x", status: "done" }] } } });
    expect(tasksOf(ev)?.[0].content).toBe("x");
    expect(tasksOf(text("hi"))).toBeUndefined();
  });
});

describe("parseDiff", () => {
  it("splits files and counts changes", () => {
    const d = "--- a/x.go\n+++ b/x.go\n@@ -1,2 +1,2 @@\n ctx\n-old\n+new\n+more\n--- /dev/null\n+++ b/new.txt\n@@ -0,0 +1 @@\n+hi\n# big.bin: too large to diff\n";
    const files = parseDiff(d);
    expect(files.map((f) => `${f.path} +${f.added} -${f.removed}`)).toEqual(["x.go +2 -1", "new.txt +1 -0", "big.bin +0 -0"]);
    expect(files[0].lines.map((l) => l.kind)).toEqual(["hunk", "ctx", "del", "add", "add"]);
    expect(files[2].lines[0]).toEqual({ kind: "meta", text: "big.bin: too large to diff" });
    const deleted = parseDiff("--- a/gone.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-bye\n");
    expect(deleted[0].path).toBe("gone.txt");
  });
});
