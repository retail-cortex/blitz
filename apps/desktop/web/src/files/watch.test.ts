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

import { createRouterTransport } from "@connectrpc/connect";
import { describe, expect, it, vi } from "vitest";
import { setTransport } from "../api";
import { FileService, type WatchFilesResponse } from "../gen/blitz/v1/file_pb";
import { watchFolders } from "./watch";

type Msg = Pick<WatchFilesResponse, "folders" | "polling">;

// A service whose WatchFiles streams each call's messages in turn, then
// ends (or, for the last call, stays open until stopped).
function service(calls: Msg[][]) {
  const asked: { workspace: string; folders: string[] }[] = [];
  setTransport(
    createRouterTransport(({ service }) => {
      service(FileService, {
        async *watchFiles(r, ctx) {
          const n = asked.push({ workspace: r.workspace, folders: [...r.folders] }) - 1;
          for (const m of calls[n] ?? []) yield m;
          if (n >= calls.length - 1) await new Promise<void>((done) => ctx.signal.addEventListener("abort", () => done()));
        },
      });
    }),
  );
  return asked;
}

describe("watching the tree's folders (FIL-33b)", () => {
  it("lists the folders that change, live while the service watches them all", async () => {
    const asked = service([[{ folders: [], polling: false }, { folders: ["src"], polling: false }, { folders: [], polling: true }]]);
    const changed: string[][] = [];
    const live: boolean[] = [];
    const stop = watchFolders("/w/shop", ["", "src"], { changed: (f) => changed.push(f), live: (on) => live.push(on) });
    await vi.waitFor(() => expect(live).toEqual([true, true, false]));
    expect(changed).toEqual([["src"]]);
    expect(asked).toEqual([{ workspace: "/w/shop", folders: ["", "src"] }]);
    stop();
  });

  it("asks again after the stream breaks, not live meanwhile, and stops when told", async () => {
    const asked = service([[{ folders: [], polling: false }], [{ folders: ["docs"], polling: false }]]);
    const changed: string[][] = [];
    const live: boolean[] = [];
    const stop = watchFolders("/w/shop", [""], { changed: (f) => changed.push(f), live: (on) => live.push(on) }, 5);
    await vi.waitFor(() => expect(changed).toEqual([["docs"]]));
    expect(live).toEqual([true, false, true]);
    expect(asked).toHaveLength(2);
    stop();
    await new Promise((r) => setTimeout(r, 20));
    expect(asked).toHaveLength(2); // stopped: not asked again
  });

  it("isn't live while the service is away", async () => {
    setTransport(
      createRouterTransport(({ service }) => {
        service(FileService, {
          async *watchFiles() {
            throw new Error("the service restarted");
          },
        });
      }),
    );
    const live: boolean[] = [];
    const stop = watchFolders("/w/shop", [""], { changed: () => {}, live: (on) => live.push(on) }, 5);
    await vi.waitFor(() => expect(live.length).toBeGreaterThanOrEqual(2));
    expect(live.every((on) => !on)).toBe(true);
    stop();
  });
});
