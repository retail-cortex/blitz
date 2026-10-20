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
  mdiCreationOutline,
  mdiDatabaseLockOutline,
  mdiDatabaseOutline,
  mdiDeleteOutline,
  mdiLanConnect,
  mdiLoading,
  mdiPlay,
  mdiPlus,
  mdiPowerPlugOffOutline,
  mdiPowerPlugOutline,
  mdiShieldAlertOutline,
  mdiTableLarge,
  mdiUndoVariant,
} from "@mdi/js";
import { workspaces } from "../api";
import { message } from "../errors";
import { language, t, tn, useLanguage } from "../i18n";
import type { Connector, ConnectorCategory, ConnectorKind, ConnectorTable, ConnectorTool } from "../gen/blitz/v1/workspace_pb";
import { Button, Dialog, Field, Icon, IconButton, Segmented, Switch, useSnackbar } from "../ui/controls";
import { can, connectionFromForm, formFromConnection, newConnectorForm, paramTypes, queryForm, readsOnly, sameConnectorForm, statementParams, textIn, withKind, type ConnectorForm, type ConnectorScope, type QueryForm } from "./connectorForm";

/** A category's icon, by the name its descriptor gives. */
const categoryIcons: Record<string, string> = { database: mdiDatabaseOutline };

/** The agents' name for a connector tool: its category's prefix, then __. */
const agentTool = (prefix: string, tool: string) => `${prefix}__${tool}`;

/** What a connection is open as: one by category, scope and name, or a new one (name ""). */
type Open = { category: string; scope: ConnectorScope; name: string } | null;

/**
 * The connectors the agents reach (Studio › Connectors,
 * spec_connectors_047), by category (Databases, served by MCP Toolbox for
 * Databases inside Blitz): each category's connections, this workspace's
 * and every workspace's, with its kind and whether it's read-only. A
 * connection's form slides in over the list, drawn from its kind's
 * descriptor: its settings, a password kept as a secret, read-only (the
 * default when it can be), the ready-made tools, and, as its kind can,
 * its own queries with Try it and Write a query (the query builder), the
 * tables, and Test connection. A file the form can't describe is edited
 * as it is.
 */
