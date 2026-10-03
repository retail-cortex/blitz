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

import { useEffect, useMemo, useRef, useState } from "react";
import { mdiFileDocumentOutline, mdiHistory, mdiMagnify, mdiNoteTextOutline, mdiRefresh } from "@mdi/js";
import { workspaces } from "./api";
import { message } from "./errors";
import { loadSession, openFile, showView } from "./events";
import { fileIcon } from "./files/icons";
import { nameOf } from "./files/tree";
import type { SearchHit, SearchStatus } from "./gen/blitz/v1/workspace_pb";
import { t } from "./i18n";
import { useApp } from "./state";
import { defaultSources, findTerms, hitTarget, markTerms, queryTerms, searchSources, toggleSource, type SearchSource } from "./search";
import { Chip, Icon, IconButton, Segmented, useModal } from "./ui/controls";

/**
 * Cmd/Ctrl+Shift+F, or the palette: search everything in the workspace
 * (SearchWorkspace as you type), in the sources chosen (files and PDFs to
 * start). Each hit shows where it is and its passage, the words marked;
 * Enter or a click opens it: a file at its line, a chat, a plan.
 */
export function SearchDialog({ dir, initialQuery = "", onClose }: { dir: string; initialQuery?: string; onClose: () => void }) {
  const [query, setQuery] = useState(initialQuery);
  // Hidden files (dotfiles, ones git ignores) only when the Files shelf shows them.
  const hidden = useApp().prefs.show_hidden;
  const [sources, setSources] = useState<SearchSource[]>(defaultSources);
  // How to search: by meaning too, once an embedding model is set.
  const [mode, setMode] = useState<"hybrid" | "keyword" | "semantic">("hybrid");
  const [hits, setHits] = useState<SearchHit[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [pick, setPick] = useState(0);
  const [status, setStatus] = useState<SearchStatus>();
  const input = useRef<HTMLInputElement>(null);
  const list = useRef<HTMLDivElement>(null);
  const box = useRef<HTMLDivElement>(null);
  useModal(box, onClose);

  // The index's status, asked again every second while it scans.
  useEffect(() => {
    input.current?.focus();
    let live = true;
    let timer: ReturnType<typeof setTimeout>;
    const ask = () =>
      workspaces.getSearchStatus({ workspace: dir }).then(
        (r) => {
          if (!live) return;
          setStatus(r.status);
          if (r.status?.scanning || !r.status?.lastScan) timer = setTimeout(ask, 1000);
        },
        () => {},
      );
    ask();
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [dir]);

  // The hits for what's typed, once typing pauses.
  const scanned = status?.lastScan;
  const semantic = !!status?.embeddingModel;
  useEffect(() => {
    if (!queryTerms(query).length) {
      setHits([]);
      setError("");
      return;
    }
    let live = true;
    const timer = setTimeout(() => {
      setBusy(true);
      workspaces.searchWorkspace({ workspace: dir, query, sources, limit: 30, mode: semantic ? mode : "", hidden }).then(
        (r) => live && (setHits(r.hits), setError("")),
        (e) => live && (setHits([]), setError(message(e))),
      ).finally(() => live && setBusy(false));
    }, 150);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [dir, query, sources, scanned, semantic, mode, hidden]);
  useEffect(() => setPick(0), [hits]);
  useEffect(() => list.current?.querySelector(".search-hit.on")?.scrollIntoView({ block: "nearest" }), [pick]);

  const terms = useMemo(() => queryTerms(query), [query]);
  const open = (h: SearchHit) => {
    const to = hitTarget(h);
    if (to.kind === "none") return;
    onClose();
    if (to.kind === "chat") {
      showView({ dir, view: "chat" });
      loadSession({ dir, id: to.id });
    } else {
      openFile({ dir, path: to.path, line: to.line, find: findTerms(query) });
    }
  };
  const reindex = () => workspaces.reindex({ workspace: dir }).then(() => setStatus((s) => (s ? { ...s, scanning: true } : s)), (e) => setError(message(e)));

  const items = status?.items ?? {};
  const counts = [
    ...searchSources.map((s) => `${t(`desktop.search.source.${s}`)} ${items[s] ?? 0}`),
    ...(semantic ? [t("desktop.search.embedded", { count: status?.embedded ?? 0 })] : []),
    ...(status?.enriched ? [t("desktop.search.enriched", { count: status.enriched })] : []),
  ].join(" · ");
  return (
    <div className="scrim palette-scrim" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="palette search-dialog" role="dialog" aria-modal="true" aria-label={t("desktop.search.title")} ref={box}>
        <div className="palette-search">
          <Icon path={mdiMagnify} />
          <input
            ref={input}
            value={query}
            placeholder={t("desktop.search.placeholder")}
            aria-label={t("desktop.search.title")}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "ArrowDown" || e.key === "ArrowUp") {
                e.preventDefault();
                if (hits.length) setPick((p) => (p + (e.key === "ArrowDown" ? 1 : hits.length - 1)) % hits.length);
              } else if (e.key === "Enter") {
                e.preventDefault();
                if (hits[pick]) open(hits[pick]);
              }
            }}
          />
        </div>
        <div className="search-sources" role="group" aria-label={t("desktop.search.sources")}>
          {semantic && (
            <Segmented
              small
              label={t("desktop.search.mode")}
              value={mode}
              onChange={(m) => (setMode(m), input.current?.focus())}
              options={[
                { value: "hybrid", label: t("desktop.search.mode.hybrid") },
                { value: "keyword", label: t("desktop.search.mode.keyword") },
                { value: "semantic", label: t("desktop.search.mode.semantic") },
              ]}
            />
          )}
          {searchSources.map((s) => (
            <Chip key={s} selected={sources.includes(s)} aria-pressed={sources.includes(s)} onClick={() => (setSources((on) => toggleSource(on, s)), input.current?.focus())}>
              {t(`desktop.search.source.${s}`)}
            </Chip>
          ))}
        </div>
        <div className="palette-list" ref={list} role="listbox" aria-busy={busy}>
          {error && <p className="palette-empty error-text">{error}</p>}
          {!error && terms.length > 0 && !busy && hits.length === 0 && <p className="palette-empty muted">{t("desktop.search.none")}</p>}
          {!error && terms.length === 0 && <p className="palette-empty muted">{t("desktop.search.hint")}</p>}
          {hits.map((h, i) => {
            const to = hitTarget(h);
            const where = h.source === "chats" ? h.title : h.line && to.kind === "file" && to.line ? `${h.ref}:${h.line}` : h.ref;
            return (
              <button
                key={`${h.source}:${h.ref}`}
                role="option"
                aria-selected={i === pick}
                aria-disabled={to.kind === "none"}
                className={`search-hit ${i === pick ? "on" : ""}`}
                onMouseEnter={() => setPick(i)}
                onClick={() => open(h)}
              >
                <span className="search-hit-head">
                  <Icon path={hitIcon(h)} />
                  <span className="ellipsis">{where}</span>
                  {h.section && <span className="detail muted t-body-sm">{h.section}</span>}
                  <span className="detail muted t-body-sm">{t(`desktop.search.source.${h.source}`)}</span>
                </span>
                {(h.summary || h.tags.length > 0) && (
                  <span className="search-about t-body-sm">
                    {h.summary && <span className="search-summary">{h.summary}</span>}
                    {h.tags.map((tag) => (
                      <span key={tag} className="search-tag">
                        {tag}
                      </span>
                    ))}
                  </span>
                )}
                <pre className="search-snippet t-body-sm">
                  {markTerms(h.snippet, terms).map((p, j) => (p.mark ? <mark key={j}>{p.text}</mark> : p.text))}
                </pre>
              </button>
            );
          })}
        </div>
        <div className="palette-foot search-foot t-body-sm muted">
          <span className="ellipsis" title={status?.embedError || undefined}>
            {status?.enabled === false ? t("desktop.search.off") : status?.scanning || !status?.lastScan ? t("desktop.search.scanning") : status?.embedError ? `${counts} · ${t("desktop.search.embed_error")}` : counts}
          </span>
          {status?.enabled !== false && <IconButton icon={mdiRefresh} label={t("desktop.search.reindex")} small disabled={status?.scanning} onClick={reindex} />}
        </div>
      </div>
    </div>
  );
}

/** A hit's icon: its file's, a PDF's or notebook's, a chat's, a note's. */
function hitIcon(h: SearchHit): string {
  switch (h.source) {
    case "chats":
      return mdiHistory;
    case "notes":
      return mdiNoteTextOutline;
    case "documents":
      return mdiFileDocumentOutline;
  }
  return fileIcon(nameOf(h.ref));
}
