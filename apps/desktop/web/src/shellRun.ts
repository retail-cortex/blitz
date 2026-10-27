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

// ! commands in the chat, as the terminal's: what the agent's shell code
// blocks would run (their Run button puts it in the composer as !…), and
// whether a line is one.
import { createContext } from "react";

/** The languages a fenced block is a shell command in. */
const shellLangs = new Set(["sh", "bash", "shell", "zsh", "console", "terminal", "shell-session", "shellsession"]);

/**
 * The command a code block would run, "" when it isn't a shell block. In
 * a console block (lines with a $ or % prompt among their output), the
 * prompted lines alone, without the prompt; in another, its lines as they
 * are, comments and blank lines dropped.
 */
export function shellCommandOf(lang: string, code: string): string {
  if (!shellLangs.has(lang.toLowerCase())) return "";
  const lines = code.split("\n");
  const prompted = lines.filter((l) => /^\s*[$%]\s+/.test(l));
  const keep = prompted.length ? prompted.map((l) => l.replace(/^\s*[$%]\s+/, "")) : lines;
  return keep
    .map((l) => l.trimEnd())
    .filter((l) => l.trim() && !/^\s*#/.test(l))
    .join("\n");
}

/** Whether a composer line is a ! command, and the command after it. */
export function shellLine(text: string): { shell: boolean; command: string } {
  const t = text.trimStart();
  return t.startsWith("!") ? { shell: true, command: t.slice(1).trim() } : { shell: false, command: "" };
}

/**
 * The conversation's way to put a code block's command in its composer
 * as !command (null outside a conversation: no Run button).
 */
export const ShellRunContext = createContext<((command: string) => void) | null>(null);
