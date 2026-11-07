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

import { useEffect, useMemo, useState } from "react";
import { mdiClose, mdiCreationOutline, mdiLoading, mdiPlay, mdiPlus } from "@mdi/js";
import { Button, Field, IconButton, Segmented, Switch, useSnackbar } from "@blitz/webui";
import { workspaces } from "../api";
import { message } from "../errors";
import type { Connector, DataTable } from "../gen/blitz/v1/workspace_pb";
import { t } from "../i18n";
import { SuggestInput } from "../SecretSuggest";
import { paramValue, type StepForm } from "./workflowForm";
import { sampleParams, templated } from "./dataStep";
import { DataTableView } from "../DataTableView";

/** The template a parameter most often holds, and SQL's start: code, not text to translate. */
const inputTemplate = "{{ input }}";
const sqlPlaceholder = "SELECT …";

/** How many rows Try it shows. */
const tryRows = 20;

/**
 * A Data step's settings (spec_059_data DAT-20..DAT-24): one of the
 * workspace's database connections, a saved query of it (its parameters
 * by name) or SQL (written, or drafted from a description, its parameters
 * in order), its limits, and Try it, which runs it now on sample values,
 * read-only, and shows the first rows.
 */
export function DataStepFields({ workspace, step, onChange }: { workspace: string; step: StepForm; onChange: (patch: Partial<StepForm>) => void }) {
  const snack = useSnackbar();
  const [connections, setConnections] = useState<Connector[] | null>(null);
  const [samples, setSamples] = useState<Record<string, string>>({});
  const [trying, setTrying] = useState(false);
  const [drafting, setDrafting] = useState(false);
  const [tried, setTried] = useState<{ table?: DataTable; problem: string } | null>(null);

  useEffect(() => {
    let live = true;
    workspaces.listConnectors({ workspace }).then(
      (r) => live && setConnections(r.connections.filter((c) => c.category === "databases")),
      (e) => live && (setConnections([]), snack(message(e), { error: true })),
    );
    return () => {
      live = false;
    };
  }, [workspace, snack]);

  const conn = connections?.find((c) => c.name === step.connection);
  const saved = conn?.queries.find((q) => q.name === step.savedQuery);
  const bigquery = conn?.kind === "bigquery";
  // The values Try it asks for: one per parameter that holds a template.
  const asked = useMemo(() => (step.dataFrom === "saved" ? (saved?.params ?? []).map((p) => ({ key: p.name, value: step.dataArgs[p.name] ?? "" })) : step.params.map((v, i) => ({ key: String(i + 1), value: v }))).filter((p) => templated(p.value)), [step.dataFrom, step.dataArgs, step.params, saved]);
  // A change to what runs makes the last try stale.
  useEffect(() => setTried(null), [step.connection, step.dataFrom, step.savedQuery, step.sql]);

  const tryIt = async () => {
    if (!conn) return;
    setTrying(true);
    try {
      const fill = (v: string, key: string) => (templated(v) ? (samples[key] ?? "") : v);
      const r =
        step.dataFrom === "saved"
          ? await workspaces.runConnectorQuery({ workspace, connection: conn, query: step.savedQuery, paramsJson: JSON.stringify(sampleParams((saved?.params ?? []).map((p) => [p.name, fill(step.dataArgs[p.name] ?? "", p.name)]))), maxRows: tryRows, readOnly: true })
          : await workspaces.runConnectorQuery({ workspace, connection: conn, statement: step.sql, paramsJson: JSON.stringify(step.params.map((v, i) => paramValue(fill(v, String(i + 1))))), maxRows: tryRows, readOnly: true });
      setTried({ table: r.table, problem: r.problem });
    } catch (e) {
      setTried({ problem: message(e) });
    } finally {
      setTrying(false);
    }
  };

  const draft = async () => {
    if (!conn) return;
    setDrafting(true);
    try {
      const r = await workspaces.draftConnectorQuery({ workspace, connection: conn, request: step.describe });
      if (r.problem || !r.query) {
        snack(r.problem, { error: true });
        return;
      }
      onChange({ sql: r.query.statement, params: r.query.params.map(() => "") });
    } catch (e) {
      snack(message(e), { error: true });
    } finally {
      setDrafting(false);
    }
  };

  return (
    <>
      <Field label={t("desktop.flows.data.connection")} supporting={connections && connections.length === 0 ? t("desktop.flows.data.none") : t("desktop.flows.data.connection_hint")}>
        {(id) => (
          <select id={id} className="select" value={step.connection} onChange={(e) => onChange({ connection: e.target.value, savedQuery: "", dataArgs: {} })}>
            <option value="">{t("desktop.flows.choose")}</option>
            {[...new Set([...(connections ?? []).map((c) => c.name), step.connection].filter(Boolean))].map((name) => {
              const c = connections?.find((x) => x.name === name);
              const notes = [c && !c.enabled ? t("desktop.flows.data.off") : "", c?.readOnly ? t("desktop.flows.data.read_only") : ""].filter(Boolean);
              return (
                <option key={name} value={name}>
                  {notes.length ? `${name} (${notes.join(", ")})` : name}
                </option>
              );
            })}
          </select>
        )}
      </Field>
      <div className="stack" style={{ gap: 4 }}>
        <span className="t-label">{t("desktop.flows.data.from")}</span>
        <Segmented label={t("desktop.flows.data.from")} value={step.dataFrom} options={(["sql", "saved"] as const).map((k) => ({ value: k, label: t(`desktop.flows.data.from_kinds.${k}`) }))} onChange={(v) => onChange({ dataFrom: v as StepForm["dataFrom"] })} />
      </div>
      {step.dataFrom === "saved" ? (
        <>
          <Field label={t("desktop.flows.data.query")} supporting={saved?.description}>
            {(id) => (
              <select id={id} className="select" value={step.savedQuery} disabled={!conn} onChange={(e) => onChange({ savedQuery: e.target.value, dataArgs: {} })}>
                <option value="">{t("desktop.flows.choose")}</option>
                {[...new Set([...(conn?.queries ?? []).map((q) => q.name), step.savedQuery].filter(Boolean))].map((q) => (
                  <option key={q} value={q}>
                    {q}
                  </option>
                ))}
              </select>
            )}
          </Field>
          {saved?.params.map((p) => (
            <Field key={p.name} label={p.name} supporting={p.description}>
              {(id) => <SuggestInput id={id} className="input mono" value={step.dataArgs[p.name] ?? ""} placeholder={inputTemplate} onValue={(v) => onChange({ dataArgs: { ...step.dataArgs, [p.name]: v } })} workspace={workspace} />}
            </Field>
          ))}
        </>
      ) : (
        <>
          <Field label={t("desktop.flows.data.sql")} supporting={t("desktop.flows.data.sql_hint")}>
            {(id) => <textarea id={id} className="input mono" rows={5} value={step.sql} spellCheck={false} placeholder={sqlPlaceholder} onChange={(e) => onChange({ sql: e.target.value })} />}
          </Field>
          <Field label={t("desktop.flows.data.describe")} supporting={t("desktop.flows.data.draft_hint")}>
            {(id) => (
              <div className="stack" style={{ gap: 6 }}>
                <input id={id} className="input" value={step.describe} placeholder={t("desktop.flows.data.describe_placeholder")} onChange={(e) => onChange({ describe: e.target.value })} />
                <div className="row" style={{ justifyContent: "flex-end" }}>
                  <Button variant="tonal" icon={drafting ? mdiLoading : mdiCreationOutline} iconSpin={drafting} disabled={!conn || drafting || !step.describe.trim()} onClick={() => void draft()}>
                    {t("desktop.flows.data.draft")}
                  </Button>
                </div>
              </div>
            )}
          </Field>
          <div className="stack" style={{ gap: 6 }}>
            <span className="t-label">{t("desktop.flows.data.params")}</span>
            <span className="t-body-sm muted">{t("desktop.flows.data.params_hint")}</span>
            {step.params.map((v, i) => (
              <div key={i} className="row" style={{ gap: 6 }}>
                <SuggestInput className="input mono" aria-label={t("desktop.flows.data.param", { n: i + 1 })} value={v} placeholder={inputTemplate} onValue={(x) => onChange({ params: step.params.map((p, j) => (j === i ? x : p)) })} workspace={workspace} />
                <IconButton icon={mdiClose} label={t("desktop.flows.data.remove_param")} small onClick={() => onChange({ params: step.params.filter((_, j) => j !== i) })} />
              </div>
            ))}
            <div>
              <Button variant="text" icon={mdiPlus} onClick={() => onChange({ params: [...step.params, ""] })}>
                {t("desktop.flows.data.add_param")}
              </Button>
            </div>
          </div>
        </>
      )}
      <span className="t-body-sm muted">{t("desktop.flows.data.output_hint")}</span>
      <Field label={t("desktop.flows.data.max_rows")} supporting={t("desktop.flows.data.max_rows_hint")}>
        {(id) => <input id={id} className="input" inputMode="numeric" value={step.maxRows} placeholder="1000" onChange={(e) => onChange({ maxRows: e.target.value.replace(/[^\d]/g, "") })} />}
      </Field>
      <div className="stack" style={{ gap: 4 }}>
        <label className="row agent-switch">
          <Switch label={t("desktop.flows.data.fail_when_cut")} checked={step.failWhenCut} onChange={(v) => onChange({ failWhenCut: v })} />
          <span className="t-label">{t("desktop.flows.data.fail_when_cut")}</span>
        </label>
        <span className="t-body-sm muted">{t("desktop.flows.data.fail_when_cut_hint")}</span>
      </div>
      {!conn?.readOnly && (
        <div className="stack" style={{ gap: 4 }}>
          <label className="row agent-switch">
            <Switch label={t("desktop.flows.data.allow_writes")} checked={step.allowWrites} onChange={(v) => onChange({ allowWrites: v })} />
            <span className="t-label">{t("desktop.flows.data.allow_writes")}</span>
          </label>
          <span className="t-body-sm muted">{t("desktop.flows.data.allow_writes_hint")}</span>
        </div>
      )}
      {bigquery && (
        <Field label={t("desktop.flows.data.max_cost")} supporting={t("desktop.flows.data.max_cost_hint")}>
          {(id) => <input id={id} className="input" inputMode="decimal" value={step.maxCost} placeholder="1" onChange={(e) => onChange({ maxCost: e.target.value.replace(/[^\d.]/g, "") })} />}
        </Field>
      )}
      <div className="stack flow-data-try" style={{ gap: 6 }}>
        <span className="t-body-sm muted">{t("desktop.flows.data.try_hint")}</span>
        {asked.map((p) => (
          <Field key={p.key} label={t("desktop.flows.data.sample", { param: p.key })} supporting={p.value}>
            {(id) => <input id={id} className="input mono" value={samples[p.key] ?? ""} onChange={(e) => setSamples({ ...samples, [p.key]: e.target.value })} />}
          </Field>
        ))}
        <div>
          <Button variant="tonal" icon={mdiPlay} disabled={!conn || trying || (step.dataFrom === "saved" ? !step.savedQuery : !step.sql.trim())} onClick={() => void tryIt()}>
            {t("desktop.flows.data.try")}
          </Button>
        </div>
        {tried?.problem && <p className="error-text t-body-sm">{tried.problem}</p>}
        {tried?.table && <DataTableView table={tried.table} />}
      </div>
    </>
  );
}
