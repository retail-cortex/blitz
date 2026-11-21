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

import { create, type DescService } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError, type ConnectRouter } from "@connectrpc/connect";
import { withKeepalives } from "./fakeChecks";
import { MessageSchema, SessionInfoSchema, type Message, type SessionInfo } from "../gen/blitz/v1/session_pb";
import { ErrorInfoSchema, UsageSchema, type Usage } from "../gen/blitz/v1/turn_pb";

/** Whether the page's address has ?name (the fake's switches). */
export const fakeParam = (name: string) => new URLSearchParams(location.search).has(name);

/** Async delay helper for fake streaming and simulated latency. */
export const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

/** Current timestamp as a protobuf Timestamp. */
export const now = () => timestampFromDate(new Date());

/** Timestamp min minutes in the past. */
export const ago = (min: number) => timestampFromDate(new Date(Date.now() - min * 60000));

/** Builds a fake Usage proto message. */
export const fakeUsage = (calls: number, input: number, output: number, usd: number) => create(UsageSchema, { calls, input: BigInt(input), output: BigInt(output), costUsd: usd, priced: true });

/** Builds a fake conversation Message proto. */
export const msg = (role: string, text: string, kind = ""): Message => create(MessageSchema, { role, text, kind, time: now() });

/** Throws a Connect NotFound error with NOT_FOUND ErrorInfo. */
export function notFound(what: string): never {
  const err = new ConnectError(`${what} not found`, Code.NotFound);
  err.details.push({ desc: ErrorInfoSchema, value: { reason: "NOT_FOUND" } });
  throw err;
}

/** An invalid-argument error with the service's reason (INVALID_ARGUMENT, as its invalid() sends). */
export function fakeInvalid(msgText: string, reason = "INVALID_ARGUMENT", metadata: Record<string, string> = {}): ConnectError {
  const err = new ConnectError(msgText, Code.InvalidArgument);
  err.details.push({ desc: ErrorInfoSchema, value: { reason, metadata } });
  return err;
}

/** A failed-precondition error with the service's reason, as the real one sends. */
export function fakeError(msgText: string, reason: string): ConnectError {
  const err = new ConnectError(msgText, Code.FailedPrecondition);
  err.details.push({ desc: ErrorInfoSchema, value: { reason } });
  return err;
}

/** Deterministic content version hash for fake files and specs. */
export const fakeVersion = (text: string) => String(text.length) + ":" + [...text].reduce((h, c) => (h * 31 + c.charCodeAt(0)) >>> 0, 7).toString(16);

/**
 * The router's service, its server streams passed on as the page server
 * does (withKeepalives): an empty message whenever one is quiet, which a
 * stream whose messages are the whole state must tell from an empty list.
 */
export function keepalivesFor(router: ConnectRouter): ConnectRouter["service"] {
  return ((def: DescService, impl: Record<string, unknown>, opts?: unknown) => {
    const wrapped: Record<string, unknown> = { ...impl };
    for (const m of def.methods) {
      const f = impl[m.localName];
      if (m.methodKind === "server_streaming" && typeof f === "function") wrapped[m.localName] = (req: unknown, ctx: { signal: AbortSignal }) => withKeepalives(f.call(impl, req, ctx) as AsyncIterable<unknown>, ctx.signal);
    }
    return router.service(def as never, wrapped as never, opts as never);
  }) as ConnectRouter["service"];
}

/** In-memory state for one fake workspace. */
export interface State {
  sessions: SessionInfo[];
  active: string;
  settings: { agent: string; model: string; provider: string; effort: string; mode: string; agency: string; locale: string; style: string; speechModel?: string; imageModel?: string };
  pending: Map<string, (answer: string) => void>;
  usage: Usage;
  // The project settings' trust state: the "shop" workspace has some.
  projectTrust?: string;
  // Background tasks, by ID: they finish a few seconds after they start.
  tasks: Map<string, { agent: string; prompt: string; sessionId: string; started: number; stopped?: number; answered?: number }>;
}

const intro = `Here's how **shop** is organised:

| Directory | What's there |
|---|---|
| \`cmd/shop\` | The server's \`main\`: flags, config, graceful shutdown |
| \`internal/cart\` | Cart pricing, with the discount rules |
| \`internal/store\` | Postgres access through \`sqlc\` |

Build and test with:

\`\`\`sh
make build
go test ./...
\`\`\`

The discount code lives in [\`internal/cart/discount.go\`](https://github.com/example/shop). Two things stand out:

1. \`ApplyCoupon\` rounds *before* summing line items, so totals can be off by a cent.
2. There's no test for expired coupons.`;

/** All opened fake workspaces by directory path. */
export const states = new Map<string, State>();

