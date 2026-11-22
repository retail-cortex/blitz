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

// The terminal's slash commands, read from its source (the REPL's command
// tree, Commands in apps/cli/internal/tui/commandtree.go, spec_067_cli_parity
// CLP-10), for the tests that keep the window's commands and the docs in step
// with them.

/** The top-level command names and aliases in the REPL's command tree, and ! when it runs shell commands. */
export function terminalCommands(source: string): string[] {
  const start = source.indexOf("var Commands = []*Command{");
  const end = source.indexOf("\n}\n", start);
  const block = source.slice(start, end);
  const names = new Set<string>();
  for (const [, line] of block.matchAll(/^\t\{(Name: .*)$/gm)) {
    const name = /^Name: "([a-z_]+)"/.exec(line);
    if (name) names.add(name[1]);
    const aliases = /Aliases: \[\]string\{([^}]*)\}/.exec(line);
    for (const [, a] of (aliases?.[1] ?? "").matchAll(/"([a-z_]+)"/g)) names.add(a);
  }
  if (source.includes('"!<command>"')) names.add("!");
  return [...names].sort();
}
