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

// Settings › MCP servers (spec_mcp_009 MCP-46): the form for one server,
// to and from MCPServerConfig, and the list as the page shows it.
import { create } from "@bufbuild/protobuf";
import { MCPServerConfigSchema, type MCPServerConfig } from "./gen/blitz/v1/config_pb";

/** The form's fields, as typed. */
export interface McpForm {
  name: string;
  kind: "command" | "url";
  command: string;
  /** One argument a line. */
  args: string;
  /** KEY=value, a line each. */
  env: string;
  url: string;
  /** Name: value, a line each. */
  headers: string;
  autoApprove: boolean;
  sandbox: boolean;
  /** Seconds, "" for the default. */
  timeout: string;
  /** Comma-separated; "" for the main agent, * for all. */
  agents: string;
  disabled: boolean;
}

/** An empty form: a new command server, sandboxed. */
export const emptyForm: McpForm = { name: "", kind: "command", command: "", args: "", env: "", url: "", headers: "", autoApprove: false, sandbox: true, timeout: "", agents: "", disabled: false };

const lines = (s: string) =>
  s
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean);

/** Lines of "key<sep>value" as a map; a line without sep is a problem. */
export function parsePairs(s: string, sep: "=" | ":"): { pairs: Record<string, string>; bad: string[] } {
  const pairs: Record<string, string> = {};
  const bad: string[] = [];
  for (const l of lines(s)) {
    const i = l.indexOf(sep);
    if (i <= 0) {
      bad.push(l);
      continue;
    }
    pairs[l.slice(0, i).trim()] = l.slice(i + 1).trim();
  }
  return { pairs, bad };
}

/** A map as lines of "key<sep>value", sorted by key. */
export function formatPairs(m: Record<string, string> | undefined, sep: "=" | ":"): string {
  return Object.keys(m ?? {})
    .sort()
    .map((k) => `${k}${sep === ":" ? ": " : "="}${m![k]}`)
    .join("\n");
}

/** A server's settings as the form shows them. */
export function toForm(s: MCPServerConfig): McpForm {
  return {
    name: s.name,
    kind: s.url ? "url" : "command",
    command: s.command,
    args: s.args.join("\n"),
    env: formatPairs(s.env, "="),
    url: s.url,
    headers: formatPairs(s.headers, ":"),
    autoApprove: s.autoApprove,
    sandbox: s.sandbox !== false,
    timeout: s.timeoutSeconds ? String(s.timeoutSeconds) : "",
    agents: s.agents.join(", "),
    disabled: s.disabled,
  };
}

/** What's wrong with the form, as i18n keys (none: it can be saved). */
export function formProblems(f: McpForm, taken: readonly string[]): string[] {
  const out: string[] = [];
  const name = f.name.trim();
  if (!name) out.push("desktop.mcp.problem.name");
  else if (!/^[\w.-]+$/.test(name)) out.push("desktop.mcp.problem.name_chars");
  else if (taken.includes(name)) out.push("desktop.mcp.problem.name_taken");
  if (f.kind === "command" && !f.command.trim()) out.push("desktop.mcp.problem.command");
  if (f.kind === "url" && !/^https?:\/\/\S+$/.test(f.url.trim())) out.push("desktop.mcp.problem.url");
  if (f.kind === "command" && parsePairs(f.env, "=").bad.length) out.push("desktop.mcp.problem.env");
  if (f.kind === "url" && parsePairs(f.headers, ":").bad.length) out.push("desktop.mcp.problem.headers");
  if (f.timeout.trim() && !/^\d+$/.test(f.timeout.trim())) out.push("desktop.mcp.problem.timeout");
  return out;
}

/** The form as a server's settings (check formProblems first). */
export function fromForm(f: McpForm): MCPServerConfig {
  const command = f.kind === "command";
  return create(MCPServerConfigSchema, {
    name: f.name.trim(),
    command: command ? f.command.trim() : "",
    args: command ? lines(f.args) : [],
    env: command ? parsePairs(f.env, "=").pairs : {},
    url: command ? "" : f.url.trim(),
    headers: command ? {} : parsePairs(f.headers, ":").pairs,
    autoApprove: f.autoApprove,
    // Only a stdio server runs sandboxed; on is the default, left unsaid.
    sandbox: command && !f.sandbox ? false : undefined,
    timeoutSeconds: f.timeout.trim() ? Number(f.timeout.trim()) : 0,
    agents: f.agents
      .split(",")
      .map((a) => a.trim())
      .filter(Boolean),
    disabled: f.disabled,
  });
}

/** A server's command line or URL. */
export const targetOf = (s: MCPServerConfig) => s.url || [s.command, ...s.args].join(" ");
