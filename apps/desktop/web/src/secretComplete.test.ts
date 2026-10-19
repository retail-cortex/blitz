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
import { insertSecret, missingSecrets, secretOptions, secretTokenAt, type SecretName } from "./secretComplete";

const names: SecretName[] = [
  { key: "github_token", scope: "global" },
  { key: "gcp_key", scope: "global" },
  { key: "github_token", scope: "workspace" },
  { key: "db_pass", scope: "workspace" },
];
const labels = { global: "Global", workspace: "This workspace" };

describe("secretTokenAt", () => {
  it.each([
    { text: "use {secrets:", caret: 13, want: { start: 4, end: 13, query: "" } },
    { text: "use {secrets:global:gi", caret: 22, want: { start: 4, end: 22, query: "global:gi" } },
    { text: "use {secrets:x} done", caret: 20, want: null },
    { text: "no token", caret: 8, want: null },
  ])("$text", ({ text, caret, want }) => expect(secretTokenAt(text, caret)).toEqual(want));
});

describe("secretOptions", () => {
  const labelsOf = (q: string, ws = true) => secretOptions(q, names, ws, labels).map((o) => o.label);
  it("offers the scopes first, then names in either", () => {
    expect(labelsOf("")).toEqual(["global:", "workspace:", "global:github_token", "global:gcp_key", "workspace:github_token", "workspace:db_pass"]);
    expect(labelsOf("g")).toEqual(["global:", "global:github_token", "global:gcp_key", "workspace:github_token"]);
  });
  it("after a scope, that scope's names", () => {
    expect(labelsOf("global:")).toEqual(["global:github_token", "global:gcp_key"]);
    expect(labelsOf("workspace:d")).toEqual(["workspace:db_pass"]);
    expect(labelsOf("other:")).toEqual([]);
  });
  it("without a workspace, only the global scope", () => {
    expect(labelsOf("")).toContain("workspace:");
    expect(labelsOf("", false)).toEqual(["global:", "global:github_token", "global:gcp_key"]);
    expect(labelsOf("workspace:", false)).toEqual([]);
  });
  it("puts names in scoped, scopes open", () => {
    const [scope, , name] = secretOptions("", names, true, labels);
    expect(scope).toMatchObject({ insert: "{secrets:global:", scope: true });
    expect(name.insert).toBe("{secrets:global:github_token}");
  });
});

describe("insertSecret", () => {
  it("replaces the token and what's left of it", () => {
    const text = "a {secrets:git} b";
    const token = secretTokenAt(text, 14)!;
    expect(insertSecret(text, token, "{secrets:global:github_token}")).toEqual({ text: "a {secrets:global:github_token} b", caret: 31 });
  });
  it("keeps typing after a scope", () => {
    const text = "a {secrets:gl";
    expect(insertSecret(text, secretTokenAt(text, text.length)!, "{secrets:global:")).toEqual({ text: "a {secrets:global:", caret: 18 });
  });
});

describe("missingSecrets", () => {
  it("names what isn't there, once", () => {
    const text = "{secrets:global:github_token} {secrets:workspace:gcp_key} {secrets:db_pass} {secrets:nope} {secrets:nope}";
    expect(missingSecrets(text, names)).toEqual(["{secrets:workspace:gcp_key}", "{secrets:nope}"]);
  });
});
