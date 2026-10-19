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
import { ComposeSessionSchema, WorkflowFileSchema } from "../gen/blitz/v1/workspace_pb";
import { draftSteps, question } from "./compose";

describe("the composer", () => {
  it("knows the question it waits on, with its suggested answers", () => {
    const s = create(ComposeSessionSchema, { asking: true, turns: [{ role: "user", text: "a brief" }, { role: "composer", text: "Where are the notes?", options: ["notes.md"] }] });
    expect(question(s)).toEqual({ text: "Where are the notes?", options: ["notes.md"] });
    expect(question(create(ComposeSessionSchema, { asking: false, turns: s.turns }))).toBeUndefined();
    expect(question(undefined)).toBeUndefined();
  });

  it("lists a drafted workflow's steps in the order they run", () => {
    const wf = create(WorkflowFileSchema, {
      nodes: [
        { id: "save", type: "save_file", title: "Save it" },
        { id: "draft", type: "agent", title: "" },
        { id: "notes", type: "read_file", title: "Read the notes" },
        { id: "loose", type: "accumulate" },
      ],
      edges: [
        { from: "notes", to: "draft" },
        { from: "draft", to: "save" },
      ],
    });
    expect(draftSteps(wf).map((s) => s.title)).toEqual(["Read the notes", "loose", "draft", "Save it"]);
    expect(draftSteps(undefined)).toEqual([]);
  });
});
