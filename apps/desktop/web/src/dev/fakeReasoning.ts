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

import { create, type JsonObject, type MessageInitShape } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { GetReasoningResponseSchema, ReasoningNodeSchema, ReasoningTurnSchema, type GetReasoningResponse } from "../gen/blitz/v1/session_pb";
import { UsageSchema } from "../gen/blitz/v1/turn_pb";

/** A model call's usage: tokens in, out and reasoning, priced. */
const usage = (input: number, output: number, reasoning = 0, costUsd = 0) =>
  create(UsageSchema, { calls: 1, input: BigInt(input), output: BigInt(output), reasoning: BigInt(reasoning), costUsd, priced: true });

/** The fake's two turns: one that reasons, uses tools and asks a sub-agent; one whose provider keeps its reasoning. */
function turns(start: Date): { prompt: string; nodes: MessageInitShape<typeof ReasoningNodeSchema>[] }[] {
  const at = (s: number) => timestampFromDate(new Date(start.getTime() + s * 1000));
  const g = { provider: "google", model: "gemini-3-pro", author: "blitz" };
  return [
    {
      prompt: "Add retries to the uploader, with backoff, and make sure the tests still pass",
      nodes: [
        { id: "n0", kind: "turn", title: "Add retries to the uploader, with backoff, and make sure the tests still pass", text: "Add retries to the uploader, with backoff, and make sure the tests still pass", author: "user", status: "ok", start: at(0), durationMs: 41_200n, usage: create(UsageSchema, { calls: 6, input: 48_200n, output: 3_900n, reasoning: 1_640n, costUsd: 0.0814, priced: true }) },
        { id: "n1", parent: "n0", kind: "call", ...g, status: "ok", start: at(0), durationMs: 3_100n, usage: usage(9_288, 631, 410, 0.0121) },
        { id: "n2", parent: "n1", kind: "thought", title: "Finding where uploads fail", text: "**Finding where uploads fail**\nThe client calls `http.Do` once and returns the first error, so a dropped connection fails the whole upload.", status: "ok" },
        { id: "n3", parent: "n1", kind: "thought", title: "Choosing what to retry", text: "**Choosing what to retry**\nOnly idempotent requests and 5xx or timeout errors: a 4xx means the request itself is wrong.", status: "ok" },
        { id: "n4", parent: "n1", kind: "tool", tool: "read_file", title: "read_file", args: { path: "pkg/upload/client.go" } as JsonObject, result: { lines: 212 } as JsonObject, status: "ok", durationMs: 12n },
        { id: "n5", parent: "n1", kind: "tool", tool: "grep", title: "grep", args: { pattern: "Do(req", path: "pkg/upload" } as JsonObject, result: { matches: 3 } as JsonObject, status: "ok", durationMs: 40n },
        { id: "n6", parent: "n0", kind: "call", ...g, status: "ok", start: at(4), durationMs: 5_800n, usage: usage(11_020, 1_204, 520, 0.0188) },
        { id: "n7", parent: "n6", kind: "thought", title: "Writing the backoff", text: "**Writing the backoff**\nExponential, starting at 200 ms with jitter, capped at 5 s and 4 attempts, and stopping when the context is cancelled.", status: "ok" },
        { id: "n8", parent: "n6", kind: "tool", tool: "replace_in_file", title: "replace_in_file", args: { path: "pkg/upload/client.go", old: "resp, err := c.http.Do(req)", new: "resp, err := c.doWithRetry(ctx, req)" } as JsonObject, result: { replaced: 1 } as JsonObject, status: "ok", durationMs: 9n },
        { id: "n9", parent: "n6", kind: "tool", tool: "run_shell", title: "run_shell", args: { command: "go test ./pkg/upload/..." } as JsonObject, result: { error: "exit status 1", output: "--- FAIL: TestUploadTimeout (0.20s)\n    client_test.go:88: expected 1 attempt, got 4" } as JsonObject, status: "failed", durationMs: 6_400n },
        { id: "n10", parent: "n0", kind: "call", ...g, status: "ok", start: at(17), durationMs: 2_900n, usage: usage(12_400, 380, 260, 0.0141) },
        { id: "n11", parent: "n10", kind: "thought", title: "The test expects one attempt", text: "**The test expects one attempt**\nTestUploadTimeout sets a 100 ms deadline: the retry should give up once the context is done, which it does, but the test counts attempts. The test is out of date, not the code.", status: "ok" },
        { id: "n12", parent: "n10", kind: "tool", tool: "replace_in_file", title: "replace_in_file", args: { path: "pkg/upload/client_test.go" } as JsonObject, result: { replaced: 1 } as JsonObject, status: "ok", durationMs: 7n },
        { id: "n13", parent: "n10", kind: "tool", tool: "run_shell", title: "run_shell", args: { command: "go test ./pkg/upload/..." } as JsonObject, result: { output: "ok  \tpkg/upload\t1.42s" } as JsonObject, status: "ok", durationMs: 4_100n },
        { id: "n14", parent: "n0", kind: "call", ...g, status: "ok", start: at(24), durationMs: 1_800n, usage: usage(12_900, 210, 120, 0.0098) },
        { id: "n15", parent: "n14", kind: "thought", title: "Asking for a second look", text: "Retries change behaviour under load; a reviewer should check the backoff and the cancellation.", status: "ok" },
        { id: "n16", parent: "n14", kind: "agent", tool: "invoke_agent", title: "reviewer", author: "blitz", args: { agent_name: "reviewer", prompt: "Review the retry in pkg/upload/client.go" } as JsonObject, result: { response: "Looks right. One note: cap the total wait, not just each delay." } as JsonObject, status: "ok", durationMs: 9_600n, usage: create(UsageSchema, { calls: 2, input: 6_100n, output: 690n, reasoning: 330n, costUsd: 0.0094, priced: true }) },
        { id: "n17", parent: "n16", kind: "call", provider: "google", model: "gemini-3-flash", author: "reviewer", status: "ok", durationMs: 2_400n, usage: usage(3_000, 420, 330, 0.0051) },
        { id: "n18", parent: "n17", kind: "thought", title: "Checking the backoff", text: "Delays double from 200 ms with jitter, capped at 5 s; four attempts can wait up to about 8.6 s in all.", author: "reviewer", status: "ok" },
        { id: "n19", parent: "n17", kind: "tool", tool: "git_diff", title: "git_diff", author: "reviewer", args: { path: "pkg/upload" } as JsonObject, result: { files: 2 } as JsonObject, status: "ok", durationMs: 30n },
        { id: "n20", parent: "n16", kind: "call", provider: "google", model: "gemini-3-flash", author: "reviewer", status: "ok", durationMs: 1_900n, usage: usage(3_100, 270, 0, 0.0043) },
        { id: "n21", parent: "n20", kind: "text", title: "Looks right.", text: "Looks right. One note: cap the total wait, not just each delay.", author: "reviewer", status: "ok" },
        { id: "n22", parent: "n0", kind: "call", ...g, status: "ok", start: at(36), durationMs: 2_200n, usage: usage(13_600, 840, 0, 0.0172) },
        { id: "n23", parent: "n22", kind: "text", title: "The uploader now retries with backoff.", text: "The uploader now retries with backoff: up to 4 attempts on timeouts and 5xx answers, 200 ms doubling with jitter, capped at 5 s, and it stops as soon as the request is cancelled.\n\nI updated `TestUploadTimeout`, which counted attempts, and the tests pass. The reviewer suggests capping the total wait as well; say if you'd like that.", status: "ok" },
      ],
    },
    {
      prompt: "What's the total wait at most?",
      nodes: [
        { id: "n0", kind: "turn", title: "What's the total wait at most?", text: "What's the total wait at most?", author: "user", status: "ok", start: at(60), durationMs: 2_600n, usage: usage(14_100, 900, 512, 0.0215) },
        { id: "n1", parent: "n0", kind: "call", provider: "openai", model: "gpt-5.2", author: "blitz", status: "ok", hidden: true, durationMs: 2_600n, usage: usage(14_100, 900, 512, 0.0215) },
        { id: "n2", parent: "n1", kind: "text", title: "About 8.6 seconds", text: "About 8.6 seconds: 200 + 400 + 800 + 1,600 ms of delay, plus jitter, plus the attempts themselves.", status: "ok" },
      ],
    },
  ];
}

/** GetReasoning for the fake: its two turns, once the chat has a prompt. */
export function fakeReasoning(prompts: number, turn?: number): GetReasoningResponse {
  if (prompts === 0) return create(GetReasoningResponseSchema, { turn: -1 });
  const all = turns(new Date(Date.now() - 5 * 60_000));
  const shown = turn === undefined || turn >= all.length ? all.length - 1 : Math.max(0, turn);
  return create(GetReasoningResponseSchema, {
    turn: shown,
    turns: all.map((x, i) => {
      const root = x.nodes[0];
      return create(ReasoningTurnSchema, { index: i, prompt: x.prompt, start: root.start, calls: root.usage?.calls ?? 0, thoughts: x.nodes.filter((n) => n.kind === "thought").length, usage: root.usage });
    }),
    nodes: all[shown].nodes.map((n) => create(ReasoningNodeSchema, n)),
  });
}