export function ConnectorsPane({ workspace, onDirty }: { workspace: string; onDirty?: (dirty: boolean) => void }) {
  useLanguage();
  const lang = language();
  const snack = useSnackbar();
  const [categories, setCategories] = useState<ConnectorCategory[]>([]);
  const [kinds, setKinds] = useState<ConnectorKind[]>([]);
  const [connections, setConnections] = useState<Connector[]>([]);
  const [error, setError] = useState("");
  const [open, setOpen] = useState<Open>(null);
  const [form, setForm] = useState<ConnectorForm>(newConnectorForm("", undefined));
  const [base, setBase] = useState<ConnectorForm>(newConnectorForm("", undefined));
  const [problems, setProblems] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState<(() => void) | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [testing, setTesting] = useState(false);
  const [tested, setTested] = useState<{ tools: ConnectorTool[]; problem: string } | null>(null);
  const [reading, setReading] = useState(false);
  const [schema, setSchema] = useState<{ tables: ConnectorTable[]; problem: string } | null>(null);
  const [building, setBuilding] = useState(false);

  const refresh = useCallback(async () => {
    try {
      const r = await workspaces.listConnectors({ workspace });
      setCategories(r.categories);
      setKinds(r.kinds);
      setConnections(r.connections);
      setError("");
    } catch (e) {
      setError(message(e));
    }
  }, [workspace]);
  useEffect(() => void refresh(), [refresh]);

  const usable = categories.filter((c) => !c.problem);
  const kindsOf = (category: string) => kinds.filter((k) => k.category === category && !k.problem);
  const kindOf = (id: string) => kinds.find((k) => k.id === id && !k.problem);
  const categoryOf = (id: string) => categories.find((c) => c.id === id);
  const broken = [...categories.filter((c) => c.problem), ...kinds.filter((k) => k.problem)];
  const file = open && open.name ? connections.find((c) => c.category === open.category && c.scope === open.scope && c.name === open.name) : undefined;
  const untrusted = !!file && file.scope === "workspace" && !file.trusted;
  const isNew = open !== null && open.name === "";
  const dirty = open !== null && !sameConnectorForm(form, base);
  useEffect(() => onDirty?.(dirty), [dirty, onDirty]);

  const leave = (go: () => void) => (dirty ? setPending(() => go) : go());
  const showForm = (o: Open, f: ConnectorForm) =>
    leave(() => {
      setForm(f);
      setBase(f);
      setOpen(o);
      setProblems([]);
      setTested(null);
      setSchema(null);
    });
  const close = () =>
    leave(() => {
      setOpen(null);
      void refresh();
    });
  const set = <K extends keyof ConnectorForm>(k: K, v: ConnectorForm[K]) => setForm((f) => ({ ...f, [k]: v }));
  const setQuery = (i: number, q: Partial<QueryForm>) => setForm((f) => ({ ...f, queries: f.queries.map((x, j) => (j === i ? { ...x, ...q } : x)) }));

  const save = async () => {
    if (busy) return;
    setBusy(true);
    setProblems([]);
    try {
      const { connection, secretValues } = connectionFromForm(form);
      const r = await workspaces.saveConnector({ workspace, previousName: file?.name ?? "", connection, secretValues });
      if (r.problems.length || !r.connection) return setProblems(r.problems);
      snack(t("desktop.connectors.saved", { name: r.connection.name }));
      setOpen(null);
      await refresh();
    } catch (e) {
      setProblems([message(e)]);
    } finally {
      setBusy(false);
    }
  };
  // A connection on or off in this workspace (CON-17): off, the agents
  // here don't get its tools.
  const enable = async (c: Connector, on: boolean) => {
    try {
      await workspaces.setConnectorEnabled({ workspace, category: c.category, scope: c.scope, name: c.name, enabled: on });
      snack(t(on ? "desktop.connectors.enabled" : "desktop.connectors.disabled", { name: c.name }));
      await refresh();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const remove = async () => {
    setDeleting(false);
    if (!file) return;
    try {
      await workspaces.deleteConnector({ workspace, category: file.category, scope: file.scope, name: file.name });
      snack(t("desktop.connectors.deleted", { name: file.name }));
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
      const r = await workspaces.testConnector({ workspace, ...connectionFromForm(form) });
      setTested({ tools: r.tools, problem: r.problem });
    } catch (e) {
      setTested({ tools: [], problem: message(e) });
    } finally {
      setTesting(false);
    }
  };
  const readSchema = async () => {
    setReading(true);
    setSchema(null);
    try {
      const r = await workspaces.readConnectorSchema({ workspace, ...connectionFromForm(form) });
      setSchema({ tables: r.tables, problem: r.problem });
    } catch (e) {
      setSchema({ tables: [], problem: message(e) });
    } finally {
      setReading(false);
    }
  };

  const kind = kindOf(form.kind);
  const category = categoryOf(form.category);
  const prefix = category?.prefix ?? "";
  const scopes: { scope: ConnectorScope; title: string }[] = [
    { scope: "workspace", title: t("desktop.connectors.here") },
    { scope: "global", title: t("desktop.connectors.everywhere") },
  ];
  const folder = (c: ConnectorCategory | undefined, scope: ConnectorScope) => (scope === "global" ? `~/.blitz/connectors/${c?.id ?? ""}` : `.agents/connectors/${c?.id ?? ""}`);
  const title = form.name || t("desktop.connectors.new");
  const where = file?.path ?? `${folder(category, form.scope)}/${form.name.trim() || "…"}.yaml`;
  const testable = form.custom || can(kind, "test");
  return (
    <div className={`slide-pane connectors-pane ${open ? "is-open" : ""}`}>
      <aside className="slide-list" inert={open !== null}>
        <div className="row side-head">
          <span className="t-title-sm spacer">{t("desktop.studio.connectors")}</span>
        </div>
        <p className="t-body-sm muted">{t("desktop.connectors.intro", { path: ".agents/connectors/kinds" })}</p>
        {error && (
          <div className="card error row small-card">
            <Icon path={mdiAlertCircleOutline} size="sm" /> {error}
          </div>
        )}
        {usable.map((cat) => {
          const list = connections.filter((c) => c.category === cat.id);
          const first = kindsOf(cat.id)[0];
          return (
            <section key={cat.id} className="stack rule-section" style={{ gap: 4 }}>
              <div className="row" style={{ gap: 6 }}>
                <Icon path={categoryIcons[cat.icon] ?? mdiPowerPlugOutline} size="sm" />
                <span className="t-title-sm spacer">{textIn(cat.name, lang)}</span>
                <IconButton icon={mdiPlus} label={t("desktop.connectors.add_to", { category: textIn(cat.name, lang) })} small disabled={!first} onClick={() => showForm({ category: cat.id, scope: "workspace", name: "" }, newConnectorForm(cat.id, first))} />
              </div>
              <span className="t-body-sm muted">{textIn(cat.hint, lang)}</span>
              {cat.state && (
                <div className={`row small-card t-body-sm ${cat.state === "failed" ? "error-text" : "muted"}`} style={{ gap: 6 }}>
                  <Icon path={cat.state === "failed" ? mdiAlertCircleOutline : cat.state === "connected" ? mdiCheckCircleOutline : mdiPowerPlugOutline} size="sm" />
                  <span className="spacer">{cat.state === "failed" ? t("desktop.connectors.server_failed", { error: cat.error }) : cat.state === "connected" ? tn("desktop.connectors.server_connected", cat.toolCount) : t("desktop.connectors.server_idle")}</span>
                </div>
              )}
              {list.length === 0 && <p className="t-body-sm muted">{t("desktop.connectors.none")}</p>}
              <div className="list">
                {list.map((c) => {
                  const k = kindOf(c.kind);
                  const distrusted = c.scope === "workspace" && !c.trusted;
                  return (
                    <div key={c.path} className="list-item">
                      <button type="button" className="row-open" onClick={() => showForm({ category: c.category, scope: c.scope === "global" ? "global" : "workspace", name: c.name }, formFromConnection(c, k))}>
                        <Icon path={c.problem ? mdiAlertCircleOutline : !c.enabled ? mdiPowerPlugOffOutline : distrusted ? mdiShieldAlertOutline : c.readOnly ? mdiDatabaseLockOutline : (categoryIcons[cat.icon] ?? mdiPowerPlugOutline)} className={c.problem ? "error-text" : !c.enabled ? "muted" : undefined} />
                        <span className="lines spacer">
                          <span className="ellipsis">{c.name}</span>
                          <small className={`ellipsis ${c.problem ? "error-text" : ""}`}>
                            {c.problem ||
                              [
                                c.enabled ? "" : t("desktop.connectors.off_here"),
                                c.custom || !k ? t("desktop.connectors.custom") : textIn(k.name, lang),
                                c.scope === "global" ? t("desktop.connectors.everywhere") : t("desktop.connectors.here"),
                                distrusted ? t("desktop.connectors.untrusted_short") : "",
                                c.readOnly ? t("desktop.connectors.read_only") : c.custom ? "" : t("desktop.connectors.read_write"),
                                c.custom ? "" : tn("desktop.connectors.tool_count", c.tools.length + c.queries.length),
                                c.description,
                              ]
                                .filter(Boolean)
                                .join(" · ")}
                          </small>
                        </span>
                      </button>
                      <span className="trailing">
                        <Switch label={t("desktop.connectors.enable", { name: c.name })} checked={c.enabled} onChange={(on) => void enable(c, on)} />
                      </span>
                    </div>
                  );
                })}
              </div>
            </section>
          );
        })}
        {broken.length > 0 && (
          <div className="card error stack small-card" role="alert" style={{ gap: 4 }}>
            <span className="t-label">{t("desktop.connectors.problems")}</span>
            {broken.map((d) => (
              <span key={`${d.origin}:${d.id}:${d.path}`} className="row t-body-sm" style={{ gap: 6 }} title={d.path}>
                <Icon path={mdiAlertCircleOutline} size="sm" />
                <span className="mono">{d.id}</span>
                <span className="spacer">{d.problem}</span>
              </span>
            ))}
          </div>
        )}
      </aside>
      <section className="slide-detail" inert={open === null}>
        {open && (
          <>
            <div className="row slide-back">
              <IconButton icon={mdiArrowLeft} label={t("desktop.connectors.back")} onClick={close} />
              <h2 className="t-headline spacer ellipsis">{title}</h2>
              {dirty && <span className="t-body-sm muted">{t("desktop.agents.unsaved")}</span>}
            </div>
            {untrusted && (
              <div className="card outlined row small-card t-body-sm" style={{ gap: 6 }}>
                <Icon path={mdiShieldAlertOutline} size="sm" />
                <span className="spacer">{t("desktop.connectors.untrusted")}</span>
              </div>
            )}
            <fieldset className="worker agent-form rule-form">
              <div className="pair">
                <Field label={t("desktop.connectors.name")} supporting={t("desktop.connectors.name_hint", { prefix, name: form.name.trim() || "shop" })}>
                  {(id) => <input id={id} className="input mono" value={form.name} onChange={(e) => set("name", e.target.value)} />}
                </Field>
                <Field label={t("desktop.connectors.description")} supporting={t("desktop.connectors.description_hint")}>
                  {(id) => <input id={id} className="input" value={form.description} disabled={form.custom} onChange={(e) => set("description", e.target.value)} />}
                </Field>
              </div>
              <div className="stack" style={{ gap: 6 }}>
                <span className="t-label">{t("desktop.connectors.scope")}</span>
                <Segmented value={form.scope} label={t("desktop.connectors.scope")} options={scopes.map((s) => ({ value: s.scope, label: s.title }))} onChange={(v) => set("scope", v)} />
                <span className="t-body-sm muted">{t(`desktop.connectors.scope_hint.${form.scope}`, { path: folder(category, form.scope) })}</span>
              </div>
              {form.custom ? (
                <Field label={t("desktop.connectors.yaml")} supporting={t("desktop.connectors.custom_hint")}>
                  {(id) => <textarea id={id} className="input mono" rows={16} value={form.text} onChange={(e) => set("text", e.target.value)} />}
                </Field>
              ) : (
                <>
                  <div className="stack" style={{ gap: 6 }}>
                    <span className="t-label">{t("desktop.connectors.kind")}</span>
                    <Segmented value={form.kind} label={t("desktop.connectors.kind")} options={kindsOf(form.category).map((k) => ({ value: k.id, label: textIn(k.name, lang) }))} onChange={(v) => setForm((f) => withKind(f, kindOf(v)!))} />
                    {kind && <span className="t-body-sm muted">{textIn(kind.hint, lang)}</span>}
                  </div>
                  {kind && (
                    <div className="db-fields">
                      {kind.fields.map((fl) =>
                        fl.type === "secret" ? (
                          <Field key={fl.key} label={textIn(fl.label, lang) + (fl.required ? " *" : "")} supporting={form.secretsSet.includes(fl.key) ? t("desktop.connectors.secret_kept") : t("desktop.connectors.secret_hint")}>
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
                          <Field key={fl.key} label={textIn(fl.label, lang) + (fl.required && !fl.defaultValue ? " *" : "")} supporting={textIn(fl.help, lang)}>
                            {(id) => (
                              <input
                                id={id}
                                className={`input ${fl.type === "path" ? "mono" : ""}`}
                                inputMode={fl.type === "number" ? "numeric" : undefined}
                                value={form.settings[fl.key] ?? ""}
                                placeholder={fl.defaultValue}
                                onChange={(e) => set("settings", { ...form.settings, [fl.key]: e.target.value })}
                              />
                            )}
                          </Field>
                        ),
                      )}
                    </div>
                  )}
                  {kind?.canReadOnly && (
                    <label className="row agent-switch">
                      <Switch label={t("desktop.connectors.read_only")} checked={form.readOnly} onChange={(v) => set("readOnly", v)} />
                      <span className="stack" style={{ gap: 0 }}>
                        <span className="t-label">{t("desktop.connectors.read_only")}</span>
                        <span className="t-body-sm muted">{form.readOnly ? textIn(kind.readOnlyHint, lang) : t("desktop.connectors.read_write_hint")}</span>
                      </span>
                    </label>
                  )}
                  {kind && kind.tools.length > 0 && (
                    <div className="stack" style={{ gap: 6 }}>
                      <span className="t-label">{t("desktop.connectors.tools")}</span>
                      <span className="t-body-sm muted">{t("desktop.connectors.tools_hint", { prefix, name: form.name.trim() || "shop" })}</span>
                      <div className="list">
                        {kind.tools.map((rt) => {
                          const hidden = rt.writes && form.readOnly;
                          return (
                            <label key={rt.id} className="list-item row agent-switch">
                              <Switch label={rt.id} checked={form.tools.includes(rt.id)} onChange={(v) => set("tools", v ? [...form.tools, rt.id] : form.tools.filter((x) => x !== rt.id))} />
                              <span className="stack spacer" style={{ gap: 0 }}>
                                <span className="mono">{rt.id}</span>
                                <span className="t-body-sm muted">{hidden ? t("desktop.connectors.tool_hidden") : textIn(rt.about, lang)}</span>
                              </span>
                            </label>
                          );
                        })}
                      </div>
                    </div>
                  )}
                  {kind && can(kind, "queries") && (
                    <div className="stack" style={{ gap: 6 }}>
                      <div className="row wrap" style={{ gap: 8 }}>
                        <span className="t-label spacer">{t("desktop.connectors.queries")}</span>
                        {can(kind, "query-builder") && (
                          <Button small icon={mdiCreationOutline} disabled={!form.name.trim()} title={t("desktop.connectors.builder_hint")} onClick={() => setBuilding(true)}>
                            {t("desktop.connectors.builder")}
                          </Button>
                        )}
                        <Button icon={mdiPlus} onClick={() => set("queries", [...form.queries, { name: "", description: "", statement: "", params: [] }])}>
                          {t("desktop.connectors.add_query")}
                        </Button>
                      </div>
                      <span className="t-body-sm muted">{textIn(kind.queriesHint, lang)}</span>
                      {form.queries.map((q, i) => (
                        <QueryCard
                          key={i}
                          workspace={workspace}
                          form={form}
                          kind={kind}
                          prefix={prefix}
                          query={q}
                          onChange={(x) => setQuery(i, x)}
                          onRemove={() => set("queries", form.queries.filter((_, j) => j !== i))}
                        />
                      ))}
                    </div>
                  )}
                </>
              )}
            </fieldset>
            {testable && (
              <div className="card outlined stack rule-try" style={{ gap: 8 }}>
                <div className="row wrap" style={{ gap: 8 }}>
                  <span className="stack spacer" style={{ gap: 0 }}>
                    <span className="t-title-sm">{t("desktop.connectors.test")}</span>
                    <span className="t-body-sm muted">{t("desktop.connectors.test_hint")}</span>
                  </span>
                  {can(kind, "schema") && (
                    <Button icon={reading ? mdiLoading : mdiTableLarge} iconSpin={reading} disabled={reading || !form.name.trim()} onClick={() => void readSchema()}>
                      {t("desktop.connectors.schema_button")}
                    </Button>
                  )}
                  <Button variant="tonal" icon={testing ? mdiLoading : mdiLanConnect} iconSpin={testing} disabled={testing || !form.name.trim()} onClick={() => void test()}>
                    {testing ? t("desktop.connectors.testing") : t("desktop.connectors.test_button")}
                  </Button>
                </div>
                {tested && (
                  <div className={`stack small-card ${tested.problem ? "error-text" : ""}`} style={{ gap: 4 }}>
                    <span className="row t-body-sm" style={{ gap: 6 }}>
                      <Icon path={tested.problem ? mdiCloseCircleOutline : mdiCheckCircleOutline} size="sm" />
                      {tested.problem ? t("desktop.connectors.test_failed", { problem: tested.problem }) : tn("desktop.connectors.test_ok", tested.tools.length)}
                    </span>
                    {tested.tools.map((tl) => (
                      <span key={tl.name} className="row t-body-sm" style={{ gap: 6 }} title={tl.description}>
                        <span className="mono">{agentTool(prefix, tl.name)}</span>
                        <span className="muted ellipsis spacer">{tl.description}</span>
                        <span className="muted">{tl.readOnly ? t("desktop.connectors.runs_unasked") : t("desktop.connectors.asks_first")}</span>
                      </span>
                    ))}
                  </div>
                )}
                {schema && (
                  <div className={`stack small-card ${schema.problem ? "error-text" : ""}`} style={{ gap: 4 }}>
                    <span className="row t-body-sm" style={{ gap: 6 }}>
                      <Icon path={schema.problem ? mdiCloseCircleOutline : mdiTableLarge} size="sm" />
                      {schema.problem ? t("desktop.connectors.schema_failed", { problem: schema.problem }) : tn("desktop.connectors.schema_count", schema.tables.length)}
                    </span>
                    {schema.tables.map((tb) => (
                      <span key={tb.name} className="row t-body-sm" style={{ gap: 6 }} title={tb.about}>
                        <span className="mono">{tb.name}</span>
                        <span className="muted ellipsis spacer">{tb.columns.map((c) => c.name).join(", ")}</span>
                      </span>
                    ))}
                  </div>
                )}
              </div>
            )}
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
              <Button variant="filled" icon={mdiContentSaveOutline} disabled={busy || !form.name.trim() || (!!file && !dirty && !isNew && !untrusted)} onClick={() => void save()}>
                {untrusted && !dirty ? t("desktop.connectors.trust_save") : t("desktop.rules.save")}
              </Button>
            </div>
          </>
        )}
      </section>
      {building && kind && (
        <QueryBuilder
          workspace={workspace}
          form={form}
          prefix={prefix}
          onClose={() => setBuilding(false)}
          onUse={(q) => {
            set("queries", [...form.queries, q]);
            setBuilding(false);
          }}
        />
      )}
      {deleting &&
        file &&
        createPortal(
          <Dialog
            title={t("desktop.connectors.delete_title", { name: file.name })}
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
            <p>{t("desktop.connectors.delete_body", { path: file.path })}</p>
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

/**
 * The query builder (the query-builder capability): the user says what
 * they're looking for; the service reads the connection's tables, failing
 * when it can read none, and the sql-query skill writes a query. The draft
 * is shown with its notes, and Use adds it to the form, unsaved.
 */
function QueryBuilder({ workspace, form, prefix, onClose, onUse }: { workspace: string; form: ConnectorForm; prefix: string; onClose: () => void; onUse: (q: QueryForm) => void }) {
  const [request, setRequest] = useState("");
  const [busy, setBusy] = useState(false);
  const [draft, setDraft] = useState<{ query: QueryForm; notes: string[]; tables: number } | null>(null);
  const [error, setError] = useState("");
  const ask = async () => {
    setBusy(true);
    setError("");
    try {
      const r = await workspaces.draftConnectorQuery({ workspace, ...connectionFromForm(form), request: request.trim() });
      if (r.problem || !r.query) setError(r.problem);
      else setDraft({ query: queryForm(r.query), notes: r.notes, tables: r.tables });
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  };
  return createPortal(
    <Dialog
      title={t("desktop.connectors.builder")}
      icon={mdiCreationOutline}
      onClose={onClose}
      wide
      footer={
        draft ? (
          <>
            <Button onClick={() => setDraft(null)}>{t("desktop.draft.again")}</Button>
            <Button variant="filled" onClick={() => onUse(draft.query)}>
              {t("desktop.draft.use")}
            </Button>
          </>
        ) : (
          <>
            <Button onClick={onClose}>{t("desktop.cancel")}</Button>
            <Button variant="filled" icon={busy ? mdiLoading : mdiCreationOutline} iconSpin={busy} disabled={busy || !request.trim()} onClick={() => void ask()}>
              {busy ? t("desktop.connectors.builder_busy") : t("desktop.connectors.builder_button")}
            </Button>
          </>
        )
      }
    >
      {draft ? (
        <div className="stack" style={{ gap: 12 }}>
          <span className="t-body-sm">{tn("desktop.connectors.builder_done", draft.tables, { name: agentTool(prefix, `${form.name.trim()}_${draft.query.name}`) })}</span>
          {draft.notes.length > 0 && (
            <ul className="t-body-sm draft-changes">
              {draft.notes.map((n) => (
                <li key={n}>{n}</li>
              ))}
            </ul>
          )}
          <span className="t-body-sm muted">{draft.query.description}</span>
          <textarea className="input mono draft-text" readOnly value={draft.query.statement} rows={8} />
          {draft.query.params.map((p) => (
            <span key={p.name} className="row t-body-sm" style={{ gap: 6 }}>
              <span className="mono">{p.name}</span>
              <span className="muted">{t(`desktop.connectors.param_type.${p.type}`)}</span>
              <span className="muted ellipsis spacer">{p.description}</span>
            </span>
          ))}
        </div>
      ) : (
        <div className="stack" style={{ gap: 8 }}>
          <span className="t-body-sm muted">{t("desktop.connectors.builder_hint")}</span>
          <label className="field">
            <span className="t-label">{t("desktop.draft.request")}</span>
            <textarea className="input" rows={3} autoFocus value={request} placeholder={t("desktop.connectors.builder_placeholder")} onChange={(e) => setRequest(e.target.value)} />
          </label>
          {error && (
            <div className="card error row small-card t-body-sm" role="alert" style={{ gap: 6 }}>
              <Icon path={mdiAlertCircleOutline} size="sm" /> {t("desktop.connectors.builder_failed", { problem: error })}
            </div>
          )}
        </div>
      )}
    </Dialog>,
    document.body,
  );
}

/** A connection's own query: its name, what it's for, its statement and parameters, and Try it on the connection as written. */
function QueryCard({
  workspace,
  form,
  kind,
  prefix,
  query,
  onChange,
  onRemove,
}: {
  workspace: string;
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
      const r = await workspaces.tryConnectorTool({ workspace, ...connectionFromForm(form), tool: query.name.trim(), argsJson: JSON.stringify(values) });
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
            {trying ? t("desktop.connectors.trying") : t("desktop.rules.try")}
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
