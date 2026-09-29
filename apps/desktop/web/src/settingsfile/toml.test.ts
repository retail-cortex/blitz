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
import { findSetting, keyAt, searchSettings, splitKey, type SettingRef } from "./toml";

const file = `# settings
[llm]
provider = "gemini"
  max_retries=3 # a comment

[llm.gemini]
model = "x"
[model_settings."gpt-5.1"]
top_p = 0.9
[permissions]
allow = [
  "shell(ls)",
]
deny = ["shell(rm [x])"]
[[hooks.pre_tool]]
command = "echo"
ui.theme = "dark"
`;

describe("keyAt", () => {
  it.each([
    { line: 1, want: null },
    { line: 2, want: { path: ["llm"], from: 1, to: 4 } },
    { line: 3, want: { path: ["llm", "provider"], from: 0, to: 8 } },
    { line: 4, want: { path: ["llm", "max_retries"], from: 2, to: 13 } },
    { line: 5, want: null },
    { line: 7, want: { path: ["llm", "gemini", "model"], from: 0, to: 5 } },
    { line: 8, want: { path: ["model_settings", "gpt-5.1"], from: 1, to: 25 } },
    { line: 9, want: { path: ["model_settings", "gpt-5.1", "top_p"], from: 0, to: 5 } },
    { line: 11, want: { path: ["permissions", "allow"], from: 0, to: 5 } },
    { line: 12, want: null },
    { line: 14, want: { path: ["permissions", "deny"], from: 0, to: 4 } },
    { line: 15, want: { path: ["hooks", "pre_tool"], from: 2, to: 16 } },
    { line: 16, want: { path: ["hooks", "pre_tool", "command"], from: 0, to: 7 } },
    { line: 17, want: { path: ["hooks", "pre_tool", "ui", "theme"], from: 0, to: 8 } },
    { line: 99, want: null },
  ])("line $line", ({ line, want }) => {
    expect(keyAt(file, line)).toEqual(want);
  });
});

describe("splitKey", () => {
  it.each([
    ["llm", ["llm"]],
    ["llm.gemini", ["llm", "gemini"]],
    [` model_settings . "a.b" `, ["model_settings", "a.b"]],
    [`pricing.'claude-opus-5'.x`, ["pricing", "claude-opus-5", "x"]],
  ])("%s", (s, want) => {
    expect(splitKey(s)).toEqual(want);
  });
});

const ref: SettingRef[] = [
  { key: "llm", type: "table", default: "", doc: "Providers." },
  { key: "llm.provider", type: "string", default: '"gemini"', doc: "The model provider." },
  { key: "model_settings.<model>.top_p", type: "number", default: "", doc: "Nucleus sampling." },
  { key: "hooks.pre_tool[]", type: "array of tables", default: "", doc: "Hooks before a tool." },
  { key: "hooks.pre_tool[].command", type: "string", default: "", doc: "Run with bash." },
];

describe("findSetting", () => {
  it.each([
    [["llm"], "llm"],
    [["llm", "provider"], "llm.provider"],
    [["model_settings", "gpt-5.1", "top_p"], "model_settings.<model>.top_p"],
    [["hooks", "pre_tool"], "hooks.pre_tool[]"],
    [["hooks", "pre_tool", "command"], "hooks.pre_tool[].command"],
    [["llm", "nope"], undefined],
  ])("%j", (key, want) => {
    expect(findSetting(ref, key)?.key).toBe(want);
  });
});

describe("searchSettings", () => {
  it.each([
    ["", 5],
    ["model provider", 1],
    ["HOOKS bash", 1],
    ["sampling top", 1],
    ["nothing", 0],
  ])("%s", (q, n) => {
    expect(searchSettings(ref, q)).toHaveLength(n);
  });
});
