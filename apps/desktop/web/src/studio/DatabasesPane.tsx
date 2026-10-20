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

import { useCallback, useEffect, useState } from "react";
import { createPortal } from "react-dom";
import {
  mdiAlertCircleOutline,
  mdiArrowLeft,
  mdiCheckCircleOutline,
  mdiCloseCircleOutline,
  mdiContentSaveOutline,
  mdiDatabaseLockOutline,
  mdiDatabaseOutline,
  mdiDeleteOutline,
  mdiLanConnect,
  mdiLoading,
  mdiPlay,
  mdiPlus,
  mdiUndoVariant,
} from "@mdi/js";
import { workspaces } from "../api";
import { message } from "../errors";
import { t, tn } from "../i18n";
import type { DatabaseConnection, DatabaseKind, DatabaseTool } from "../gen/blitz/v1/workspace_pb";
import { Button, Dialog, Field, Icon, IconButton, Segmented, Switch, useSnackbar } from "../ui/controls";
import { connectionFromForm, formFromConnection, newDatabaseForm, paramTypes, readsOnly, sameDatabaseForm, statementParams, withKind, type DatabaseForm, type DatabaseScope, type QueryForm } from "./databaseForm";

/** The agents' name for a database tool: the databases server's prefix, db (spec_databases_046). */
const agentTool = (tool: string) => `db__${tool}`;

/** What a connection is open as: one by scope and name, or a new one (name ""). */
type Open = { scope: DatabaseScope; name: string } | null;

/** How the databases server is doing, from ListDatabases. */
interface ServerState {
  state: string;
  error: string;
  tools: number;
}

/**
 * The databases the agents can use (Studio › Databases,
 * spec_databases_046), served by MCP Toolbox for Databases inside Blitz:
 * this workspace's and every workspace's, each listed with its kind and
 * whether it's read-only. A connection's form slides in over the list:
 * its kind (Postgres, AlloyDB, SQLite, BigQuery) and settings, a password
 * kept as a secret, read-only (the default), the ready-made tools, and
 * custom queries, each with Try it; Test connection lists what it offers.
 * A file the form can't describe is edited as YAML.
 */
