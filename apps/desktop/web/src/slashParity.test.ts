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

// The window's slash commands match the terminal's (spec_024_desktop
// DSK-102): every command in the REPL's command tree (Commands in
// apps/cli/internal/tui/commandtree.go), and each of its other names, is a built-in here, or one a window
// has no use for (terminalOnly), so one added there can't be missed here.
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { builtinCommands, composerSyntax, terminalOnly } from "./slashCommands";
import { terminalCommands } from "./terminalCommands";

// The terminal's other names for a command the window has.
const aliases: Record<string, string> = {
  set: "config",
  show: "config",
  usage: "cost",
  ps: "processes",
  perms: "permissions",
  pin: "pin_model",
  lang: "locale",
  language: "locale",
};

describe("parity with the terminal", () => {
  const file = join(process.cwd(), "..", "..", "..", "apps", "cli", "internal", "tui", "commandtree.go");
  it("reads the terminal's commands", () => {
    expect(existsSync(file), "the REPL's commandtree.go is the test's data").toBe(true);
  });
  const terminal = existsSync(file) ? terminalCommands(readFileSync(file, "utf8")) : [];
  it("finds them all", () => {
    expect(terminal.length).toBeGreaterThan(40);
    expect(terminal).toContain("set");
    expect(terminal).toContain("!");
  });
  const ours = new Set(builtinCommands.map((b) => b.name));
  it.each(terminal.map((n) => [n]))("/%s is here, or terminal-only", (name) => {
    const here = ours.has(aliases[name] ?? name) || (terminalOnly as readonly string[]).includes(name) || (composerSyntax as readonly string[]).includes(name);
    expect(here, `the terminal's /${name} has no desktop command: add it to slashCommands.ts, or to terminalOnly with the window's way in desktop.help.terminal_only`).toBe(true);
  });
});
