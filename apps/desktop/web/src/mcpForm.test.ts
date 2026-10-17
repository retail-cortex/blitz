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
import { MCPServerConfigSchema } from "./gen/blitz/v1/config_pb";
import { MCPServerSchema } from "./gen/blitz/v1/workspace_pb";
import { emptyForm, formatPairs, formProblems, fromForm, mcpRows, parsePairs, toForm } from "./mcpForm";

describe("pairs", () => {
  it("reads lines of key and value, and says which aren't", () => {
    expect(parsePairs("A=1\n\n B = two=2 \nnope\n=x", "=")).toEqual({ pairs: { A: "1", B: "two=2" }, bad: ["nope", "=x"] });
    expect(parsePairs("Authorization: Bearer x:y", ":")).toEqual({ pairs: { Authorization: "Bearer x:y" }, bad: [] });
  });
  it("writes them sorted", () => {
    expect(formatPairs({ B: "2", A: "1" }, "=")).toBe("A=1\nB=2");
    expect(formatPairs({ X: "y" }, ":")).toBe("X: y");
    expect(formatPairs(undefined, "=")).toBe("");
  });
});

describe("the form", () => {
  it("round-trips a command server", () => {
    const s = create(MCPServerConfigSchema, { name: "fs", command: "npx", args: ["@acme/fs", "--ro"], env: { ROOT: "/srv" }, autoApprove: true, sandbox: false, timeoutSeconds: 60, agents: ["qa", "*"], disabled: true });
    const f = toForm(s);
    expect(f).toMatchObject({ kind: "command", args: "@acme/fs\n--ro", env: "ROOT=/srv", sandbox: false, timeout: "60", agents: "qa, *", disabled: true });
    expect(fromForm(f)).toMatchObject({ name: "fs", command: "npx", args: ["@acme/fs", "--ro"], env: { ROOT: "/srv" }, url: "", sandbox: false, timeoutSeconds: 60, agents: ["qa", "*"], disabled: true, autoApprove: true });
  });

  it("round-trips a URL server, leaving out what's for commands", () => {
    const f = toForm(create(MCPServerConfigSchema, { name: "docs", url: "https://mcp.example/docs", headers: { "X-Team": "core" } }));
    expect(f).toMatchObject({ kind: "url", headers: "X-Team: core", sandbox: true, timeout: "" });
    const s = fromForm({ ...f, command: "leftover", env: "A=1" });
    expect(s).toMatchObject({ url: "https://mcp.example/docs", headers: { "X-Team": "core" }, command: "", args: [], env: {}, timeoutSeconds: 0 });
    expect(s.sandbox).toBeUndefined();
  });

  it.each([
    { name: "empty", f: emptyForm, want: ["desktop.mcp.problem.name", "desktop.mcp.problem.command"] },
    { name: "bad name", f: { ...emptyForm, name: "my server", command: "x" }, want: ["desktop.mcp.problem.name_chars"] },
    { name: "taken", f: { ...emptyForm, name: "fs", command: "x" }, want: ["desktop.mcp.problem.name_taken"] },
    { name: "bad url", f: { ...emptyForm, name: "d", kind: "url" as const, url: "ftp://x" }, want: ["desktop.mcp.problem.url"] },
    { name: "bad env", f: { ...emptyForm, name: "d", command: "x", env: "NOPE" }, want: ["desktop.mcp.problem.env"] },
    { name: "bad header", f: { ...emptyForm, name: "d", kind: "url" as const, url: "https://x", headers: "NOPE" }, want: ["desktop.mcp.problem.headers"] },
    { name: "bad timeout", f: { ...emptyForm, name: "d", command: "x", timeout: "1.5" }, want: ["desktop.mcp.problem.timeout"] },
    { name: "fine", f: { ...emptyForm, name: "d.b-1", command: "x", timeout: "30" }, want: [] },
  ])("$name", ({ f, want }) => expect(formProblems(f, ["fs"])).toEqual(want));
});

describe("mcpRows", () => {
  it("lists the global servers with their state, then the project's", () => {
    const configs = [create(MCPServerConfigSchema, { name: "fs", command: "npx", args: ["a"] }), create(MCPServerConfigSchema, { name: "docs", url: "https://d" })];
    const running = [create(MCPServerSchema, { name: "db", target: "db-mcp", state: "connected" }), create(MCPServerSchema, { name: "fs", target: "npx a", state: "failed" })];
    const rows = mcpRows(configs, running);
    expect(rows.map((r) => [r.name, r.target, r.running?.state ?? "", !!r.config])).toEqual([
      ["fs", "npx a", "failed", true],
      ["docs", "https://d", "", true],
      ["db", "db-mcp", "connected", false],
    ]);
  });
});
