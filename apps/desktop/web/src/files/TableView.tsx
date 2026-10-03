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
import { mdiArrowDown, mdiArrowUp, mdiChevronLeft, mdiChevronRight, mdiFilterRemoveOutline, mdiMagnify } from "@mdi/js";
import { files } from "../api";
import { message } from "../errors";
import type { ReadTableResponse } from "../gen/blitz/v1/file_pb";
import { t } from "../i18n";
import { Icon, IconButton } from "../ui/controls";
import { activeFilters, nextSort, offsetFor, pageRange, pageSize, rowOfLine, type Sort } from "./table";

/**
 * A CSV or TSV file as a table (FIL-70): read a page at a time by the
 * engine (ReadTable), so a file of any size opens; a search across its
 * columns, a filter under each column (">5", "10..20", "=north", "empty"
 * or text), a sort by a header's click, and pages. A column says its kind
 * and, once a model described the file, what it holds. Opened at a line (a
 * search hit), it shows that row's page with the row marked.
 */
export function TableView({ dir, path, line }: { dir: string; path: string; line?: number }) {
  const focus = rowOfLine(line);
  const [search, setSearch] = useState("");
  const [filters, setFilters] = useState<Record<number, string>>({});
  const [sort, setSort] = useState<Sort | null>(null);
  const [offset, setOffset] = useState(focus ? offsetFor(focus) : 0);
  const [page, setPage] = useState<ReadTableResponse>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const focused = useRef<HTMLTableRowElement>(null);

  // The page for what's asked, once typing pauses.
  useEffect(() => {
    let live = true;
    const timer = setTimeout(() => {
      setBusy(true);
      files
        .readTable({ workspace: dir, path, search, filters: activeFilters(filters), sortColumn: sort ? sort.column + 1 : 0, descending: sort?.desc ?? false, offset, limit: pageSize })
        .then(
          (r) => live && (setPage(r), setError("")),
          (e) => live && setError(message(e)),
        )
        .finally(() => live && setBusy(false));
    }, 250);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [dir, path, search, filters, sort, offset]);
  useEffect(() => focused.current?.scrollIntoView({ block: "center" }), [page]);
  // Asking again starts at the first page.
  const ask = <T,>(set: (v: T) => void) => (v: T) => (set(v), setOffset(0));
  const setSearchAt = ask(setSearch);
  const setFilter = (col: number, expr: string) => ask(setFilters)({ ...filters, [col]: expr });

  const columns = page?.columns ?? [];
  const rows = page?.rows ?? [];
  const matched = page?.matched ?? 0;
  const filtered = !!search.trim() || activeFilters(filters).length > 0;
  return (
    <div className="preview table-view" aria-busy={busy}>
      <div className="table-bar">
        <span className="table-search">
          <Icon path={mdiMagnify} size="sm" />
          <input className="input" value={search} placeholder={t("desktop.table.search")} aria-label={t("desktop.table.search")} onChange={(e) => setSearchAt(e.target.value)} />
        </span>
        <span className="t-body-sm muted">
          {page && (filtered ? t("desktop.table.matched", { matched: matched.toLocaleString(), total: page.total.toLocaleString() }) : t("desktop.table.rows", { total: page.total.toLocaleString() }))}
          {page?.latin1 && <span className="chip static table-badge">{t("desktop.table.latin1")}</span>}
        </span>
        {filtered && <IconButton icon={mdiFilterRemoveOutline} label={t("desktop.table.clear")} small onClick={() => (setSearch(""), setFilters({}), setOffset(0))} />}
        <span className="table-pages t-body-sm">
          <IconButton icon={mdiChevronLeft} label={t("desktop.table.previous")} small disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - pageSize))} />
          <span className="muted">{pageRange(offset, rows.length)}</span>
          <IconButton icon={mdiChevronRight} label={t("desktop.table.next")} small disabled={offset + rows.length >= matched} onClick={() => setOffset(offset + pageSize)} />
        </span>
      </div>
      {error && <p className="error-text t-body-sm table-note">{error}</p>}
      {page && sort && !page.sorted && <p className="t-body-sm muted table-note">{t("desktop.table.too_many_to_sort")}</p>}
      <div className="table-scroll">
        <table className="table-grid">
          <thead>
            <tr>
              <th className="table-rownum" aria-label={t("desktop.table.row")}>
                #
              </th>
              {columns.map((c, i) => (
                <th key={i} title={c.intent || undefined} className={c.kind === "number" ? "num" : ""}>
                  <button className="table-head" onClick={() => (setSort(nextSort(sort, i)), setOffset(0))} aria-sort={sort?.column === i ? (sort.desc ? "descending" : "ascending") : "none"}>
                    <span className="ellipsis">{c.name}</span>
                    {sort?.column === i && <Icon path={sort.desc ? mdiArrowDown : mdiArrowUp} size="sm" />}
                  </button>
                  <span className="table-kind t-body-sm muted ellipsis">{c.intent || t(`desktop.table.kind.${c.kind || "text"}`)}</span>
                  <input
                    className="input table-filter mono"
                    value={filters[i] ?? ""}
                    placeholder={c.kind === "number" ? ">5, 10..20" : t("desktop.table.filter")}
                    aria-label={t("desktop.table.filter_on", { column: c.name })}
                    onChange={(e) => setFilter(i, e.target.value)}
                  />
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.number} ref={r.number === focus ? focused : undefined} className={r.number === focus ? "focus" : ""}>
                <td className="table-rownum muted">{r.number.toLocaleString()}</td>
                {columns.map((c, i) => (
                  <td key={i} className={c.kind === "number" ? "num mono" : "mono"}>
                    {r.cells[i] ?? ""}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
        {page && rows.length === 0 && <p className="t-body-sm muted table-note">{t("desktop.table.none")}</p>}
      </div>
    </div>
  );
}
