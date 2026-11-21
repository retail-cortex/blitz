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

import type { MutableRefObject } from "react";
import type { FileEntry } from "../gen/blitz/v1/file_pb";
import type { Naming } from "./shelfTypes";
import { isFolder, nextMatch, parentOf, rangeOf, type Row, type Tree } from "./tree";

/** Dependencies needed to handle keyboard actions in the Files shelf tree. */
export interface ShelfKeyContext {
  naming: Naming | null;
  shown: Row[];
  selected: string | null;
  picked: ReadonlySet<string>;
  clip: { paths: string[]; cut: boolean } | null;
  tree: Tree;
  anchor: MutableRefObject<string | null>;
  list: MutableRefObject<HTMLDivElement | null>;
  typed: MutableRefObject<{ text: string; at: number }>;
  selection: () => string[];
  setSelected: (path: string | null) => void;
  setLead: (path: string | null) => void;
  setPicked: (picked: ReadonlySet<string>) => void;
  setNaming: (naming: Naming | null) => void;
  setClip: (clip: { paths: string[]; cut: boolean } | null) => void;
  toggle: (entry: FileEntry, open?: boolean) => Promise<void>;
  onOpen: (path: string) => void;
  remove: (paths: string[]) => Promise<void>;
  paste: (into?: string) => Promise<void>;
  duplicate: (paths: string[]) => Promise<void>;
}

/**
 * Handles keyboard navigation, multi-selection, clipboard actions, deletion,
 * renaming, and type-to-find in the Files shelf tree.
 */
export function handleShelfKeyDown(ev: React.KeyboardEvent, ctx: ShelfKeyContext): void {
  const { naming, shown, selected, picked, clip, tree, anchor, list, typed, selection, setSelected, setLead, setPicked, setNaming, setClip, toggle, onOpen, remove, paste, duplicate } = ctx;
  if (naming || (ev.target as HTMLElement).tagName === "INPUT") return;
  const i = shown.findIndex((r) => r.entry.path === selected || r.chain?.some((c) => c.path === selected));
  const cur = shown[i];
  // A compacted chain's folder selected by its name (#110), else the row's.
  const row = cur?.chain?.find((c) => c.path === selected) ?? cur?.entry;
  const go = (j: number, extend = false) => {
    const r = shown[Math.max(0, Math.min(shown.length - 1, j))];
    if (!r) return;
    if (extend) {
      const from = anchor.current ?? selected ?? r.entry.path;
      setLead(r.entry.path);
      setPicked(new Set(rangeOf(shown, from, r.entry.path)));
      anchor.current = from;
    } else setSelected(r.entry.path);
    list.current?.querySelector<HTMLElement>(`[data-path="${CSS.escape(r.entry.path)}"]`)?.focus();
  };
  const mod = ev.metaKey || ev.ctrlKey;
  const opens = (r: Row | undefined) => !!r && (isFolder(r.entry) || !!r.nests);
  switch (true) {
    case ev.key === "ArrowDown":
      go(i + 1, ev.shiftKey);
      break;
    case ev.key === "ArrowUp":
      go(i - 1, ev.shiftKey);
      break;
    case ev.key === "ArrowRight":
      if (cur && opens(cur)) void toggle(cur.entry, true);
      break;
    case ev.key === "ArrowLeft": {
      if (cur && opens(cur) && tree.expanded.has(cur.entry.path)) void toggle(cur.entry, false);
      else if (cur) {
        // Up to the row it's under: a compacted chain's parent is the chain's first's.
        const up = parentOf((cur.chain?.[0] ?? cur.entry).path);
        const j = shown.findIndex((r) => r.entry.path === up || r.chain?.some((c) => c.path === up));
        const nester = shown.slice(0, i).reverse().find((r) => r.nests && r.depth === cur.depth - 1);
        if (j >= 0) go(j);
        else if (nester) go(shown.indexOf(nester));
      }
      break;
    }
    case ev.key === "Enter":
      if (row) (isFolder(row) ? void toggle(row) : onOpen(row.path));
      break;
    case ev.key === "F2":
      if (row) setNaming({ kind: "rename", path: row.path });
      break;
    case ev.key === "Delete" || (ev.key === "Backspace" && ev.metaKey):
      if (selection().length > 0) void remove(selection());
      break;
    case mod && ev.key.toLowerCase() === "a":
      if (shown.length === 0) return;
      setPicked(new Set(shown.map((r) => r.entry.path)));
      if (!selected) setLead(shown[0].entry.path);
      break;
    case mod && ev.key.toLowerCase() === "c" && !ev.shiftKey:
      if (selection().length === 0) return;
      setClip({ paths: selection(), cut: false });
      break;
    case mod && ev.key.toLowerCase() === "x":
      if (selection().length === 0) return;
      setClip({ paths: selection(), cut: true });
      break;
    case mod && ev.key.toLowerCase() === "v":
      if (!clip) return;
      void paste();
      break;
    case mod && ev.key.toLowerCase() === "d":
      if (selection().length === 0) return;
      void duplicate(selection());
      break;
    case ev.key === "Escape":
      if (clip) setClip(null);
      else if (selected || picked.size > 0) setSelected(null);
      else return;
      break;
    case ev.key.length === 1 && !mod && !ev.altKey && ev.key !== " ": {
      const now = Date.now();
      typed.current = { text: (now - typed.current.at < 800 ? typed.current.text : "") + ev.key, at: now };
      // One letter again steps to the next match; more letters refine from here.
      const from = typed.current.text.length === 1 ? i : i - 1;
      const j = nextMatch(shown, from, typed.current.text);
      if (j >= 0) go(j);
      break;
    }
    default:
      return;
  }
  ev.preventDefault();
}
