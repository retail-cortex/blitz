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
import { expandHome, serviceSocket } from "./socket";

describe("serviceSocket", () => {
  it.each([
    ["the setting", "/run/b.sock", { BLITZ_SOCKET: "/env.sock" }, "/run/b.sock"],
    ["the environment", "", { BLITZ_SOCKET: "/env.sock" }, "/env.sock"],
    ["the default", " ", {}, "/home/me/.blitz/run/blitz.sock"],
    ["a setting under home", "~/s.sock", {}, "/home/me/s.sock"],
  ])("%s", (_, setting, env, want) => expect(serviceSocket(setting, env, "/home/me")).toBe(want));
  it("expands only a leading ~", () => {
    expect(expandHome("~", "/h")).toBe("/h");
    expect(expandHome("/a/~/b", "/h")).toBe("/a/~/b");
  });
});
