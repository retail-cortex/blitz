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

import { useState } from "react";
import { mdiDeleteOutline, mdiLoading, mdiPlay, mdiPlus } from "@mdi/js";
import { message } from "../errors";
import { t } from "../i18n";
import type { ConnectorKind } from "../gen/blitz/v1/workspace_pb";
import { Button, Field, IconButton } from "../ui/controls";
import { agentTool, type ConnectorApi } from "./connectorApi";
import { can, connectionFromForm, paramTypes, readsOnly, statementParams, type ConnectorForm, type QueryForm } from "./connectorForm";

/** A connection's own query: its name, what it's for, its statement and parameters, and Try it on the connection as written. */
export function QueryCard({
  api,
  form,
  kind,
  prefix,
  query,
  onChange,
  onRemove,
}: {
  api: ConnectorApi;
  form: ConnectorForm;
  kind: ConnectorKind;
  prefix: string;
  query: QueryForm;
  onChange: (q: Partial<QueryForm>) => void;
  onRemove: () => void;
}) {
  const [args, setArgs] = useState<Record<string, string>>({});
  const [trying, setTrying] = useState(false);
  const [result, setResult] = useState<{ output: string; error: boolean } | null>(null);
  const reads = readsOnly(query.statement);
  const tryIt = async () => {
    setTrying(true);
    setResult(null);
    try {
      const values = Object.fromEntries(query.params.map((p) => [p.name, typed(args[p.name] ?? "", p.type)]));
      const r = await api.tryTool({ ...connectionFromForm(form), tool: query.name.trim(), argsJson: JSON.stringify(values) });
      setResult(r.problem ? { output: r.problem, error: true } : { output: r.output, error: r.isError });
    } catch (e) {
      setResult({ output: message(e), error: true });
    } finally {
      setTrying(false);
    }
  };
  return (
    <div className="card outlined stack db-query" style={{ gap: 8 }}>
      <div className="pair">
        <Field label={t("desktop.connectors.query_name")} supporting={query.name.trim() ? agentTool(prefix, `${form.name.trim() || "…"}_${query.name.trim()}`) : t("desktop.connectors.query_name_hint")}>
          {(id) => <input id={id} className="input mono" value={query.name} onChange={(e) => onChange({ name: e.target.value })} />}
        </Field>
        <Field label={t("desktop.connectors.query_description")} supporting={t("desktop.connectors.query_description_hint")}>
          {(id) => <input id={id} className="input" value={query.description} onChange={(e) => onChange({ description: e.target.value })} />}
        </Field>
      </div>
      <Field label={t("desktop.connectors.query_sql")} supporting={query.statement.trim() ? t(reads ? "desktop.connectors.query_reads" : "desktop.connectors.query_writes") : kind.queriesExample}>
        {(id) => (
          <textarea
            id={id}
            className="input mono"
            rows={4}
            value={query.statement}
            placeholder={kind.queriesExample}
            onChange={(e) => onChange({ statement: e.target.value })}
            onBlur={() => {
              // A new query's parameters, drafted from its statement.
              if (query.params.length === 0) {
                const names = statementParams(query.statement);
                if (names.length) onChange({ params: names.map((n) => ({ name: n, type: "string", description: "" })) });
              }
            }}
          />
        )}
      </Field>
      {query.params.map((p, i) => {
        const edit = (x: Partial<typeof p>) => onChange({ params: query.params.map((y, j) => (j === i ? { ...y, ...x } : y)) });
        return (
          <div key={i} className="db-param">
            <input className="input mono" aria-label={t("desktop.connectors.param_name")} placeholder={t("desktop.connectors.param_name")} value={p.name} onChange={(e) => edit({ name: e.target.value })} />
            <select className="select" aria-label={t("desktop.connectors.param_type")} value={p.type} onChange={(e) => edit({ type: e.target.value })}>
              {paramTypes.map((ty) => (
                <option key={ty} value={ty}>
                  {t(`desktop.connectors.param_type.${ty}`)}
                </option>
              ))}
            </select>
            <input className="input" aria-label={t("desktop.connectors.param_description")} placeholder={t("desktop.connectors.param_description")} value={p.description} onChange={(e) => edit({ description: e.target.value })} />
            <input className="input" aria-label={t("desktop.connectors.param_value", { name: p.name })} placeholder={t("desktop.connectors.param_value", { name: p.name })} value={args[p.name] ?? ""} onChange={(e) => setArgs({ ...args, [p.name]: e.target.value })} />
            <IconButton icon={mdiDeleteOutline} label={t("desktop.connectors.param_remove", { name: p.name })} small onClick={() => onChange({ params: query.params.filter((_, j) => j !== i) })} />
          </div>
        );
      })}
      <div className="row" style={{ gap: 8 }}>
        <Button icon={mdiPlus} onClick={() => onChange({ params: [...query.params, { name: "", type: "string", description: "" }] })}>
          {t("desktop.connectors.add_param")}
        </Button>
        <span className="spacer" />
        <Button danger icon={mdiDeleteOutline} onClick={onRemove}>
          {t("desktop.connectors.remove_query")}
        </Button>
        {can(kind, "try") && (
          <Button variant="tonal" icon={trying ? mdiLoading : mdiPlay} iconSpin={trying} disabled={trying || !query.name.trim() || !query.statement.trim() || !form.name.trim()} onClick={() => void tryIt()}>
            {trying ? t("desktop.connectors.trying") : t("desktop.connectors.try_it")}
          </Button>
        )}
      </div>
      {result && <pre className={`small-card db-result ${result.error ? "error-text" : ""}`}>{result.output || t("desktop.connectors.no_rows")}</pre>}
    </div>
  );
}

/** A Try it value typed as its parameter's type. */
function typed(v: string, type: string): unknown {
  if (type === "integer" || type === "float") return v.trim() === "" ? 0 : Number(v);
  if (type === "boolean") return v.trim().toLowerCase() === "true";
  return v;
}