/** Returns or initializes in-memory state for a workspace directory. */
export function state(dir: string): State {
  let s = states.get(dir);
  if (!s) {
    const first = create(SessionInfoSchema, {
      id: "session-1",
      title: dir.endsWith("shop") ? "Understand the cart code" : "",
      workspace: dir,
      created: now(),
      updated: now(),
      messages: dir.endsWith("shop") ? [msg("user", "Explain how this project is organised, and how to build and test it."), msg("model", intro)] : [],
    });
    first.messageCount = first.messages.length;
    const other = create(SessionInfoSchema, { id: "session-0", title: "Fix the flaky checkout test", workspace: dir, messageCount: 14, updated: now() });
    s = {
      sessions: [
        first,
        other,
        create(SessionInfoSchema, { id: "session-w1", title: "⏰ nightly-deps 2026-10-04 02:00", origin: "worker", agent: "blitz", workspace: dir, messageCount: 6, updated: now() }),
        create(SessionInfoSchema, { id: "session-w2", title: "⏰ nightly-deps 2026-10-03 02:00", origin: "worker", agent: "blitz", workspace: dir, messageCount: 4, updated: now() }),
        create(SessionInfoSchema, { id: "session-1b", title: "Add the cart API docs", workspace: dir, messageCount: 9, updated: now() }),
      ],
      active: first.id,
      settings: { agent: "blitz", model: "gemini-4-argon", provider: "gemini", effort: "", mode: "auto", agency: "high", locale: "en-US", style: "default" },
      pending: new Map(),
      tasks: new Map(),
      usage: create(UsageSchema, { calls: 3, input: 18234n, output: 2210n, lastPrompt: 12876n, costUsd: 0.0123, priced: true }),
    };
    states.set(dir, s);
  }
  return s;
}

/** Active session inside a workspace's State. */
export const active = (s: State) => s.sessions.find((x) => x.id === s.active)!;

/** The home workspace the global workers run in (~/.blitz/home). */
export const fakeHome = "/home/me/.blitz/home";

/** Providers the fake has signed in to. */
export const fakeSignedIn = new Set<string>();

/**
 * The organization's service this computer is connected to (?org starts
 * connected), and whether the person is signed in to it.
 */
export const fakeOrg = { url: fakeParam("org") ? "https://blitz.acme.com" : "", signedIn: fakeParam("signedin"), termsAccepted: false };

/** Builds the fake EnterpriseState payload. */
export function fakeOrgState(checked: boolean) {
  if (!fakeOrg.url) return {};
  return {
    url: fakeOrg.url,
    name: "Acme Assist",
    version: "1.0.0",
    capabilities: ["gateway", "jobs"],
    signedIn: fakeOrg.signedIn,
    email: fakeOrg.signedIn ? "ana@acme.com" : "",
    personName: fakeOrg.signedIn ? "Ana Kim" : "",
    roles: fakeOrg.signedIn ? (fakeParam("admin") ? ["admin", "member"] : ["member"]) : [],
    renewsUnix: fakeOrg.signedIn ? BigInt(Math.floor(Date.now() / 1000) + 540) : 0n,
    checked,
    reachable: checked,
    // ?managed: device management set it (EN-10).
    managed: fakeParam("managed"),
    // The person's effective policy and why (EN-35, EN-39): Ollama with
    // llama3* for the Data group, connections locked off.
    ...(fakeOrg.signedIn
      ? {
          policy: [
            { path: "connectors.own", valueJson: "false", level: "global", by: [], locked: true },
            { path: "mcp.own", valueJson: "false", level: "default", by: [], locked: false },
            { path: "models.own.ollama.allowed", valueJson: "true", level: "group", by: ["Data"], locked: false },
            { path: "models.own.ollama.models", valueJson: '["llama3*"]', level: "group", by: ["Data"], locked: false },
            { path: "rules.tested", valueJson: "true", level: "person", by: ["ana@acme.com"], locked: false },
            { path: "tools.shell_commands", valueJson: "true", level: "default", by: [], locked: false },
          ],
          ownProviders: [{ provider: "ollama", models: ["llama3*"] }],
        }
      : {}),
  };
}

const fakeTools: Record<string, string> = { google: "gcloud", anthropic: "ant", aws: "aws", azure: "az" };

/** Returns fake CLI sign-in status for a provider. */
export function fakeSignIn(provider: string) {
  const found = provider === "google" || provider === "aws";
  return {
    provider,
    tool: fakeTools[provider],
    toolFound: found,
    install: "https://example.com/install",
    signedIn: fakeSignedIn.has(provider),
    detail: fakeSignedIn.has(provider) ? "you@example.com (authorized_user)" : found ? "no Application Default Credentials" : "",
  };
}

/** Mutable task-alerts preference shared between ConfigService and WorkspaceService. */
export const fakeAlertsState = { value: "notify" };