export function DatabasesPane({ workspace, onDirty }: { workspace: string; onDirty?: (dirty: boolean) => void }) {
  const snack = useSnackbar();
  const [connections, setConnections] = useState<DatabaseConnection[]>([]);
  const [kinds, setKinds] = useState<DatabaseKind[]>([]);
  const [server, setServer] = useState<ServerState>({ state: "", error: "", tools: 0 });
  const [error, setError] = useState("");
  const [open, setOpen] = useState<Open>(null);
  const [form, setForm] = useState<DatabaseForm>(newDatabaseForm(undefined));
  const [base, setBase] = useState<DatabaseForm>(newDatabaseForm(undefined));
  const [problems, setProblems] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState<(() => void) | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [testing, setTesting] = useState(false);
  const [tested, setTested] = useState<{ tools: DatabaseTool[]; problem: string } | null>(null);

  const refresh = useCallback(async () => {
    try {
      const r = await workspaces.listDatabases({ workspace });
      setConnections(r.connections);
      setKinds(r.kinds);
      setServer({ state: r.state, error: r.error, tools: r.toolCount });
      setError("");
    } catch (e) {
      setError(message(e));
    }
  }, [workspace]);
  useEffect(() => void refresh(), [refresh]);

  const kindOf = (type: string) => kinds.find((k) => k.type === type);
  const file = open && open.name ? connections.find((c) => c.scope === open.scope && c.name === open.name) : undefined;
  const isNew = open !== null && open.name === "";
  const dirty = open !== null && !sameDatabaseForm(form, base);
  useEffect(() => onDirty?.(dirty), [dirty, onDirty]);

  const leave = (go: () => void) => (dirty ? setPending(() => go) : go());
  const showForm = (o: Open, f: DatabaseForm) =>
    leave(() => {
      setForm(f);
      setBase(f);
      setOpen(o);
      setProblems([]);
      setTested(null);
    });
  const close = () =>
    leave(() => {
      setOpen(null);
      void refresh();
    });
  const set = <K extends keyof DatabaseForm>(k: K, v: DatabaseForm[K]) => setForm((f) => ({ ...f, [k]: v }));
  const setQuery = (i: number, q: Partial<QueryForm>) => setForm((f) => ({ ...f, queries: f.queries.map((x, j) => (j === i ? { ...x, ...q } : x)) }));

  const save = async () => {
    if (busy) return;
    setBusy(true);
    setProblems([]);
    try {
      const { connection, secretValues } = connectionFromForm(form);
      const r = await workspaces.saveDatabase({ workspace, previousName: file?.name ?? "", connection, secretValues });
      if (r.problems.length || !r.connection) return setProblems(r.problems);
      snack(t("desktop.databases.saved", { name: r.connection.name }));
      setOpen(null);
      await refresh();
    } catch (e) {
      setProblems([message(e)]);
    } finally {
      setBusy(false);
    }
  };
  const remove = async () => {
    setDeleting(false);
    if (!file) return;
    try {
      await workspaces.deleteDatabase({ workspace, scope: file.scope, name: file.name });
      snack(t("desktop.databases.deleted", { name: file.name }));
      setOpen(null);
      await refresh();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const test = async () => {
    setTesting(true);
    setTested(null);
    try {
      const r = await workspaces.testDatabase({ workspace, ...connectionFromForm(form) });
      setTested({ tools: r.tools, problem: r.problem });
    } catch (e) {
      setTested({ tools: [], problem: message(e) });
    } finally {
      setTesting(false);
    }
  };

  const kind = kindOf(form.type);
  const sections: { scope: DatabaseScope; title: string; hint: string }[] = [
    { scope: "workspace", title: t("desktop.databases.here"), hint: t("desktop.workers.in", { path: ".agents/databases" }) },
    { scope: "global", title: t("desktop.databases.everywhere"), hint: t("desktop.workers.in", { path: "~/.blitz/databases" }) },
  ];
  const title = form.name || t("desktop.databases.new");
  const where = file?.path ?? `${form.scope === "global" ? "~/.blitz" : ".agents"}/databases/${form.name.trim() || "…"}.yaml`;
  return (
    <div className={`slide-pane databases-pane ${open ? "is-open" : ""}`}>
      <aside className="slide-list" inert={open !== null}>
        <div className="row side-head">
          <span className="t-title-sm spacer">{t("desktop.studio.databases")}</span>
          <IconButton icon={mdiPlus} label={t("desktop.databases.new")} small onClick={() => showForm({ scope: "workspace", name: "" }, newDatabaseForm(kinds[0]))} />
        </div>
        <p className="t-body-sm muted">{t("desktop.databases.intro")}</p>
        {server.state && (
          <div className={`row small-card t-body-sm ${server.state === "failed" ? "error-text" : "muted"}`} style={{ gap: 6 }}>
            <Icon path={server.state === "failed" ? mdiAlertCircleOutline : server.state === "connected" ? mdiCheckCircleOutline : mdiDatabaseOutline} size="sm" />
            <span className="spacer">{server.state === "failed" ? t("desktop.databases.server_failed", { error: server.error }) : server.state === "connected" ? tn("desktop.databases.server_connected", server.tools) : t("desktop.databases.server_idle")}</span>
          </div>
        )}
        {error && (
          <div className="card error row small-card">
            <Icon path={mdiAlertCircleOutline} size="sm" /> {error}
          </div>
        )}
        {sections.map((s) => {
          const list = connections.filter((c) => c.scope === s.scope);
          return (
            <section key={s.scope} className="stack rule-section" style={{ gap: 4 }}>
              <span className="t-title-sm">{s.title}</span>
              <span className="t-body-sm muted">{s.hint}</span>
              {list.length === 0 && <p className="t-body-sm muted">{t("desktop.databases.none")}</p>}
              <div className="list">
                {list.map((c) => (
                  <div key={c.path} className="list-item">
                    <button type="button" className="row-open" onClick={() => showForm({ scope: s.scope, name: c.name }, formFromConnection(c, kindOf(c.type)))}>
                      <Icon path={c.problem ? mdiAlertCircleOutline : c.readOnly ? mdiDatabaseLockOutline : mdiDatabaseOutline} className={c.problem ? "error-text" : undefined} />
                      <span className="lines spacer">
                        <span className="ellipsis">{c.name}</span>
                        <small className={`ellipsis ${c.problem ? "error-text" : ""}`}>
                          {c.problem ||
                            [c.custom ? t("desktop.databases.custom") : t(`desktop.databases.kind.${c.type}`), c.readOnly ? t("desktop.databases.read_only") : c.custom ? "" : t("desktop.databases.read_write"), c.custom ? "" : tn("desktop.databases.tool_count", c.tools.length + c.queries.length), c.description].filter(Boolean).join(" · ")}
                        </small>
                      </span>
                    </button>
                  </div>
                ))}
              </div>
            </section>
          );
        })}
      </aside>
      <section className="slide-detail" inert={open === null}>
        {open && (
          <>
            <div className="row slide-back">
              <IconButton icon={mdiArrowLeft} label={t("desktop.databases.back")} onClick={close} />
              <h2 className="t-headline spacer ellipsis">{title}</h2>
              {dirty && <span className="t-body-sm muted">{t("desktop.agents.unsaved")}</span>}
            </div>
            <fieldset className="worker agent-form rule-form">
              <div className="pair">
                <Field label={t("desktop.databases.name")} supporting={t("desktop.databases.name_hint", { name: form.name.trim() || "shop" })}>
                  {(id) => <input id={id} className="input mono" value={form.name} onChange={(e) => set("name", e.target.value)} />}
                </Field>
                <Field label={t("desktop.databases.description")} supporting={t("desktop.databases.description_hint")}>
                  {(id) => <input id={id} className="input" value={form.description} disabled={form.custom} onChange={(e) => set("description", e.target.value)} />}
                </Field>
              </div>
              <div className="stack" style={{ gap: 6 }}>
                <span className="t-label">{t("desktop.databases.scope")}</span>
                <Segmented value={form.scope} label={t("desktop.databases.scope")} options={sections.map((s) => ({ value: s.scope, label: s.title }))} onChange={(v) => set("scope", v)} />
                <span className="t-body-sm muted">{t(`desktop.databases.scope_hint.${form.scope}`)}</span>
              </div>
              {form.custom ? (
                <Field label={t("desktop.databases.yaml")} supporting={t("desktop.databases.custom_hint")}>
                  {(id) => <textarea id={id} className="input mono" rows={16} value={form.text} onChange={(e) => set("text", e.target.value)} />}
                </Field>
              ) : (
                <>
                  <div className="stack" style={{ gap: 6 }}>
                    <span className="t-label">{t("desktop.databases.kind")}</span>
                    <Segmented value={form.type} label={t("desktop.databases.kind")} options={kinds.map((k) => ({ value: k.type, label: t(`desktop.databases.kind.${k.type}`) }))} onChange={(v) => setForm((f) => withKind(f, kindOf(v)!))} />
                    <span className="t-body-sm muted">{t(`desktop.databases.kind_hint.${form.type}`)}</span>
                  </div>
                  {kind && (
                    <div className="db-fields">
                      {kind.fields.map((fl) =>
                        fl.secret ? (
                          <Field key={fl.key} label={t(`desktop.databases.field.${fl.key}`)} supporting={form.secretsSet.includes(fl.key) ? t("desktop.databases.secret_kept") : t("desktop.databases.secret_hint")}>
                            {(id) => (
                              <input
                                id={id}
                                className="input"
                                type="password"
                                autoComplete="off"
                                value={form.secretValues[fl.key] ?? ""}
                                placeholder={form.secretsSet.includes(fl.key) ? "••••••••" : ""}
                                onChange={(e) => set("secretValues", { ...form.secretValues, [fl.key]: e.target.value })}
                              />
                            )}
                          </Field>
                        ) : (
                          <Field key={fl.key} label={t(`desktop.databases.field.${fl.key}`) + (fl.required && !fl.defaultValue ? " *" : "")} supporting={t(`desktop.databases.field_hint.${fl.key}`)}>
                            {(id) => <input id={id} className={`input ${fl.key === "database" && form.type === "sqlite" ? "mono" : ""}`} value={form.settings[fl.key] ?? ""} placeholder={fl.defaultValue} onChange={(e) => set("settings", { ...form.settings, [fl.key]: e.target.value })} />}
                          </Field>
                        ),
                      )}
                    </div>
                  )}
                  <label className="row agent-switch">
                    <Switch label={t("desktop.databases.read_only")} checked={form.readOnly} onChange={(v) => set("readOnly", v)} />
                    <span className="stack" style={{ gap: 0 }}>
                      <span className="t-label">{t("desktop.databases.read_only")}</span>
                      <span className="t-body-sm muted">{t(form.readOnly ? `desktop.databases.read_only_hint.${form.type}` : "desktop.databases.read_write_hint")}</span>
                    </span>
                  </label>
                  {kind && (
                    <div className="stack" style={{ gap: 6 }}>
                      <span className="t-label">{t("desktop.databases.tools")}</span>
                      <span className="t-body-sm muted">{t("desktop.databases.tools_hint", { name: form.name.trim() || "shop" })}</span>
                      <div className="list">
                        {kind.tools.map((rt) => {
                          const hidden = rt.writes && form.readOnly;
                          return (
                            <label key={rt.id} className="list-item row agent-switch">
                              <Switch label={rt.id} checked={form.tools.includes(rt.id)} onChange={(v) => set("tools", v ? [...form.tools, rt.id] : form.tools.filter((x) => x !== rt.id))} />
                              <span className="stack spacer" style={{ gap: 0 }}>
                                <span className="mono">{rt.id}</span>
                                <span className="t-body-sm muted">{hidden ? t("desktop.databases.tool_hidden") : t(`desktop.databases.tool.${rt.id}`)}</span>
                              </span>
                            </label>
                          );
                        })}
                      </div>
                    </div>
                  )}
                  <div className="stack" style={{ gap: 6 }}>
                    <div className="row">
                      <span className="t-label spacer">{t("desktop.databases.queries")}</span>
                      <Button icon={mdiPlus} onClick={() => set("queries", [...form.queries, { name: "", description: "", statement: "", params: [] }])}>
                        {t("desktop.databases.add_query")}
                      </Button>
                    </div>
                    <span className="t-body-sm muted">{t(`desktop.databases.queries_hint.${form.type}`)}</span>
                    {form.queries.map((q, i) => (
                      <QueryCard key={i} workspace={workspace} form={form} query={q} onChange={(x) => setQuery(i, x)} onRemove={() => set("queries", form.queries.filter((_, j) => j !== i))} />
                    ))}
                  </div>
                </>
              )}
            </fieldset>
            <div className="card outlined stack rule-try" style={{ gap: 8 }}>
              <div className="row" style={{ gap: 8 }}>
                <span className="stack spacer" style={{ gap: 0 }}>
                  <span className="t-title-sm">{t("desktop.databases.test")}</span>
                  <span className="t-body-sm muted">{t("desktop.databases.test_hint")}</span>
                </span>
                <Button variant="tonal" icon={testing ? mdiLoading : mdiLanConnect} iconSpin={testing} disabled={testing || !form.name.trim()} onClick={() => void test()}>
                  {testing ? t("desktop.databases.testing") : t("desktop.databases.test_button")}
                </Button>
              </div>
              {tested && (
                <div className={`stack small-card ${tested.problem ? "error-text" : ""}`} style={{ gap: 4 }}>
                  <span className="row t-body-sm" style={{ gap: 6 }}>
                    <Icon path={tested.problem ? mdiCloseCircleOutline : mdiCheckCircleOutline} size="sm" />
                    {tested.problem ? t("desktop.databases.test_failed", { problem: tested.problem }) : tn("desktop.databases.test_ok", tested.tools.length)}
                  </span>
                  {tested.tools.map((tl) => (
                    <span key={tl.name} className="row t-body-sm" style={{ gap: 6 }} title={tl.description}>
                      <span className="mono">{agentTool(tl.name)}</span>
                      <span className="muted ellipsis spacer">{tl.description}</span>
                      <span className="muted">{tl.readOnly ? t("desktop.databases.runs_unasked") : t("desktop.databases.asks_first")}</span>
                    </span>
                  ))}
                </div>
              )}
            </div>
            {problems.length > 0 && (
              <div className="card error stack" role="alert" style={{ gap: 4 }}>
                {problems.map((p) => (
                  <span key={p} className="row t-body-sm" style={{ gap: 6 }}>
                    <Icon path={mdiAlertCircleOutline} size="sm" /> {p}
                  </span>
                ))}
              </div>
            )}
            <div className="row wrap agent-actions slide-actions">
              <span className="t-body-sm muted spacer ellipsis" title={where}>
                {t("desktop.agents.where", { path: where })}
              </span>
              {file && (
                <Button danger icon={mdiDeleteOutline} onClick={() => setDeleting(true)}>
                  {t("desktop.rules.delete")}
                </Button>
              )}
              {dirty && file && (
                <Button icon={mdiUndoVariant} onClick={() => setForm(base)}>
                  {t("desktop.agents.revert")}
                </Button>
              )}
              <Button onClick={close}>{t("desktop.cancel")}</Button>
              <Button variant="filled" icon={mdiContentSaveOutline} disabled={busy || !form.name.trim() || (!!file && !dirty && !isNew)} onClick={() => void save()}>
                {t("desktop.rules.save")}
              </Button>
            </div>
          </>
        )}
      </section>
      {deleting &&
        file &&
        createPortal(
          <Dialog
            title={t("desktop.databases.delete_title", { name: file.name })}
            icon={mdiDeleteOutline}
            onClose={() => setDeleting(false)}
            footer={
              <>
                <Button onClick={() => setDeleting(false)}>{t("desktop.cancel")}</Button>
                <Button variant="filled" danger onClick={() => void remove()}>
                  {t("desktop.rules.delete")}
                </Button>
              </>
            }
          >
            <p>{t("desktop.databases.delete_body", { path: file.path })}</p>
          </Dialog>,
          document.body,
        )}
      {pending &&
        createPortal(
          <Dialog
            title={t("desktop.agents.discard_title")}
            icon={mdiUndoVariant}
            onClose={() => setPending(null)}
            footer={
              <>
                <Button onClick={() => setPending(null)}>{t("desktop.cancel")}</Button>
                <Button
                  variant="filled"
                  danger
                  onClick={() => {
                    const go = pending;
                    setPending(null);
                    go();
                  }}
                >
                  {t("desktop.agents.discard")}
                </Button>
              </>
            }
          >
            <p>{t("desktop.agents.discard_body", { name: title })}</p>
          </Dialog>,
          document.body,
        )}
    </div>
  );
}

/** A custom query: its name, what it's for, its SQL and parameters, and Try it on the connection as written. */
function QueryCard({ workspace, form, query, onChange, onRemove }: { workspace: string; form: DatabaseForm; query: QueryForm; onChange: (q: Partial<QueryForm>) => void; onRemove: () => void }) {
  const [args, setArgs] = useState<Record<string, string>>({});
  const [trying, setTrying] = useState(false);
  const [result, setResult] = useState<{ output: string; error: boolean } | null>(null);
  const reads = readsOnly(query.statement);
  const tryIt = async () => {
    setTrying(true);
    setResult(null);
    try {
      const values = Object.fromEntries(query.params.map((p) => [p.name, typed(args[p.name] ?? "", p.type)]));
      const r = await workspaces.tryDatabaseTool({ workspace, ...connectionFromForm(form), tool: query.name.trim(), argsJson: JSON.stringify(values) });
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
        <Field label={t("desktop.databases.query_name")} supporting={query.name.trim() ? agentTool(`${form.name.trim() || "…"}_${query.name.trim()}`) : t("desktop.databases.query_name_hint")}>
          {(id) => <input id={id} className="input mono" value={query.name} onChange={(e) => onChange({ name: e.target.value })} />}
        </Field>
        <Field label={t("desktop.databases.query_description")} supporting={t("desktop.databases.query_description_hint")}>
          {(id) => <input id={id} className="input" value={query.description} onChange={(e) => onChange({ description: e.target.value })} />}
        </Field>
      </div>
      <Field label={t("desktop.databases.query_sql")} supporting={query.statement.trim() ? t(reads ? "desktop.databases.query_reads" : "desktop.databases.query_writes") : t(`desktop.databases.query_sql_hint.${form.type}`)}>
        {(id) => (
          <textarea
            id={id}
            className="input mono"
            rows={4}
            value={query.statement}
            onChange={(e) => onChange({ statement: e.target.value })}
            onBlur={() => {
              // A new query's parameters, drafted from its SQL.
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
            <input className="input mono" aria-label={t("desktop.databases.param_name")} placeholder={t("desktop.databases.param_name")} value={p.name} onChange={(e) => edit({ name: e.target.value })} />
            <select className="select" aria-label={t("desktop.databases.param_type")} value={p.type} onChange={(e) => edit({ type: e.target.value })}>
              {paramTypes.map((ty) => (
                <option key={ty} value={ty}>
                  {t(`desktop.databases.param_type.${ty}`)}
                </option>
              ))}
            </select>
            <input className="input" aria-label={t("desktop.databases.param_description")} placeholder={t("desktop.databases.param_description")} value={p.description} onChange={(e) => edit({ description: e.target.value })} />
            <input className="input" aria-label={t("desktop.databases.param_value", { name: p.name })} placeholder={t("desktop.databases.param_value", { name: p.name })} value={args[p.name] ?? ""} onChange={(e) => setArgs({ ...args, [p.name]: e.target.value })} />
            <IconButton icon={mdiDeleteOutline} label={t("desktop.databases.param_remove", { name: p.name })} small onClick={() => onChange({ params: query.params.filter((_, j) => j !== i) })} />
          </div>
        );
      })}
      <div className="row" style={{ gap: 8 }}>
        <Button icon={mdiPlus} onClick={() => onChange({ params: [...query.params, { name: "", type: "string", description: "" }] })}>
          {t("desktop.databases.add_param")}
        </Button>
        <span className="spacer" />
        <Button danger icon={mdiDeleteOutline} onClick={onRemove}>
          {t("desktop.databases.remove_query")}
        </Button>
        <Button variant="tonal" icon={trying ? mdiLoading : mdiPlay} iconSpin={trying} disabled={trying || !query.name.trim() || !query.statement.trim() || !form.name.trim()} onClick={() => void tryIt()}>
          {trying ? t("desktop.databases.trying") : t("desktop.rules.try")}
        </Button>
      </div>
      {result && <pre className={`small-card db-result ${result.error ? "error-text" : ""}`}>{result.output || t("desktop.databases.no_rows")}</pre>}
    </div>
  );
}

/** A Try it value typed as its parameter's type. */
function typed(v: string, type: string): unknown {
  if (type === "integer" || type === "float") return v.trim() === "" ? 0 : Number(v);
  if (type === "boolean") return v.trim().toLowerCase() === "true";
  return v;
}
