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
import { addLink, emptyWorkflowForm, linkId, linkText, setLink, formFromWorkflow, fromTemplate, newStep, removeStep, sameWorkflowForm, specFromForm, stepNeeds, templates } from "./workflowForm";

describe("the workflow canvas", () => {
  it("reads a workflow and writes back each step's own settings", () => {
    const w = create(WorkflowFileSchema, {
      name: "brief",
      nodes: [
        { id: "a", type: "agent", x: 10.4, y: 20, agent: "writer", instructions: "Write.", rules: ["short"], path: "ignored" },
        { id: "s", type: "script", skill: "pdf", script: "render", args: ["--a4", "-q"] },
        { id: "z", type: "weird" },
      ],
      edges: [{ from: "a", to: "s", when: "output.data.n > 1", label: "big" }, { from: "a", to: "gone" }],
    });
    const f = formFromWorkflow(w);
    expect(f.steps[1].args).toBe("--a4 -q");
    expect(f.steps[2].type).toBe("agent");
    const spec = specFromForm(f);
    expect(spec.nodes[0]).toMatchObject({ x: 10, agent: "writer", rules: ["short"], path: "" });
    expect(spec.nodes[1]).toMatchObject({ skill: "pdf", script: "render", args: ["--a4", "-q"], instructions: "" });
    expect(spec.edges).toEqual([{ from: "a", to: "s", when: "output.data.n > 1", if: "", default: false, error: false, label: "big" }]);
    expect(f.links[0]).toMatchObject({ kind: "when", condition: "output.data.n > 1" });
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
    expect(f.links[0].kind).toBe("always");
    const g = setLink(f, linkId(f.links[0]), { kind: "error" });
    expect(specFromForm(g).edges[0]).toMatchObject({ error: true, when: "" });
    expect(linkText(g.links[0], (k) => `<${k}>`)).toBe("<error>");
    expect(linkText({ ...g.links[0], kind: "if", condition: "The customer sounds angry about the refund" }, (k) => k)).toBe("The customer sounds angry a…");
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
    expect(stepNeeds(newStep("extract", [], 0, 0))).toBe("fields");
    const x = { ...newStep("extract", [], 0, 0), fields: [{ name: "total", type: "number" as const, description: "" }, { name: " ", type: "string" as const, description: "" }], retryAttempts: "3", retryDelay: "2s" };
    expect(specFromForm({ ...emptyWorkflowForm, steps: [x] }).nodes[0]).toMatchObject({ fields: [{ name: "total", type: "number", description: "" }], retryAttempts: 3, retryDelay: "2s" });
    expect(specFromForm({ ...emptyWorkflowForm, steps: [{ ...newStep("exclusive", [], 0, 0), retryAttempts: "3", timeout: "1m" }] }).nodes[0]).toMatchObject({ retryAttempts: 0, timeout: "" });
  });

  it("starts from templates", () => {
    for (const tpl of templates) {
      const f = fromTemplate(tpl, (k) => k);
      for (const l of f.links) expect(f.steps.map((s) => s.id)).toEqual(expect.arrayContaining([l.from, l.to]));
    }
    expect(fromTemplate("brief", (k) => k).steps.map((s) => s.type)).toEqual(["read_file", "browser", "accumulate", "agent", "save_file"]);
  });
});
