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

import { Window } from "happy-dom";
import { act } from "react";
import type { Root } from "react-dom/client";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";

// The workspace's files, as FileService lists them: folders by path.
const fs = vi.hoisted(() => {
  const FOLDER = 2; // FileKind.FOLDER, checked below
  const FILE = 1;
  const tree = new Map<string, { name: string; kind: number }[]>();
  const renames: { from: string; to: string }[] = [];
  const entry = (folder: string, e: { name: string; kind: number }) => ({ ...e, path: folder ? `${folder}/${e.name}` : e.name, git: "", hidden: false, agentRule: "", staged: "" });
  return {
    FOLDER,
    FILE,
    tree,
    renames,
    reset() {
      tree.clear();
      renames.length = 0;
      tree.set("", [
        { name: "a", kind: FOLDER },
        { name: "x.md", kind: FILE },
      ]);
      tree.set("a", [{ name: "b", kind: FOLDER }]);
      tree.set("a/b", []);
    },
    files: {
      listDir: async ({ path }: { path: string }) => ({ entries: (tree.get(path) ?? []).map((e) => entry(path, e)), truncated: false, repository: false }),
      renameFile: async ({ from, to }: { from: string; to: string }) => {
        renames.push({ from, to });
        const name = (p: string) => p.slice(p.lastIndexOf("/") + 1);
        const parent = (p: string) => (p.includes("/") ? p.slice(0, p.lastIndexOf("/")) : "");
        const moved = tree.get(parent(from))!.find((e) => e.name === name(from))!;
        tree.set(parent(from), tree.get(parent(from))!.filter((e) => e !== moved));
        tree.set(parent(to), [...(tree.get(parent(to)) ?? []), { ...moved, name: name(to) }]);
        return {};
      },
    },
  };
});

vi.mock("../api", () => ({ files: fs.files }));
vi.mock("./watch", () => ({ watchFolders: () => () => {} }));
vi.mock("./language", async (orig) => ({ ...(await orig<typeof import("./language")>()), languageSession: () => ({ worstProblems: () => new Map(), subscribeAll: () => () => {} }) }));
vi.mock("../state", async (orig) => {
  const { defaultPrefs } = await import("../prefs");
  const app = { prefs: { ...defaultPrefs, advanced: true, files_compact: "on" }, update: () => {} };
  return { ...(await orig<typeof import("../state")>()), useApp: () => app, useAdvanced: () => true };
});

const win = new Window({ url: "http://localhost/" });
win.document.write("<!doctype html><html><head></head><body></body></html>");
let FilesShelf: typeof import("./FilesShelf").FilesShelf;
let createRoot: typeof import("react-dom/client").createRoot;
beforeAll(async () => {
  const w = win as unknown as Record<string, unknown>;
  for (const name of ["window", "document", "navigator", "Node", "Element", "HTMLElement", "HTMLButtonElement", "Text", "DocumentFragment", "MutationObserver", "ResizeObserver", "Event", "MouseEvent", "KeyboardEvent", "DragEvent", "DataTransfer", "CustomEvent", "FocusEvent", "CSS", "getComputedStyle"]) {
    const v = w[name];
    vi.stubGlobal(name, typeof v === "function" && /^[a-z]/.test(name) ? (v as (...a: unknown[]) => unknown).bind(win) : v);
  }
  vi.stubGlobal("requestAnimationFrame", (f: () => void) => setTimeout(f, 0));
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  ({ FilesShelf } = await import("./FilesShelf"));
  const { FileKind } = await import("../gen/blitz/v1/file_pb");
  expect([FileKind.FILE, FileKind.FOLDER]).toEqual([fs.FILE, fs.FOLDER]);
  ({ createRoot } = await import("react-dom/client"));
}, 60_000);
afterAll(() => {
  vi.unstubAllGlobals();
  void win.happyDOM.close();
});

let root: Root | undefined;
let host: HTMLDivElement | undefined;
afterEach(() => {
  act(() => root?.unmount());
  host?.remove();
  root = host = undefined;
});

// Lets the shelf's listings land.
const settle = () => act(() => new Promise<void>((r) => setTimeout(r, 20)));

async function mount() {
  fs.reset();
  host = document.body.appendChild(document.createElement("div"));
  root = createRoot(host);
  const noop = () => {};
  act(() => root!.render(<FilesShelf dir="/w" width={260} onResize={noop} showHidden={false} onToggleHidden={noop} active={null} refresh={0} reveal={null} onOpen={noop} onMoved={noop} onChanged={noop} onClose={noop} />));
  await settle();
}

const row = (path: string) => host!.querySelector<HTMLElement>(`[data-path="${path}"]`);

// A drag of a row onto target, as the browser sends it.
async function drag(from: HTMLElement, to: HTMLElement) {
  const data = new DataTransfer();
  const send = (type: string, target: HTMLElement) => {
    const ev = new DragEvent(type, { bubbles: true, cancelable: true });
    Object.defineProperty(ev, "dataTransfer", { value: data }); // happy-dom doesn't take it from the init
    return target.dispatchEvent(ev);
  };
  act(() => void send("dragstart", from));
  act(() => void send("dragover", to));
  act(() => void send("drop", to));
  await settle();
}

// A folder holding only a folder shows on one row ("a / b", FIL-47); each
// of its folders is still a folder to select and drop into (#110).
describe("FilesShelf's compacted folders", () => {
  it("takes a drop into the chain's first folder by its name", async () => {
    await mount();
    act(() => row("a")!.click());
    await settle();
    // The chain's row is b's, a is one of its names.
    expect(row("a")).toBeNull();
    const part = host!.querySelector<HTMLElement>('[data-folder="a"]');
    expect(part?.textContent).toBe("a");
    expect(row("a/b")?.textContent).toContain("a/b");

    await drag(row("x.md")!, part!);
    expect(fs.renames).toEqual([{ from: "x.md", to: "a/x.md" }]);
    // a holds a file now: its own row again, open, with b and x.md in it.
    expect(row("a")?.getAttribute("aria-expanded")).toBe("true");
    expect(row("a/x.md")).not.toBeNull();
  });

  it("still drops on the chain's last folder by the rest of the row", async () => {
    await mount();
    act(() => row("a")!.click());
    await settle();
    await drag(row("x.md")!, row("a/b")!);
    expect(fs.renames).toEqual([{ from: "x.md", to: "a/b/x.md" }]);
  });

  it("selects the chain's first folder by its name", async () => {
    await mount();
    act(() => row("a")!.click());
    await settle();
    const part = host!.querySelector<HTMLElement>('[data-folder="a"]')!;
    act(() => part.click());
    expect(part.classList.contains("selected")).toBe(true);
    expect(row("a/b")?.classList.contains("selected")).toBe(false);
    expect(row("a/b")?.getAttribute("aria-expanded")).toBe("true"); // a click on a name doesn't close the chain
  });
});
