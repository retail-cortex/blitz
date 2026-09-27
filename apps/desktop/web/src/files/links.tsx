// Paths in the conversation that open the editor (spec_files_029 FIL-35).
// The workspace provides which files exist (Go to file's list), so only
// real files become links.
import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { files } from "../api";
import { openFile } from "../events";
import { parseFileRef, type FileRef } from "./paths";

interface FileLinks {
  dir: string;
  known: ReadonlySet<string>;
}

const Context = createContext<FileLinks | null>(null);

/** Provides a workspace's files to the paths in its conversation; refresh changes when files may have. */
export function FileLinksProvider({ dir, refresh, children }: { dir: string; refresh: number; children: ReactNode }) {
  const [known, setKnown] = useState<ReadonlySet<string>>(new Set());
  const last = useRef({ dir: "", at: 0 });
  useEffect(() => {
    // Again for the same workspace at most every 10 seconds: the list can be long.
    if (last.current.dir === dir && Date.now() - last.current.at < 10_000) return;
    last.current = { dir, at: Date.now() };
    files.findFiles({ workspace: dir, query: "", limit: 20000 }).then(
      (r) => setKnown(new Set(r.paths)),
      () => {},
    );
  }, [dir, refresh]);
  return <Context.Provider value={{ dir, known }}>{children}</Context.Provider>;
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
