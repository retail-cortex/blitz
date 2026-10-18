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
import { emptyGuardrailForm, formForBuiltin, formFromGuardrail, sameGuardrailForm, specFromForm, stageHasTools } from "./guardrailForm";

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
    });
  });

  it("keeps each built-in's own settings only", () => {
    expect(specFromForm({ ...formForBuiltin("secrets"), pii: true, judge: true, tools: "x" })).toMatchObject({ builtin: "secrets", action: "redact", pii: true, judge: false, tools: [] });
    expect(specFromForm({ ...formForBuiltin("prompt_injection"), judge: true, pii: true, tools: "read_file, web_fetch" })).toMatchObject({ action: "warn", judge: true, pii: false, tools: ["read_file", "web_fetch"] });
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
});
