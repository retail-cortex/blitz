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
import { mdiChevronDown, mdiChevronUp, mdiClose, mdiMagnify } from "@mdi/js";
import { t } from "../i18n";
import { Icon, IconButton } from "../ui/controls";
import { findMarks, findRanges, hitMarks, paint, unpaint } from "./find";

/**
 * Find in file for a document's preview (FIL-72): the text typed, found
 * anywhere, whatever its case; Enter goes to the next, Shift+Enter to the
 * one before, Escape closes. box finds the rendered document; text is
 * what it shows, so a change finds again; a new focus (asked again) puts
 * the cursor back in the query, all of it selected.
 */
export function FindBar({ box, text, focus, onClose }: { box: () => HTMLElement | null; text: string; focus: number; onClose: () => void }) {
  const [query, setQuery] = useState("");
  const [at, setAt] = useState(0);
  const [count, setCount] = useState(0);
  const [found, setFound] = useState(0); // each search, one more: marks again
  const ranges = useRef<Range[]>([]);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    input.current?.focus();
    input.current?.select();
  }, [focus]);

  // Find again when the query or the document changes, from the first.
  useEffect(() => {
    const b = box();
    const q = query.trim().toLowerCase();
    ranges.current = b && q ? findRanges(b, [q], false) : [];
    setCount(ranges.current.length);
    setAt(0);
    setFound((n) => n + 1);
  }, [query, text, box]);
  useEffect(() => {
    if (!ranges.current.length) return unpaint(findMarks);
    paint(findMarks, ranges.current, ranges.current[Math.min(at, ranges.current.length - 1)]);
  }, [at, found]);
  useEffect(() => {
    unpaint(hitMarks); // a search hit's marks give way
    return () => unpaint(findMarks);
  }, []);

  const step = (by: number) => count && setAt((i) => (i + by + count) % count);
  return (
    <div className="find-bar" role="search">
      <Icon path={mdiMagnify} size="sm" />
      <input
        ref={input}
        value={query}
        aria-label={t("desktop.files.find")}
        placeholder={t("desktop.files.find")}
        spellCheck={false}
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            step(e.shiftKey ? -1 : 1);
          } else if (e.key === "Escape") {
            e.preventDefault();
            e.stopPropagation();
            onClose();
          }
        }}
      />
      <span className="find-count muted" aria-live="polite">
        {query.trim() ? (count ? t("desktop.files.find_count", { n: at + 1, total: count }) : t("desktop.files.find_none")) : ""}
      </span>
      <IconButton icon={mdiChevronUp} label={t("desktop.files.find_previous")} small disabled={!count} onClick={() => step(-1)} />
      <IconButton icon={mdiChevronDown} label={t("desktop.files.find_next")} small disabled={!count} onClick={() => step(1)} />
      <IconButton icon={mdiClose} label={t("desktop.close")} small onClick={onClose} />
    </div>
  );
}
