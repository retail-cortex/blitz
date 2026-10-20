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
import { SkillFileSchema } from "../gen/blitz/v1/workspace_pb";
import { activePackageFiles, addTrigger, emptySkillForm, filePath, packageKind, packageTemplate, formFromSkill, kindOf, languageOfScript, sameSkillForm, scenarioName, skillName, specFromForm, splitArgs, validSkillName } from "./skillForm";

describe("the skill form", () => {
  it("reads a skill, tools as a list, and writes it back", () => {
    const f = create(SkillFileSchema, { spec: { name: "notes", description: "Notes", tools: "read_file  grep", agents: ["reviewer"], members: ["a"], instructions: "Do it.", triggers: ["write notes"] } });
    const form = formFromSkill(f);
    expect(form).toEqual({ name: "notes", description: "Notes", tools: ["read_file", "grep"], agents: ["reviewer"], members: ["a"], instructions: "Do it.", triggers: ["write notes"] });
    expect(specFromForm({ ...form, name: " notes " })).toEqual({ name: "notes", description: "Notes", tools: "read_file grep", agents: ["reviewer"], members: ["a"], instructions: "Do it.", triggers: ["write notes"] });
    expect(sameSkillForm(form, { ...form, triggers: [] })).toBe(false);
    expect(sameSkillForm(form, { ...form, name: "notes " })).toBe(true);
    expect(sameSkillForm(form, { ...form, members: [] })).toBe(false);
    expect(formFromSkill(create(SkillFileSchema, {}))).toEqual(emptySkillForm);
  });

  it.each([
    ["pdf", true],
    ["release-notes-2", true],
    ["", false],
    ["Notes", false],
    ["-x", false],
    ["x-", false],
    ["a--b", false],
    ["a_b", false],
    ["a".repeat(65), false],
  ])("names: %s → %s", (name, want) => {
    expect(validSkillName(name)).toBe(want);
  });

  it.each([
    ["Release notes!", "release-notes"],
    ["  Écrire  les   notes ", "ecrire-les-notes"],
    ["---", ""],
  ])("makes a name from %j", (text, want) => {
    expect(skillName(text)).toBe(want);
  });

  it("tells the kind, simple when unsaid", () => {
    expect(kindOf({ kind: "meta" })).toBe("meta");
    expect(kindOf({ kind: "" })).toBe("simple");
    expect(kindOf({ kind: "other" })).toBe("simple");
  });

  it.each([
    ["references", "style", ".md", "references/style.md"],
    ["references", "deep/guide.txt", ".md", "references/deep/guide.txt"],
    ["scripts", "fetch", ".py", "scripts/fetch.py"],
    ["scripts", "../x", ".py", ""],
    ["scripts", ".hidden", ".py", ""],
    ["references", "  ", ".md", ""],
  ] as const)("new file %s/%s", (folder, name, ext, want) => {
    expect(filePath(folder, name, ext)).toBe(want);
  });

  it("tells which package files count: the first of each kind", () => {
    expect([...activePackageFiles(["scripts/requirements.txt", "pyproject.toml", "package.json"])]).toEqual(["scripts/requirements.txt", "package.json"]);
    expect([...activePackageFiles([])]).toEqual([]);
    expect(packageKind("scripts/package.json")).toBe("node");
    expect(packageKind("pyproject.toml")).toBe("python");
    expect(packageTemplate("scripts/package.json")).toContain('"dependencies"');
    expect(packageTemplate("scripts/requirements.txt")).toMatch(/^#/);
  });

  it("tells a script's language, and splits a test run's arguments", () => {
    expect(languageOfScript("scripts/a.py")).toBe("python");
    expect(languageOfScript("scripts/a.ts")).toBe("typescript");
    expect(languageOfScript("scripts/a.sh")).toBe("shell");
    expect(languageOfScript("scripts/a.BAT")).toBe("cmd");
    expect(languageOfScript("scripts/a.ps1")).toBe("powershell");
    expect(languageOfScript("scripts/a.mjs")).toBe("javascript");
    expect(languageOfScript("scripts/a.rb")).toBe("");
    expect(splitArgs(`--from v1 "the notes" 'x y' z`)).toEqual(["--from", "v1", "the notes", "x y", "z"]);
    expect(splitArgs("  ")).toEqual([]);
  });
});

describe("trigger phrases and scenarios", () => {
  it.each([
    { name: "a phrase, tidied", list: [], typed: "  write   the notes ", want: ["write the notes"] },
    { name: "one already there", list: ["write the notes"], typed: "write the notes", want: ["write the notes"] },
    { name: "blank", list: ["a"], typed: "   ", want: ["a"] },
  ])("adds $name", ({ list, typed, want }) => expect(addTrigger(list, typed)).toEqual(want));

  it.each([
    { name: "from the prompt's first words", prompt: "Write the release notes for v2.4 please", taken: [], want: "write-the-release-notes-for" },
    { name: "beside one taken", prompt: "Write notes", taken: ["write-notes"], want: "write-notes-2" },
    { name: "with nothing usable", prompt: "?!", taken: [], want: "scenario" },
  ])("names a scenario $name", ({ prompt, taken, want }) => expect(scenarioName(prompt, taken)).toBe(want));
});
