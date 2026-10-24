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

import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { AgentBrowserCommandSchema } from "./gen/blitz/v1/workspace_pb";
import { openedMessage, runRelay, type RelayEnds } from "./agentBrowserRelay";

const cmd = (c: Partial<{ browserId: string; kind: string; message: string; show: string; profile: string; allowLocal: boolean; denyDomains: string[] }>) => ({ command: create(AgentBrowserCommandSchema, c) });

/** Ends whose stream yields commands as they're pushed, recording what the relay did. */
function fakeEnds(failOpen = false) {
  const log: string[] = [];
  const listeners: Record<string, (arg: unknown) => void> = {};
  const pending: ({ command?: ReturnType<typeof cmd>["command"] } | null)[] = [];
  let wake: (() => void) | undefined;
  const push = (c: ReturnType<typeof cmd> | null) => {
    pending.push(c);
    wake?.();
  };
  const ends: RelayEnds = {
    async *watch(signal) {
      for (;;) {
        while (pending.length) {
          const c = pending.shift()!;
          if (c === null) return; // the stream ends
          yield c;
        }
        if (signal.aborted) return;
        await new Promise<void>((r) => (wake = r));
      }
    },
    reply: async (r) => void log.push(`reply ${r.browserId} ${r.messages.join("|")}${r.closed ? ` closed: ${r.reason}` : ""}`),
    open: async (id, show, profile, allowLocal, deny) => {
      if (failOpen) throw new Error("no window");
      log.push(`open ${id} ${show} ${profile || "-"} ${allowLocal} ${deny.join(",")}`);
    },
    send: async (id, m) => void log.push(`send ${id} ${m}`),
    close: async (id) => void log.push(`close ${id}`),
    on: (event, f) => {
      listeners[event] = f;
      return () => delete listeners[event];
    },
    showTab: (id) => void log.push(`tab ${id}`),
  };
  return { ends, log, push, emit: (event: string, arg: unknown) => listeners[event]?.(arg), listening: () => Object.keys(listeners).length };
}

const settle = () => new Promise((r) => setTimeout(r, 0));

describe("the agent's browser relay", () => {
  it("opens views, carries messages both ways in order, and says when they close", async () => {
    const f = fakeEnds();
    const ctl = new AbortController();
    const done = runRelay(f.ends, ctl.signal, 1);
    f.push(cmd({ browserId: "agent-1", kind: "open", show: "watch", profile: "work", allowLocal: true, denyDomains: ["evil.example"] }));
    await settle();
    f.push(cmd({ browserId: "agent-1", kind: "message", message: '{"id":1,"method":"Page.navigate"}' }));
    await settle();
    f.emit("agentbrowser:message", { id: "agent-1", message: '{"id":1,"result":{}}' });
    f.emit("agentbrowser:message", { id: "agent-1", message: '{"method":"Page.loadEventFired"}' });
    f.emit("agentbrowser:message", { id: "someone-else", message: "{}" });
    await settle();
    f.emit("agentbrowser:closed", { id: "agent-1", reason: "the user took over the page" });
    await settle();
    expect(f.log).toEqual([
      "open agent-1 watch work true evil.example",
      "tab agent-1",
      `reply agent-1 ${openedMessage}`,
      'send agent-1 {"id":1,"method":"Page.navigate"}',
      'reply agent-1 {"id":1,"result":{}}',
      'reply agent-1 {"method":"Page.loadEventFired"}',
      "reply agent-1  closed: the user took over the page",
    ]);
    ctl.abort();
    f.push(null);
    await done;
    expect(f.listening()).toBe(0); // it stops listening
  });

  it("a silent view isn't a tab; the service's close closes it; leaving closes what's left", async () => {
    const f = fakeEnds();
    const ctl = new AbortController();
    const done = runRelay(f.ends, ctl.signal, 1);
    f.push(cmd({ browserId: "agent-2", kind: "open", show: "silent" }));
    f.push(cmd({ browserId: "agent-3", kind: "open", show: "silent" }));
    await settle();
    f.push(cmd({ browserId: "agent-2", kind: "close" }));
    await settle();
    ctl.abort();
    f.push(null);
    await done;
    expect(f.log.filter((l) => l.startsWith("tab"))).toEqual([]);
    expect(f.log).toContain("close agent-2");
    expect(f.log.at(-1)).toBe("close agent-3");
  });

  it("a view that can't open is closed, with why", async () => {
    const f = fakeEnds(true);
    const ctl = new AbortController();
    const done = runRelay(f.ends, ctl.signal, 1);
    f.push(cmd({ browserId: "agent-4", kind: "open", show: "watch" }));
    await settle();
    ctl.abort();
    f.push(null);
    await done;
    expect(f.log).toEqual(["reply agent-4  closed: no window"]);
  });

  it("watches again when the stream ends (a service restart)", async () => {
    const f = fakeEnds();
    const ctl = new AbortController();
    const done = runRelay(f.ends, ctl.signal, 1);
    f.push(null);
    await new Promise((r) => setTimeout(r, 10));
    f.push(cmd({ browserId: "agent-5", kind: "open", show: "silent" }));
    await new Promise((r) => setTimeout(r, 10));
    ctl.abort();
    f.push(null);
    await done;
    expect(f.log[0]).toBe("open agent-5 silent - false ");
  });
});
