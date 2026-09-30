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
import { compose, composeEvent, takePendingCompose, type ComposeDetail } from "./events";

// Tests run without a DOM: the window is an event target.
beforeAll(() => {
  vi.stubGlobal("window", new EventTarget());
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
