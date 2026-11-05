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
import { GuardrailFileSchema } from "../gen/blitz/v1/workspace_pb";
import { emptyGuardrailForm, formForBuiltin, formFromGuardrail, sameGuardrailForm, specFromForm, stageHasTools, defaultsEntry, defaultLimits, limitsProblems, noLimit } from "./guardrailForm";

// The built-ins as ListGuardrails sends them (gates.Builtins).
const builtins = {
  prompt_injection: { id: "prompt_injection", stage: "tool_result", defaultAction: "redact" },
  secrets: { id: "secrets", stage: "output", defaultAction: "redact" },
  limits: { id: "limits", stage: "action", defaultAction: "block" },
};

describe("the guardrail form", () => {
  it("reads the limits guardrail and writes its limits back", () => {
    const g = create(GuardrailFileSchema, { name: "limits", builtin: "limits", stage: "action", action: "block", limits: { maxTurns: 20, maxCostUsd: 2.5, allowedDomains: ["example.com", "docs.dev"], commands: ["git push --force*", "rm -rf /*"] } });
    const f = formFromGuardrail(g);
    expect(f).toMatchObject({ builtin: "limits", maxTurns: "20", maxCostUsd: "2.5", maxFilesChanged: "", domains: "example.com, docs.dev", commands: "git push --force*\nrm -rf /*" });
    expect(specFromForm({ ...f, maxFilesChanged: "3", text: "ignored" })).toEqual({
      name: "limits",
      builtin: "limits",
      stage: "",
      action: "block",
      agents: [],
      tools: [],
      description: "",
      pii: false,
      judge: false,
      limits: { maxCostUsd: 2.5, maxTurns: 20, maxFilesChanged: 3, allowedDomains: ["example.com", "docs.dev"], commands: ["git push --force*", "rm -rf /*"] },
      text: "",
      off: false,
    });
  });

  it("keeps a built-in's settings when it's switched off", () => {
    const f = formFromGuardrail(create(GuardrailFileSchema, { name: "secrets", builtin: "secrets", stage: "output", action: "warn", pii: true, off: true }));
    expect(f).toMatchObject({ off: true, action: "warn", pii: true });
    expect(specFromForm({ ...f, off: false })).toMatchObject({ off: false, action: "warn", pii: true });
    expect(formForBuiltin(builtins.limits).off).toBe(false);
    expect(specFromForm(formForBuiltin(builtins.limits)).limits).toMatchObject({ commands: [defaultsEntry], maxTurns: Number(defaultLimits.maxTurns), maxCostUsd: Number(defaultLimits.maxCostUsd) });
    expect(formForBuiltin(builtins.prompt_injection)).toMatchObject({ action: "redact", judge: true });
  });

  it("takes a built-in's stage and default action from the service", () => {
    expect(formForBuiltin({ id: "secrets", stage: "output", defaultAction: "warn" })).toMatchObject({ builtin: "secrets", stage: "output", action: "warn" });
  });

  it("keeps each built-in's own settings only", () => {
    expect(specFromForm({ ...formForBuiltin(builtins.secrets), pii: true, judge: true, tools: "x" })).toMatchObject({ builtin: "secrets", action: "redact", pii: true, judge: false, tools: [] });
    expect(specFromForm({ ...formForBuiltin(builtins.prompt_injection), judge: true, pii: true, tools: "read_file, web_fetch" })).toMatchObject({ action: "redact", judge: true, pii: false, tools: ["read_file", "web_fetch"] });
  });

  it("writes a custom guardrail, tools only for stages that check them", () => {
    const f = { ...emptyGuardrailForm, name: " careful ", stage: "output" as const, tools: "a", text: "No dates." };
    expect(specFromForm(f)).toMatchObject({ name: "careful", stage: "output", tools: [], text: "No dates.", limits: undefined });
    expect(specFromForm({ ...f, stage: "action" }).tools).toEqual(["a"]);
    expect(stageHasTools("tool_result")).toBe(true);
    expect(stageHasTools("input")).toBe(false);
    expect(sameGuardrailForm(f, { ...f, name: "careful" })).toBe(true);
    expect(sameGuardrailForm(f, { ...f, action: "warn" })).toBe(false);
  });

  describe("the limits' ceilings", () => {
    const limits = formForBuiltin(builtins.limits);
    it.each([
      { name: "empty is the default, which the file then follows", cost: "", turns: " ", want: { maxCostUsd: 10, maxTurns: 200 } },
      { name: "0 is no limit", cost: "0", turns: "0", want: { maxCostUsd: 0, maxTurns: 0 } },
      { name: "a number is kept", cost: "2.5", turns: "20", want: { maxCostUsd: 2.5, maxTurns: 20 } },
    ])("$name", ({ cost, turns, want }) => {
      const f = { ...limits, maxCostUsd: cost, maxTurns: turns };
      expect(limitsProblems(f)).toEqual({});
      expect(specFromForm(f).limits).toMatchObject(want);
    });

    it.each([
      { name: "text", field: "maxTurns" as const, value: "lots" },
      { name: "a negative number", field: "maxCostUsd" as const, value: "-1" },
      { name: "a fraction of a call", field: "maxTurns" as const, value: "2.5" },
      { name: "a fraction of a file", field: "maxFilesChanged" as const, value: "1.5" },
    ])("refuses $name", ({ field, value }) => {
      expect(limitsProblems({ ...limits, [field]: value })).toEqual({ [field]: true });
    });

    it("reads a file's 0 as no limit, not as empty", () => {
      const f = formFromGuardrail(create(GuardrailFileSchema, { name: "limits", builtin: "limits", limits: { maxTurns: 0, maxCostUsd: 0 } }));
      expect(f).toMatchObject({ maxTurns: "0", maxCostUsd: "0" });
      expect(noLimit(f.maxTurns)).toBe(true);
      expect(noLimit("")).toBe(false);
      expect(noLimit("200")).toBe(false);
      expect(limitsProblems({ ...emptyGuardrailForm, maxTurns: "lots" })).toEqual({});
    });
  });
});
