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
import { shellCommandOf, shellLine } from "./shellRun";

describe("shellCommandOf", () => {
  it.each([
    ["sh", "make build\ngo test ./...", "make build\ngo test ./..."],
    ["bash", "# install it\nbrew install jq\n\n", "brew install jq"],
    ["Shell", "ls -la", "ls -la"],
    ["console", "$ npm test\n> 12 passing\n$ npm run lint", "npm test\nnpm run lint"],
    ["zsh", "% echo hi\nhi", "echo hi"],
    ["console", "just output", "just output"],
    ["go", "fmt.Println(1)", ""],
    ["", "ls", ""],
  ])("%s: %j", (lang, code, want) => expect(shellCommandOf(lang, code)).toBe(want));
});

describe("shellLine", () => {
  it.each([
    ["!ls -la", { shell: true, command: "ls -la" }],
    ["  ! git status ", { shell: true, command: "git status" }],
    ["!", { shell: true, command: "" }],
    ["run ls!", { shell: false, command: "" }],
    ["/help", { shell: false, command: "" }],
  ])("%j", (text, want) => expect(shellLine(text)).toEqual(want));
});
