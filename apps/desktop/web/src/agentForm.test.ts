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
import { addTools, advancedCount, agentFromForm, agentPath, deleteTarget, emptyAgentForm, fileName, formFromAgent, invalidFields, sameForm, toolChoices, validAgentName, type AgentForm } from "./agentForm";
import { AgentDefinitionSchema } from "./gen/blitz/v1/workspace_pb";

const valid: AgentForm = { ...emptyAgentForm, name: "reviewer", description: "Reviews changes" };

describe("the agent form", () => {
  it.each([
    ["reviewer", true],
    ["code-review_2", true],
    ["9lives", true],
    ["", false],
    ["Reviewer", false],
    ["-lead", false],
    ["two words", false],
    ["a/b", false],
  ])("checks the name %j", (name, ok) => {
    expect(validAgentName(name)).toBe(ok);
  });

  it.each<[string, Partial<AgentForm>, string[]]>([
    ["a complete form", {}, []],
    ["no name", { name: "" }, ["name"]],
    ["no description", { description: "  " }, ["description"]],
    ["unset numbers", { temperature: "", topP: " ", maxTokens: "", thinkingBudget: "", maxTurns: "" }, []],
    ["numbers in range", { temperature: "2", topP: "1", maxTokens: "1", thinkingBudget: "0", maxTurns: "0" }, []],
    ["a temperature over 2", { temperature: "2.1" }, ["temperature"]],
    ["a negative temperature", { temperature: "-0.1" }, ["temperature"]],
    ["a top_p of 0", { topP: "0" }, ["topP"]],
    ["a top_p over 1", { topP: "1.5" }, ["topP"]],
    ["no max tokens", { maxTokens: "0" }, ["maxTokens"]],
    ["fractional max tokens", { maxTokens: "10.5" }, ["maxTokens"]],
    ["a negative thinking budget", { thinkingBudget: "-1" }, ["thinkingBudget"]],
    ["negative max turns", { maxTurns: "-3" }, ["maxTurns"]],
    ["text for a number", { temperature: "warm" }, ["temperature"]],
    ["several", { name: "X", temperature: "3" }, ["name", "temperature"]],
    ["a model with a space", { defaultModel: "gemini 3" }, ["defaultModel"]],
    ["a provider and no model", { defaultModel: "gemini/" }, ["defaultModel"]],
    ["a model with capitals (lowered when saved)", { defaultModel: "Gemini-3.8-flash" }, []],
  ])("finds what's wrong with %s", (_, change, want) => {
    expect(invalidFields({ ...valid, ...change })).toEqual(want);
  });

  it("makes a definition, leaving empty numbers unset", () => {
    const a = agentFromForm({ ...valid, name: " reviewer ", tools: ["read_file", " grep ", "read_file", ""], temperature: "0.3", maxTurns: "12" });
    expect(a.name).toBe("reviewer");
    expect(a.tools).toEqual(["read_file", "grep"]);
    expect(a.temperature).toBe(0.3);
    expect(a.maxTurns).toBe(12);
    expect(a.topP).toBeUndefined();
    expect(a.maxTokens).toBeUndefined();
    expect(a.thinkingBudget).toBeUndefined();
  });

  it("shows a definition and makes the same one back", () => {
    const def = create(AgentDefinitionSchema, {
      name: "qa2",
      displayName: "QA 2",
      description: "Tests",
      tools: ["grep"],
      defaultModel: "anthropic/claude-sonnet-5-5",
      agencyLevel: "medium",
      permissionMode: "plan",
      maxTurns: 20,
      background: true,
      isolation: "worktree",
      temperature: 0,
      topP: 0.9,
      maxTokens: 4096,
      effort: "high",
      thinkingBudget: 0,
      prompt: "You test.",
    });
    const f = formFromAgent(def);
    expect(f.temperature).toBe("0");
    expect(f.thinkingBudget).toBe("0");
    expect(advancedCount(f)).toBe(11);
    expect(agentFromForm(f)).toEqual(def);
    expect(sameForm(f, formFromAgent(def))).toBe(true);
    expect(sameForm(f, { ...f, prompt: "Other." })).toBe(false);
  });

  it.each([
    ["the model", { defaultModel: "gemini/gemini-3.8-flash" }, 1],
    ["agency and mode", { agencyLevel: "low", permissionMode: "plan" }, 2],
    ["max turns", { maxTurns: "5" }, 1],
    ["isolation", { isolation: "worktree" as const }, 1],
    ["tools, however many", { tools: ["grep", "read_file"] }, 1],
    ["a model setting", { temperature: "0.2" }, 1],
    ["blank text", { maxTurns: "  " }, 0],
    ["not the file name, display name or prompt", { name: "x", displayName: "X", prompt: "Hi.", background: true }, 0],
  ])("counts %s in Advanced", (_, change, want) => {
    expect(advancedCount({ ...emptyAgentForm, ...change })).toBe(want);
  });

  it("counts no advanced settings on a blank form", () => {
    expect(advancedCount(emptyAgentForm)).toBe(0);
    expect(formFromAgent(create(AgentDefinitionSchema, {})).maxTurns).toBe("");
  });

  it("offers the tools offered and chosen, once each, sorted", () => {
    expect(toolChoices(["read_file", "grep"], ["grep", "my_mcp_tool"])).toEqual(["grep", "my_mcp_tool", "read_file"]);
  });

  it.each([
    ["one", [], "grep", ["grep"]],
    ["several, by commas and spaces", ["grep"], "read_file, glob  edit", ["grep", "read_file", "glob", "edit"]],
    ["one already chosen", ["grep"], " grep ", ["grep"]],
    ["nothing", ["grep"], " , ", ["grep"]],
  ])("adds %s typed", (_, chosen, typed, want) => {
    expect(addTools(chosen, typed)).toEqual(want);
  });

  it.each([
    ["/w/.agents/agents", "reviewer", "/w/.agents/agents/reviewer.md"],
    ["/w/.agents/agents/", " reviewer ", "/w/.agents/agents/reviewer.md"],
    ["/w/.agents/agents", "", "/w/.agents/agents/<name>.md"],
    ["", "reviewer", ""],
  ])("saves in %j as %j", (dir, name, want) => {
    expect(agentPath(dir, name)).toBe(want);
  });

  it("names a file by its agent, or by itself when it doesn't load", () => {
    expect(fileName("/a/b/review.md", "reviewer")).toBe("reviewer");
    expect(fileName("/a/b/half-done.md")).toBe("half-done.md");
  });

  it.each([
    ["an agent by its name", "/a/reviewer.md", "reviewer", { name: "reviewer" }],
    ["a file that doesn't load by its path", "/a/half-done.md", "", { path: "/a/half-done.md" }],
  ])("deletes %s", (_, path, name, want) => {
    expect(deleteTarget(path, name)).toEqual(want);
  });
});
