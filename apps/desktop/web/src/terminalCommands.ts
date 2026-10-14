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

// The terminal's slash commands, read from its source (the REPL's
// helpGroups in apps/cli/internal/tui/commands.go), for the tests that keep
// the window's commands and the docs in step with them.

/** The command names in the REPL's helpGroups usages: "/config [...], /set" is config and set. */
export function terminalCommands(source: string): string[] {
  const start = source.indexOf("var helpGroups");
  const end = source.indexOf("\n}\n", start);
  const block = source.slice(start, end);
  const names = new Set<string>();
  for (const [, usage] of block.matchAll(/\{"([^"]+)", "help\.[a-z_]+"\}/g)) {
    for (const part of usage.split(", ")) {
      if (part.startsWith("!")) names.add("!");
      const m = /^\/([a-z_]+)/.exec(part);
      if (m) names.add(m[1]);
    }
  }
  return [...names].sort();
}
