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

// The docs' command table (docs/content/products/cli.md) names only
// commands that exist: the terminal's (its /help, helpGroups) or the
// bundled ones (pkg/engine/commands/builtin), so the docs can't drift from
// the code unseen.
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { terminalCommands } from "./terminalCommands";

const root = join(process.cwd(), "..", "..", "..");
const read = (...p: string[]) => (existsSync(join(root, ...p)) ? readFileSync(join(root, ...p), "utf8") : "");

/** The slash commands in the first column of the docs' command table. */
export function documentedCommands(md: string): string[] {
  const names = new Set<string>();
  for (const line of md.split("\n")) {
    const first = /^\|\s*([^|]+?)\s*\|/.exec(line)?.[1] ?? "";
    for (const m of first.matchAll(/`\/([a-z][\w:-]*)/g)) names.add(m[1]);
  }
  return [...names].sort();
}

describe("the docs' commands", () => {
  const cli = read("docs", "content", "products", "cli.md");
  const builtinDir = join(root, "pkg", "engine", "commands", "builtin");
  const bundled = existsSync(builtinDir) ? readdirSync(builtinDir).filter((f) => f.endsWith(".md")).map((f) => f.replace(/\.md$/, "")) : [];
  // /help and /show are the REPL's own, not rows in its /help.
  const known = new Set([...terminalCommands(read("apps", "cli", "internal", "tui", "commands.go")), ...bundled, "help", "show"]);
  it("reads the docs and the code", () => {
    expect(cli, "docs/content/products/cli.md is the test's data").not.toBe("");
    expect(bundled).toContain("spec");
    expect(known.size).toBeGreaterThan(40);
  });
  it.each(documentedCommands(cli).map((n) => [n]))("/%s exists", (name) => {
    expect(known.has(name), `docs/content/products/cli.md documents /${name}, which neither the terminal nor the bundled commands have`).toBe(true);
  });
  it("reads a table's first column", () => {
    expect(documentedCommands("| `/memory [add]`, `/init` | x `/not-this` |\n| `/goal <c>` | y |\ntext `/nor-this`")).toEqual(["goal", "init", "memory"]);
  });
});
