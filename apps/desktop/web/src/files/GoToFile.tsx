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

import { useEffect, useRef, useState } from "react";
import { mdiFileSearchOutline } from "@mdi/js";
import { files } from "../api";
import { message } from "../errors";
import { t } from "../i18n";
import { Icon, useModal } from "../ui/controls";
import { fileIcon } from "./icons";
import { splitLine } from "./paths";
import { nameOf, parentOf } from "./tree";

/**
 * Go to file (⌘P, spec_files_029 FIL-34): the workspace's files matching
 * what's typed, best first; "name:12" opens at line 12.
 */
export function GoToFile({ dir, onOpen, onClose }: { dir: string; onOpen: (path: string, line?: number, column?: number) => void; onClose: () => void }) {
  const [text, setText] = useState("");
  const [paths, setPaths] = useState<string[]>([]);
  const [index, setIndex] = useState(0);
  const [error, setError] = useState("");
  const input = useRef<HTMLInputElement>(null);
  const { query, line, column } = splitLine(text);

  // Escape, the focus kept inside and given back: as in a dialog.
  const box = useRef<HTMLDivElement>(null);
  useModal(box, onClose);
  useEffect(() => {
    let live = true;
    const timer = setTimeout(() => {
      files.findFiles({ workspace: dir, query, limit: 50 }).then(
        (r) => {
          if (!live) return;
          setPaths(r.paths);
          setIndex(0);
          setError("");
        },
        (e) => live && setError(message(e)),
      );
    }, 60);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [dir, query]);

  const pick = (p: string | undefined) => {
    if (!p) return;
    onClose();
    onOpen(p, line, column);
  };

  return (
    <div className="scrim palette-scrim" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="palette" role="dialog" aria-modal="true" aria-label={t("desktop.files.go_to")} ref={box}>
        <div className="palette-search">
          <Icon path={mdiFileSearchOutline} />
          <input
            ref={input}
            value={text}
            placeholder={t("desktop.files.go_to_placeholder")}
            aria-label={t("desktop.files.go_to")}
            spellCheck={false}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "ArrowDown") setIndex((i) => Math.min(i + 1, paths.length - 1));
              else if (e.key === "ArrowUp") setIndex((i) => Math.max(i - 1, 0));
              else if (e.key === "Enter") pick(paths[index]);
              else return;
              e.preventDefault();
            }}
          />
        </div>
        <div className="palette-list" role="listbox">
          {error && <p className="error-text t-body-sm">{error}</p>}
          {!error && paths.length === 0 && <p className="muted t-body-sm palette-empty">{t("desktop.files.no_match")}</p>}
          {paths.map((p, i) => (
            <button key={p} role="option" aria-selected={i === index} className={`palette-item ${i === index ? "on" : ""}`} onMouseEnter={() => setIndex(i)} onClick={() => pick(p)}>
              <Icon path={fileIcon(nameOf(p))} size="sm" />
              <span className="ellipsis">{nameOf(p)}</span>
              <small className="detail muted ellipsis mono">{parentOf(p)}</small>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
