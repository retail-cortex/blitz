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

import type { DataTable } from "./gen/blitz/v1/workspace_pb";
import { t } from "./i18n";
import { tableRows } from "./studio/dataStep";

/**
 * A query's result (spec_059_data DAT-01): its columns with their types,
 * lengths and descriptions, its rows, how many, and whether a limit cut
 * them. Shown by a workflow's Try it and a chat's query chip.
 */
export function DataTableView({ table }: { table: DataTable }) {
  const rows = tableRows(table.rowsJson);
  const numeric = (type: string) => type === "integer" || type === "number";
  return (
    <div className="preview table-view">
      <span className="t-body-sm muted">
        {t("desktop.flows.data.total", { total: table.total.toLocaleString() })}
        {table.truncated && ` · ${t("desktop.flows.data.cut")}`}
      </span>
      <div className="table-scroll">
        <table className="table-grid">
          <thead>
            <tr>
              {table.columns.map((c, i) => (
                <th key={i} title={c.description || undefined} className={numeric(c.type) ? "num" : ""}>
                  <span className="ellipsis">{c.name}</span>
                  <span className="table-kind t-body-sm muted ellipsis">{c.description || (c.length !== undefined ? `${c.type} (${c.length})` : c.type)}</span>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((r, n) => (
              <tr key={n}>
                {table.columns.map((c, i) => (
                  <td key={i} className={numeric(c.type) ? "num mono" : "mono"}>
                    {r[i] ?? ""}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
        {rows.length === 0 && <p className="t-body-sm muted table-note">{t("desktop.flows.data.no_rows")}</p>}
      </div>
    </div>
  );
}
