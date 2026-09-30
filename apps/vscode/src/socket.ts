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

// Where the Blitz service listens: the setting, else $BLITZ_SOCKET, else
// ~/.blitz/run/blitz.sock (pkg/socket's DefaultSocket).
import { homedir } from "node:os";
import { join } from "node:path";

/** Expands a leading ~ to the home directory. */
export function expandHome(p: string, home = homedir()): string {
  return p === "~" ? home : p.startsWith("~/") ? join(home, p.slice(2)) : p;
}

/** The service's socket, from the setting (may be ""), the environment and the home directory. */
export function serviceSocket(setting: string, env: NodeJS.ProcessEnv = process.env, home = homedir()): string {
  const p = setting.trim() || env.BLITZ_SOCKET || "~/.blitz/run/blitz.sock";
  return expandHome(p, home);
}
