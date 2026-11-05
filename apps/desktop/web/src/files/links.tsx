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

// Paths in the conversation that open the editor (spec_029_files FIL-35).
// The workspace provides which files exist (Go to file's list), so only
// real files become links.
import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { files } from "../api";
import { openFile } from "../events";
import { parseFileRef, type FileRef } from "./paths";

/** A workspace and the files in it, for the paths in its conversation. */
export interface FileLinks {
  dir: string;
  known: ReadonlySet<string>;
}

const Context = createContext<FileLinks | null>(null);

/**
 * Provides a workspace's files to the paths in its conversation; refresh
 * changes when files may have.
 */
export function FileLinksProvider({ dir, refresh, children }: { dir: string; refresh: number; children: ReactNode }) {
  const [known, setKnown] = useState<ReadonlySet<string>>(new Set());
  const last = useRef({ dir: "", at: 0 });
  const shown = useRef(dir);
  shown.current = dir;
  useEffect(() => {
    // Again for the same workspace at most every listEvery: the list can
    // be long. A refresh sooner waits for then, so a file made in a turn
    // still becomes a link.
    const timer = setTimeout(() => {
      last.current = { dir, at: Date.now() };
      files.findFiles({ workspace: dir, query: "", limit: 20000 }).then(
        (r) => shown.current === dir && setKnown(new Set(r.paths)),
        () => {},
      );
    }, listingDelay(last.current, dir, Date.now()));
    return () => clearTimeout(timer);
  }, [dir, refresh]);
  return <Context.Provider value={{ dir, known }}>{children}</Context.Provider>;
}

/** How often a workspace's files are listed again, at most. */
export const listEvery = 10_000;

/** How long a listing of dir waits, the last having been of last.dir at last.at: none for another workspace, else until listEvery has passed. */
export function listingDelay(last: { dir: string; at: number }, dir: string, now: number): number {
  return last.dir === dir ? Math.max(0, last.at + listEvery - now) : 0;
}

/** The workspace whose conversation this is and the files it has, if any. */
export function useFileLinks(): FileLinks | null {
  return useContext(Context);
}

/** The workspace file text names, if it names one that exists. */
export function useFileRef(text: string): (FileRef & { open: () => void }) | null {
  const links = useContext(Context);
  if (!links) return null;
  const ref = parseFileRef(text, links.dir);
  if (!ref || !links.known.has(ref.path)) return null;
  return { ...ref, open: () => openFile({ dir: links.dir, ...ref }) };
}

/** Opens a path from a tool's arguments (it may not exist yet: the editor says so). */
export function useOpenPath(): ((path: string) => void) | null {
  const links = useContext(Context);
  if (!links) return null;
  return (path: string) => {
    const ref = parseFileRef(path, links.dir) ?? { path };
    openFile({ dir: links.dir, ...ref });
  };
}
