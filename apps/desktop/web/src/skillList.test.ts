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
import { skillFile, skillWhere } from "./skillList";

describe("skillWhere", () => {
  const ws = "/home/me/shop";
  const reg = ["/home/me/.blitz/skills/castor-pdf"];
  it.each([
    { dir: "", want: "builtin" },
    { dir: "/home/me/.blitz/skills/castor-pdf/", want: "registry" },
    { dir: "/home/me/shop/.agents/skills/deploy", want: "workspace" },
    { dir: "/home/me/shop/skills/lint", want: "workspace" },
    { dir: "/home/me/shopping/.agents/skills/x", want: "other" },
    { dir: "/home/me/.blitz/skills/mine", want: "user" },
    { dir: "/opt/plugins/acme/skills/a", want: "other" },
    { dir: "C:\\Users\\me\\shop\\.agents\\skills\\w", want: "other" },
  ])("$dir is $want", ({ dir, want }) => expect(skillWhere(dir, ws, reg)).toBe(want));

  it("takes Windows paths as the workspace's", () => {
    expect(skillWhere("C:\\Users\\me\\shop\\.agents\\skills\\w", "C:\\Users\\me\\shop", [])).toBe("workspace");
  });

  it("has no workspace without one open", () => {
    expect(skillWhere("/home/me/shop/.agents/skills/deploy", "", [])).toBe("other");
  });
});

describe("skillFile", () => {
  it.each([
    { dir: "/w/.agents/skills/deploy", ws: "/w", want: ".agents/skills/deploy/SKILL.md" },
    { dir: "/w/.agents/skills/deploy/", ws: "/w/", want: ".agents/skills/deploy/SKILL.md" },
    { dir: "/elsewhere/s", ws: "/w", want: "" },
    { dir: "/w/s", ws: "", want: "" },
  ])("$dir in $ws", ({ dir, ws, want }) => expect(skillFile(dir, ws)).toBe(want));
});
