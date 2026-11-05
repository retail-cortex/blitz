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
import { Window } from "happy-dom";
import { act, createElement } from "react";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { setTransport } from "../api";
import { FileService } from "../gen/blitz/v1/file_pb";
import { LanguageService, ServerState } from "../gen/blitz/v1/language_pb";
import type { EditorModel } from "./useEditor";
import { openedPreviews } from "./webTabs";

const closedPreviews: string[] = [];
vi.mock("../desktop", async (actual) => ({ ...(await actual<typeof import("../desktop")>()), previewClose: async (id: string) => void closedPreviews.push(id) }));

// A DOM for React and CodeMirror, set before they load (they look once).
type Loaded = { createRoot: typeof import("react-dom/client").createRoot; useEditor: typeof import("./useEditor").useEditor; languageSession: typeof import("./language").languageSession };
let m: Loaded;
beforeAll(async () => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  const g = globalThis as { window?: unknown; document?: unknown };
  const win = new Window();
  g.window ??= win;
  g.document ??= win.document;
  const [{ createRoot }, { useEditor }, { languageSession }] = await Promise.all([import("react-dom/client"), import("./useEditor"), import("./language")]);
  m = { createRoot, useEditor, languageSession };
});
afterEach(() => void (closedPreviews.length = 0));

// The service's files (path → text and version), and the reads asked for.
function fakeService(disk: Record<string, { text: string; version: string }>) {
  const calls: string[] = [];
  let hold: Promise<void> | undefined;
  setTransport(
    createRouterTransport(({ service }) => {
      service(FileService, {
        readFile: async (r) => {
          await hold;
          return { path: r.path, ...disk[r.path] };
        },
        statFiles: (r) => ({ versions: Object.fromEntries(r.paths.map((p) => [p, disk[p]?.version ?? ""])) }),
      });
      service(LanguageService, {
        openDocument: (r) => (calls.push(`open ${r.path}`), { document: `doc:${r.path}`, language: "go", state: ServerState.READY }),
        closeDocument: (r) => (calls.push(`close ${r.document}`), {}),
        keepDocuments: () => ({ documents: [] }),
        async *watchDiagnostics() {},
      });
    }),
  );
  return { calls, holdReads: (p: Promise<void>) => (hold = p) };
}

// Mounts a workspace's editor; model is the latest.
function mount(dir: string) {
  const ref: { model?: EditorModel } = {};
  const Host = () => {
    ref.model = m.useEditor(dir);
    return null;
  };
  const root = m.createRoot((globalThis as unknown as { document: Document }).document.createElement("div"));
  act(() => root.render(createElement(Host)));
  return { ref, unmount: () => act(() => root.unmount()) };
}

describe("a workspace's editor", () => {
  it("closes its language documents and web pages when the workspace closes", async () => {
    const f = fakeService({});
    const ed = mount("/w/shop");
    act(() => void ed.ref.model!.openWeb("http://localhost:3000/"));
    const web = ed.ref.model!.tabs.find((t) => t.web)!.path;
    openedPreviews.add(web.slice("web:".length)); // as its preview does
    await m.languageSession("/w/shop").open("main.go", "package main\n");
    ed.unmount();
    await vi.waitFor(() => expect(f.calls).toEqual(["open main.go", "close doc:main.go"]));
    expect(closedPreviews).toEqual([web.slice("web:".length)]);
    expect(openedPreviews.has(web.slice("web:".length))).toBe(false);
    expect(m.languageSession("/w/shop").status("main.go")).toBeUndefined(); // a new session: nothing kept
  });

  it.each([
    { name: "a clean tab takes the file's new text", type: false, text: "two\n", disk: undefined },
    { name: "keystrokes during the read are kept, the change shown", type: true, text: "one typed\n", disk: "changed" },
  ])("reloads a file changed on disk: $name", async ({ type, text, disk }) => {
    const files = { "a.txt": { text: "one\n", version: "v1" } };
    const f = fakeService(files);
    const ed = mount("/w/notes");
    try {
      act(() => ed.ref.model!.open("a.txt"));
      await vi.waitFor(() => expect(ed.ref.model!.tabs[0]?.loading).toBe(false));
      expect(ed.ref.model!.state("a.txt")?.doc.toString()).toBe("one\n");

      // The agent rewrites it; the window looks while the user types.
      files["a.txt"] = { text: "two\n", version: "v2" };
      let release!: () => void;
      f.holdReads(new Promise<void>((r) => (release = r)));
      await act(async () => void ed.ref.model!.check());
      if (type) {
        const s = ed.ref.model!.state("a.txt")!;
        ed.ref.model!.keepState("a.txt", s.update({ changes: { from: 3, insert: " typed" } }).state);
      }
      await act(async () => release());
      await vi.waitFor(() => expect(ed.ref.model!.tabs[0].version).toBe(type ? "v1" : "v2"));
      await vi.waitFor(() => expect(ed.ref.model!.tabs[0].disk).toBe(disk));
      expect(ed.ref.model!.state("a.txt")?.doc.toString()).toBe(text);
    } finally {
      ed.unmount();
    }
  });
});
