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

import { describe, expect, it } from "vitest";
import {
  buildStagePrompt,
  canDependOn,
  countSpecs,
  getStageDraft,
  hasUnsavedStageDraft,
  layoutSpecStages,
  parseSpecStageDraft,
  resetSpecDraftStore,
  resolveQuestionsDraftRequest,
  resolveQuestionsPrompt,
  reviewStagePrompt,
  setStageDraft,
  specColumnGap,
  specRowGap,
  stageProgress,
  toggleSpecDependency,
  type SpecStageInput,
} from "./specsGraph";

function makeStage(overrides: Partial<SpecStageInput> & { name: string }): SpecStageInput {
  return {
    path: `.agents/specs/${overrides.name}.md`,
    number: overrides.name.slice(0, 3),
    title: overrides.name.slice(4),
    status: "draft",
    kind: "",
    dependsOn: [],
    context: "",
    goals: [],
    nonGoals: [],
    items: [],
    doneWhen: [],
    openQuestions: [],
    skills: [],
    agents: [],
    ...overrides,
  };
}

describe("Studio › Specs graph helpers", () => {
  it("positions stages in topological columns and builds dependency edges", () => {
    const stages: SpecStageInput[] = [
      makeStage({ name: "001-overview", status: "done", kind: "software" }),
      makeStage({ name: "002-data", status: "in-progress", dependsOn: ["001-overview"] }),
      makeStage({ name: "003-auth", status: "draft", dependsOn: ["001-overview"] }),
      makeStage({ name: "004-api", status: "draft", dependsOn: ["002-data", "003-auth"] }),
    ];
    const { nodes, edges } = layoutSpecStages(stages);
    expect(nodes).toHaveLength(4);
    expect(nodes[0]).toMatchObject({ column: 0, row: 0, x: 40, y: 40 });
    expect(nodes[1]).toMatchObject({ column: 1, row: 0, x: 40 + specColumnGap, y: 40 });
    expect(nodes[2]).toMatchObject({ column: 1, row: 1, x: 40 + specColumnGap, y: 40 + specRowGap });
    expect(nodes[3]).toMatchObject({ column: 2, row: 0, x: 40 + 2 * specColumnGap, y: 40 });
    expect(edges).toEqual([
      { id: "001-overview->002-data", source: "001-overview", target: "002-data" },
      { id: "001-overview->003-auth", source: "001-overview", target: "003-auth" },
      { id: "002-data->004-api", source: "002-data", target: "004-api" },
      { id: "003-auth->004-api", source: "003-auth", target: "004-api" },
    ]);
  });

  it("handles cyclic or unknown dependencies in layout without looping", () => {
    const stages: SpecStageInput[] = [
      makeStage({ name: "001-a", dependsOn: ["002-b", "999-missing"] }),
      makeStage({ name: "002-b", dependsOn: ["001-a"] }),
    ];
    const { nodes, edges } = layoutSpecStages(stages);
    expect(nodes).toHaveLength(2);
    expect(edges).toHaveLength(2);
  });

  it("rejects self-dependencies, unknown stages, and dependency loops", () => {
    const stages: SpecStageInput[] = [
      makeStage({ name: "001-overview" }),
      makeStage({ name: "002-data", dependsOn: ["001-overview"] }),
      makeStage({ name: "003-api", dependsOn: ["002-data"] }),
    ];
    expect(canDependOn(stages, "003-api", "001-overview")).toBe(true);
    expect(canDependOn(stages, "001-overview", "001-overview")).toBe(false);
    expect(canDependOn(stages, "", "001-overview")).toBe(false);
    expect(canDependOn(stages, "999-none", "001-overview")).toBe(false);
    expect(canDependOn(stages, "001-overview", "003-api")).toBe(false);
  });

  it("toggles dependencies and counts stages, items and open questions", () => {
    expect(toggleSpecDependency(["002-data"], "001-overview")).toEqual(["001-overview", "002-data"]);
    expect(toggleSpecDependency(["001-overview", "002-data"], "001-overview")).toEqual(["002-data"]);

    const stages: SpecStageInput[] = [
      makeStage({
        name: "001-overview",
        status: "done",
        items: [{ id: "OVR-01", section: "requirements", text: "Fast" }],
      }),
      makeStage({
        name: "002-api",
        status: "in-progress",
        items: [
          { id: "API-01", section: "requirements", text: "List" },
          { id: "API-02", section: "requirements", text: "Create" },
        ],
        openQuestions: ["Rate limit?"],
      }),
    ];
    expect(countSpecs(stages)).toEqual({
      stages: 2,
      done: 1,
      inProgress: 1,
      items: 3,
      openQuestions: 1,
    });
  });

  it("formats build, resolve and review prompts", () => {
    const stage = makeStage({
      name: "002-api",
      number: "002",
      title: "HTTP API",
      items: [
        { id: "API-01", section: "requirements", text: "List" },
        { id: "API-02", section: "requirements", text: "Create" },
      ],
    });
    expect(buildStagePrompt(stage, [])).toBe(
      "Work on stage 002 · HTTP API (@.agents/specs/002-api.md) and satisfy its requirements and done-when criteria.",
    );
    expect(buildStagePrompt(stage, ["API-02"])).toBe(
      "Implement API-02 from @.agents/specs/002-api.md (002 · HTTP API) and verify its done-when criteria.",
    );
    const untitled = makeStage({ name: "custom", number: "", title: "Custom" });
    expect(buildStagePrompt(untitled, [])).toContain("stage Custom");
    expect(resolveQuestionsPrompt(stage)).toBe("/spec Resolve the open questions in .agents/specs/002-api.md");
    expect(reviewStagePrompt(stage)).toBe("/review Check the workspace against @.agents/specs/002-api.md (002 · HTTP API).");
    expect(reviewStagePrompt(untitled)).toBe("/review Check the workspace against @.agents/specs/custom.md (Custom).");
  });

  it("computes checklist progress and formats open-question resolution requests", () => {
    const stage = makeStage({
      name: "002-api",
      status: "in-progress",
      items: [
        { id: "API-01", section: "requirements", text: "List", done: true },
        { id: "API-02", section: "requirements", text: "Create", done: false },
      ],
      doneWhen: ["Unit tests pass", "Contract tests pass"],
      doneWhenDone: [true, false],
      openQuestions: ["Rate limit per IP or account?", "Cookies or bearer tokens?"],
    });
    expect(stageProgress(stage)).toEqual({
      itemsDone: 1,
      itemsTotal: 2,
      doneWhenDone: 1,
      doneWhenTotal: 2,
      totalChecks: 4,
      doneChecks: 2,
      percent: 50,
      remainingItemIds: ["API-02"],
    });
    expect(stageProgress(makeStage({ name: "001-done", status: "done" })).percent).toBe(100);
    expect(stageProgress(makeStage({ name: "001-draft", status: "draft" })).percent).toBe(0);

    expect(resolveQuestionsDraftRequest(stage, {})).toBe("");
    expect(resolveQuestionsDraftRequest(stage, { 0: "   " })).toBe("");
    expect(resolveQuestionsDraftRequest(stage, { 0: "100 req/min per IP" })).toContain("Decision: 100 req/min per IP");
    expect(resolveQuestionsPrompt(stage, { 0: "100 req/min per IP" })).toBe(
      "/spec Resolve the open questions in .agents/specs/002-api.md with these decisions: Rate limit per IP or account? -> 100 req/min per IP",
    );
  });

  it("parses unsaved stage Markdown drafts and persists per-stage workbench state across navigations", () => {
    resetSpecDraftStore();
    const base = makeStage({
      name: "002-api",
      number: "002",
      title: "HTTP API",
      status: "approved",
      context: "Original context.",
      items: [{ id: "API-01", section: "requirements", text: "List", done: true }],
      doneWhen: ["Unit tests pass"],
      doneWhenDone: [false],
      openQuestions: ["Rate limit?", "Auth token format?"],
    });
    expect(parseSpecStageDraft(base, "   ")).toBe(base);
    const md = `# 002. HTTP & GraphQL API

Status: approved
Depends on: 001-overview

## Context

Updated context with rate limits.

## Requirements

- [x] **API-01** List items
- [ ] **API-02** Limit requests to 100 req/min per IP

## Done when

- [x] Unit tests pass
- [ ] Rate-limit test passes

## Open questions

- Auth token format?
`;
    const preview = parseSpecStageDraft(base, md);
    expect(preview.title).toBe("HTTP & GraphQL API");
    expect(preview.status).toBe("approved");
    expect(preview.context).toBe("Updated context with rate limits.");
    expect(preview.items).toEqual([
      { id: "API-01", section: "requirements", text: "List items", done: true },
      { id: "API-02", section: "requirements", text: "Limit requests to 100 req/min per IP", done: false },
    ]);
    expect(preview.doneWhen).toEqual(["Unit tests pass", "Rate-limit test passes"]);
    expect(preview.doneWhenDone).toEqual([true, false]);
    expect(preview.openQuestions).toEqual(["Auth token format?"]);

    expect(hasUnsavedStageDraft("/ws", "002-api")).toBe(false);
    setStageDraft("/ws", "002-api", {
      answers: { 0: "100 req/min per IP" },
      newReq: "Pending requirement",
      stagedContent: md,
      changes: ["Folded rate limit into API-02"],
      baseHash: "sha256:v1",
    });
    setStageDraft("/ws", "001-overview", { newDoneWhen: "Overview check" });
    expect(hasUnsavedStageDraft("/ws", "002-api")).toBe(true);
    expect(hasUnsavedStageDraft("/ws", "001-overview")).toBe(true);
    expect(getStageDraft("/ws", "002-api").answers[0]).toBe("100 req/min per IP");
    expect(getStageDraft("/ws", "002-api").newReq).toBe("Pending requirement");
    expect(getStageDraft("/ws", "002-api").stagedContent).toContain("API-02");
    expect(getStageDraft("/ws", "001-overview").newDoneWhen).toBe("Overview check");
    resetSpecDraftStore();
    expect(hasUnsavedStageDraft("/ws", "002-api")).toBe(false);
  });
});
