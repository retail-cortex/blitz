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
import { RuleFileSchema } from "../gen/blitz/v1/workspace_pb";
import { emptyRuleForm, formFromRule, moved, nextOrder, ruleSections, sameRuleForm, specFromForm, validRuleName } from "./ruleForm";

const rule = (o: Partial<{ name: string; when: string; action: string; order: number; paths: string[]; agents: string[] }>) => create(RuleFileSchema, { name: "r", text: "x", ...o });

describe("the rule form", () => {
  it("reads a gate and writes back only a gate's fields", () => {
    const f = formFromRule(rule({ name: "tested", when: "after_agent", action: "revise", order: 20, agents: ["qa"], paths: ["*.go"] }));
    expect(f).toMatchObject({ name: "tested", when: "after_agent", action: "revise", order: 20, agents: ["qa"] });
    expect(specFromForm(f)).toEqual({ name: "tested", description: "", when: "after_agent", action: "revise", order: 20, agents: ["qa"], paths: [], text: "x" });
  });

  it("reads an instruction rule, its paths typed comma-separated", () => {
    const f = formFromRule(rule({ name: "style", paths: ["*.go", "cmd/**"], action: "" }));
    expect(f.when).toBe("");
    expect(f.paths).toBe("*.go, cmd/**");
    expect(specFromForm({ ...f, action: "block", order: 5, agents: ["x"] })).toEqual({ name: "style", description: "", when: "", action: "", order: 0, agents: [], paths: ["*.go", "cmd/**"], text: "x" });
  });

  it("tells unsaved changes, names, lists, moves and the next order", () => {
    expect(sameRuleForm(emptyRuleForm, { ...emptyRuleForm, name: " " })).toBe(true);
    expect(sameRuleForm(emptyRuleForm, { ...emptyRuleForm, action: "block" })).toBe(false);
    expect(validRuleName("no-prod")).toBe(true);
    expect(validRuleName("no_prod2")).toBe(true);
    expect(validRuleName("No prod")).toBe(false);
    const rules = [rule({ name: "a", when: "before_agent", order: 10 }), rule({ name: "b", when: "after_agent", order: 30 }), rule({ name: "c" }), rule({ name: "d", when: "before_agent", order: 20 })];
    const s = ruleSections(rules);
    expect(s.before.map((r) => r.name)).toEqual(["a", "d"]);
    expect(s.after.map((r) => r.name)).toEqual(["b"]);
    expect(s.instructions.map((r) => r.name)).toEqual(["c"]);
    expect(moved(["a", "b", "c"], 2, 0)).toEqual(["c", "a", "b"]);
    expect(moved(["a", "b", "c"], 0, 5)).toEqual(["a", "b", "c"]);
    expect(nextOrder(s.before)).toBe(30);
    expect(nextOrder([])).toBe(10);
  });
});
