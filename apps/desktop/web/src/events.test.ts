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

import { beforeAll, describe, expect, it, vi } from "vitest";
import { changesAfter, compose, composeEvent, showView, startSetup, takePendingCompose, takePendingView, viewEvent, type ComposeDetail, type ViewDetail } from "./events";

// Tests run without a DOM: the window is an event target.
beforeAll(() => {
  vi.stubGlobal("window", new EventTarget());
});

describe("startSetup", () => {
  it("shows the chat, then runs /setup there in a new chat", () => {
    const seen: string[] = [];
    const view = (e: Event) => seen.push(`view ${(e as CustomEvent<ViewDetail>).detail.view}`);
    const run = (e: Event) => {
      const d = (e as CustomEvent<ComposeDetail>).detail;
      seen.push(`${d.text} run=${d.run} fresh=${d.fresh}`);
      e.preventDefault();
    };
    window.addEventListener(viewEvent, view);
    window.addEventListener(composeEvent, run);
    startSetup("/s");
    window.removeEventListener(viewEvent, view);
    window.removeEventListener(composeEvent, run);
    expect(seen).toEqual(["view chat", "/setup run=true fresh=true"]);
    expect(takePendingCompose("/s")).toBeUndefined();
  });
});

describe("showView", () => {
  it("goes to the workspace that takes it, or waits for it", () => {
    const take = (e: Event) => (e as CustomEvent<ViewDetail>).detail.dir === "/open" && e.preventDefault();
    window.addEventListener(viewEvent, take);
    showView({ dir: "/open", view: "workers" });
    showView({ dir: "/opening", view: "workers" });
    window.removeEventListener(viewEvent, take);
    expect(takePendingView("/open")).toBeUndefined();
    expect(takePendingView("/opening")).toEqual({ dir: "/opening", view: "workers" });
    expect(takePendingView("/opening")).toBeUndefined();
  });
});

describe("compose", () => {
  it("waits for a composer that isn't there yet", () => {
    compose({ dir: "/w", text: "fix it" });
    expect(takePendingCompose("/w")?.text).toBe("fix it");
    expect(takePendingCompose("/w")).toBeUndefined();
  });

  it("goes to a composer that takes it", () => {
    const f = (e: Event) => {
      if ((e as CustomEvent<ComposeDetail>).detail.dir === "/v") e.preventDefault();
    };
    window.addEventListener(composeEvent, f);
    compose({ dir: "/v", text: "hi" });
    window.removeEventListener(composeEvent, f);
    expect(takePendingCompose("/v")).toBeUndefined();
  });

  it("doesn't keep a command to run", () => {
    compose({ dir: "/u", text: "/help", run: true });
    expect(takePendingCompose("/u")).toBeUndefined();
  });
});

describe("changesAfter", () => {
  const history = { kind: "history", path: "a.go" };
  it.each([
    ["opens the changes", null, false, { kind: "changes", source: "git" }],
    ["a toggle opens them when closed", null, true, { kind: "changes", source: "git" }],
    ["a toggle closes them when shown", { kind: "changes", source: "git" }, true, null],
    ["a plain request keeps them shown", { kind: "changes", source: "session" }, false, { kind: "changes", source: "git" }],
    ["a toggle over the history shows the changes", history, true, { kind: "changes", source: "git" }],
  ])("%s", (_name, cur, toggle, want) => expect(changesAfter(cur, toggle, "git")).toEqual(want));
});
