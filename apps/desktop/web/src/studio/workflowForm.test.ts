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
import { WorkflowFileSchema } from "../gen/blitz/v1/workspace_pb";
import { addLink, emptyWorkflowForm, formFromWorkflow, fromTemplate, newStep, removeStep, sameWorkflowForm, specFromForm, stepNeeds, templates } from "./workflowForm";

describe("the workflow canvas", () => {
  it("reads a workflow and writes back each step's own settings", () => {
    const w = create(WorkflowFileSchema, {
      name: "brief",
      nodes: [
        { id: "a", type: "agent", x: 10.4, y: 20, agent: "writer", instructions: "Write.", rules: ["short"], path: "ignored" },
        { id: "s", type: "script", skill: "pdf", script: "render", args: ["--a4", "-q"] },
        { id: "z", type: "weird" },
      ],
      edges: [{ from: "a", to: "s" }, { from: "a", to: "gone" }],
    });
    const f = formFromWorkflow(w);
    expect(f.steps[1].args).toBe("--a4 -q");
    expect(f.steps[2].type).toBe("agent");
    const spec = specFromForm(f);
    expect(spec.nodes[0]).toMatchObject({ x: 10, agent: "writer", rules: ["short"], path: "" });
    expect(spec.nodes[1]).toMatchObject({ skill: "pdf", script: "render", args: ["--a4", "-q"], instructions: "" });
    expect(spec.edges).toEqual([{ from: "a", to: "s" }]);
    expect(sameWorkflowForm(f, formFromWorkflow(w))).toBe(true);
  });

  it("adds steps and links, never a loop", () => {
    const a = newStep("agent", [], 0, 0);
    const b = newStep("agent", [a], 100, 0);
    expect([a.id, b.id]).toEqual(["agent-1", "agent-2"]);
    expect(newStep("read_file", [], 0, 0).id).toBe("read-file-1");
    let f = { ...emptyWorkflowForm, steps: [a, b] };
    f = addLink(f, a.id, b.id);
    expect(f.links).toHaveLength(1);
    expect(addLink(f, a.id, b.id)).toBe(f);
    expect(addLink(f, b.id, a.id)).toBe(f);
    expect(addLink(f, a.id, a.id)).toBe(f);
    expect(removeStep(f, a.id)).toMatchObject({ steps: [b], links: [] });
  });

  it("says what a step needs", () => {
    expect(stepNeeds(newStep("agent", [], 0, 0))).toBe("instructions");
    expect(stepNeeds(newStep("script", [], 0, 0))).toBe("script");
    expect(stepNeeds(newStep("save_file", [], 0, 0))).toBe("path");
    expect(stepNeeds(newStep("email", [], 0, 0))).toBe("coming_soon");
    expect(stepNeeds(newStep("accumulate", [], 0, 0))).toBe("");
  });

  it("starts from templates", () => {
    for (const tpl of templates) {
      const f = fromTemplate(tpl, (k) => k);
      for (const l of f.links) expect(f.steps.map((s) => s.id)).toEqual(expect.arrayContaining([l.from, l.to]));
    }
    expect(fromTemplate("brief", (k) => k).steps.map((s) => s.type)).toEqual(["read_file", "browser", "accumulate", "agent", "save_file"]);
  });
});
