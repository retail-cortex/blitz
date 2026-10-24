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

// The agent's browser in the desktop app (spec_agent_browser_050): the
// page carries the service's commands for the window's views (open one,
// a DevTools message, close it) to the window, and the views' messages
// back, in order, a view at a time. A watched view shows as a tab.
import { workspaces } from "./api";
import { agentBrowser } from "./desktop";
import type { AgentBrowserCommand } from "./gen/blitz/v1/workspace_pb";

/** What the relay needs of the window and the service (replaced in tests). */
export interface RelayEnds {
  watch: (signal: AbortSignal) => AsyncIterable<{ command?: AgentBrowserCommand }>;
  reply: (r: { browserId: string; messages: string[]; closed: boolean; reason: string }) => Promise<unknown>;
  open: (id: string, show: string, profile: string, allowLocal: boolean, deny: string[]) => Promise<unknown>;
  send: (id: string, message: string) => Promise<unknown>;
  close: (id: string) => Promise<unknown>;
  on: (event: "agentbrowser:message" | "agentbrowser:closed", f: (arg: unknown) => void) => () => void;
  /** Shows the watched view id as a tab. */
  showTab: (id: string) => void;
}

/** The ends for workspace dir: the service's stream and the Electron window. */
export function relayEnds(dir: string, showTab: (id: string) => void): RelayEnds {
  return {
    watch: (signal) => workspaces.watchAgentBrowser({ workspace: dir }, { signal }),
    reply: (r) => workspaces.agentBrowserReply({ workspace: dir, ...r }),
    open: agentBrowser.open,
    send: agentBrowser.send,
    close: agentBrowser.close,
    on: agentBrowser.on,
    showTab,
  };
}

/** The opened message the service waits for from a new view. */
export const openedMessage = '{"blitz":"opened"}';

/**
 * Relays until signal aborts; a stream that ends (a service restart) is
 * watched again after retry ms.
 */
export async function runRelay(ends: RelayEnds, signal: AbortSignal, retry = 2000): Promise<void> {
  const mine = new Set<string>();
  // Each view's replies go one after another, so the engine reads them in order.
  const queues = new Map<string, Promise<unknown>>();
  const reply = (browserId: string, messages: string[], closed = false, reason = "") => {
    const next = (queues.get(browserId) ?? Promise.resolve())
      .then(() => ends.reply({ browserId, messages, closed, reason }))
      .catch(() => undefined);
    queues.set(browserId, next);
    if (closed) void next.then(() => queues.delete(browserId));
    return next;
  };
  const offMessage = ends.on("agentbrowser:message", (arg) => {
    const { id, message } = arg as { id: string; message: string };
    if (mine.has(id)) void reply(id, [message]);
  });
  const offClosed = ends.on("agentbrowser:closed", (arg) => {
    const { id, reason } = arg as { id: string; reason: string };
    if (mine.delete(id)) void reply(id, [], true, reason);
  });
  try {
    while (!signal.aborted) {
      try {
        for await (const r of ends.watch(signal)) {
          const c = r.command;
          if (!c) continue;
          if (c.kind === "open") {
            try {
              await ends.open(c.browserId, c.show, c.profile, c.allowLocal, c.denyDomains);
              mine.add(c.browserId);
              if (c.show === "watch") ends.showTab(c.browserId);
              void reply(c.browserId, [openedMessage]);
            } catch (e) {
              void reply(c.browserId, [], true, e instanceof Error ? e.message : String(e));
            }
          } else if (c.kind === "message") {
            void ends.send(c.browserId, c.message).catch((e: unknown) => {
              if (mine.delete(c.browserId)) void reply(c.browserId, [], true, e instanceof Error ? e.message : String(e));
            });
          } else if (c.kind === "close") {
            mine.delete(c.browserId);
            void ends.close(c.browserId).catch(() => undefined);
          }
        }
      } catch {
        // the service went away: watch again
      }
      if (signal.aborted) break;
      await new Promise((r) => setTimeout(r, retry));
    }
  } finally {
    offMessage();
    offClosed();
    for (const id of mine) void ends.close(id).catch(() => undefined);
  }
}
