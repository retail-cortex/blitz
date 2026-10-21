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

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  mdiAlertCircleOutline,
  mdiArrowLeft,
  mdiCheckCircleOutline,
  mdiCloseCircleOutline,
  mdiConnection,
  mdiContentSaveOutline,
  mdiDatabaseLockOutline,
  mdiDatabaseOutline,
  mdiDeleteOutline,
  mdiEarth,
  mdiLanConnect,
  mdiLoading,
  mdiPlus,
  mdiPowerPlugOffOutline,
  mdiShieldAlertOutline,
  mdiTableLarge,
  mdiUndoVariant,
} from "@mdi/js";
import { workspaces } from "../api";
import { openSettings } from "../events";
import { message } from "../errors";
import { language, t, tn, useLanguage } from "../i18n";
import type { Connector, ConnectorCategory, ConnectorKind, ConnectorTable, ConnectorTool } from "../gen/blitz/v1/workspace_pb";
import { Button, Icon, IconButton, Switch, useSnackbar } from "../ui/controls";
import { agentTool, globalApi, workspaceApi } from "./connectorApi";
import { ConnectorFields } from "./ConnectorFields";
import { can, connectionFromForm, formFromConnection, newConnectorForm, sameConnectorForm, textIn, type ConnectorForm, type ConnectorScope } from "./connectorForm";
import { ConfirmDialog, ProblemList, useDiscardGuard, useFormRequest, useFormStamp } from "./paneParts";
import { QueryBuilder } from "./QueryBuilder";

/** A category's icon, by the name its descriptor gives. */
const categoryIcons: Record<string, string> = { database: mdiDatabaseOutline };

/** A category's icon (the connectors' when its descriptor names none Blitz has). */
const iconOf = (c: ConnectorCategory | undefined) => categoryIcons[c?.icon ?? ""] ?? mdiConnection;

/** What a connection is open as: one by category, scope and name, or a new one (name ""). */
type Open = { category: string; scope: ConnectorScope; name: string } | null;

/** The dialog over the pane, if any. */
type DialogOpen = "delete" | "promote" | "build" | null;

/**
 * The connectors the agents reach (spec_connectors_047), by category
 * (Databases, served by MCP Toolbox for Databases inside Blitz). With a
 * workspace it's Studio › Connectors: the workspace's connections, which
 * it changes and can make global, and the global ones, shown as they are
 * with a switch turning each on or off here (CON-17). Without one it's
 * Settings › Connectors: the global connections, changed there alone
 * (CON-18). A connection's form (ConnectorFields) slides in over the
 * list, with Test and Read the tables as its kind can. A reply that
 * lands after the form it was asked for closed is dropped.
 */
export function ConnectorsPane({ workspace, onDirty }: { workspace?: string; onDirty?: (dirty: boolean) => void }) {
  useLanguage();
  const api = useMemo(() => (workspace ? workspaceApi(workspace) : globalApi), [workspace]);
  const studio = !api.global;
  const scope: ConnectorScope = studio ? "workspace" : "global";
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
  const [dialog, setDialog] = useState<DialogOpen>(null);
  const stamps = useFormStamp();
  const tested = useFormRequest<{ tools: ConnectorTool[]; problem: string }>(stamps, (e) => ({ tools: [], problem: message(e) }));
  const schema = useFormRequest<{ tables: ConnectorTable[]; problem: string }>(stamps, (e) => ({ tables: [], problem: message(e) }));

  const refresh = useCallback(async () => {
    try {
      const r = await api.list();
      setCategories(r.categories);
      setKinds(r.kinds);
      setConnections(r.connections);
      setError("");
    } catch (e) {
      setError(message(e));
    }
  }, [api]);
  useEffect(() => void refresh(), [refresh]);

  const usable = categories.filter((c) => !c.problem);
  const kindsOf = (category: string) => kinds.filter((k) => k.category === category && !k.problem);
  const kindOf = (id: string) => kinds.find((k) => k.id === id && !k.problem);
  const categoryOf = (id: string) => categories.find((c) => c.id === id);
  const broken = [...categories.filter((c) => c.problem), ...kinds.filter((k) => k.problem)];
  const file = open && open.name ? connections.find((c) => c.category === open.category && c.scope === open.scope && c.name === open.name) : undefined;
  const untrusted = !!file && file.scope === "workspace" && !file.trusted;
  // A global connection open in Studio: shown as it is, changed in Settings.
  const shown = studio && !!file && file.scope === "global";
  const isNew = open !== null && open.name === "";
  const dirty = open !== null && !sameConnectorForm(form, base);
  useEffect(() => onDirty?.(dirty), [dirty, onDirty]);

  const { leave, discard } = useDiscardGuard(dirty);
  // Another form, or none: replies to the one before are dropped.
  const switchTo = (o: Open, f: ConnectorForm) => {
    stamps.next();
    tested.reset();
    schema.reset();
    setForm(f);
    setBase(f);
    setOpen(o);
    setProblems([]);
    setBusy(false);
    setDialog(null);
  };
  const showForm = (o: Open, f: ConnectorForm) => leave(() => switchTo(o, f));
  const close = () =>
    leave(() => {
      switchTo(null, newConnectorForm("", undefined));
      void refresh();
    });
  // Done with the form this was asked for, when it's still open: back to the list.
  const done = (s: number, said: string) => {
    if (!stamps.current(s)) return;
    snack(said);
    switchTo(null, newConnectorForm("", undefined));
    void refresh();
  };

  const save = async () => {
    if (busy) return;
    const s = stamps.stamp();
    setBusy(true);
    setProblems([]);
    try {
      const { connection, secretValues } = connectionFromForm(form);
      const r = await api.save({ previousName: file?.name ?? "", connection, secretValues });
      if (!stamps.current(s)) return void refresh();
      if (r.problems.length || !r.connection) return setProblems(r.problems);
      done(s, t("desktop.connectors.saved", { name: r.connection.name }));
    } catch (e) {
      if (stamps.current(s)) setProblems([message(e)]);
    } finally {
      if (stamps.current(s)) setBusy(false);
    }
  };
  // A connection on or off in this workspace (CON-17): off, the agents
  // here don't get its tools.
  const enable = async (c: Connector, on: boolean) => {
    try {
      await workspaces.setConnectorEnabled({ workspace: workspace ?? "", category: c.category, scope: c.scope, name: c.name, enabled: on });
      snack(t(on ? "desktop.connectors.enabled" : "desktop.connectors.disabled", { name: c.name }));
      await refresh();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const remove = async () => {
    setDialog(null);
    if (!file) return;
    const s = stamps.stamp();
    try {
      await api.remove(file);
      done(s, t("desktop.connectors.deleted", { name: file.name }));
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  // Make Global (CON-19): the workspace's connection, with its secrets,
  // moves to the global folder, and Settings changes it from then on.
  const promote = async () => {
    setDialog(null);
    if (!file || !workspace) return;
    const s = stamps.stamp();
    try {
      const r = await workspaces.makeConnectorGlobal({ workspace, category: file.category, name: file.name });
      if (r.problems.length || !r.connection) {
        if (stamps.current(s)) setProblems(r.problems);
        return;
      }
      done(s, t("desktop.connectors.made_global", { name: file.name }));
    } catch (e) {
      if (stamps.current(s)) setProblems([message(e)]);
    }
  };

  const kind = kindOf(form.kind);
  const category = categoryOf(form.category);
  const prefix = category?.prefix ?? "";
  // The folders the service says the category's files are in.
  const folder = (sc: ConnectorScope) => (sc === "global" ? category?.globalDir : category?.workspaceDir) ?? "";
  const title = form.name || t("desktop.connectors.new");
  const where = file?.path ?? `${folder(form.scope)}/${form.name.trim() || "…"}.yaml`;
  const testable = !shown && (form.custom || can(kind, "test"));
  return (
    <div className={`slide-pane connectors-pane ${open ? "is-open" : ""}`}>
      <aside className="slide-list" inert={open !== null}>
        {studio && (
          <div className="row side-head">
            <span className="t-title-sm spacer">{t("desktop.studio.connectors")}</span>
          </div>
        )}
        <p className="t-body-sm muted">{studio ? t("desktop.connectors.intro", { path: ".agents/connectors/kinds" }) : t("desktop.connectors.global_intro", { path: "~/.blitz/connectors/kinds" })}</p>
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
                <Icon path={iconOf(cat)} size="sm" />
                <span className="t-title-sm spacer">{textIn(cat.name, lang)}</span>
                <IconButton icon={mdiPlus} label={t("desktop.connectors.add_to", { category: textIn(cat.name, lang) })} small disabled={!first} onClick={() => showForm({ category: cat.id, scope, name: "" }, newConnectorForm(cat.id, first, scope))} />
              </div>
              <span className="t-body-sm muted">{textIn(cat.hint, lang)}</span>
              {cat.state && (
                <div className={`row small-card t-body-sm ${cat.state === "failed" ? "error-text" : "muted"}`} style={{ gap: 6 }}>
                  <Icon path={cat.state === "failed" ? mdiAlertCircleOutline : cat.state === "connected" ? mdiCheckCircleOutline : mdiConnection} size="sm" />
                  <span className="spacer">{cat.state === "failed" ? t("desktop.connectors.server_failed", { error: cat.error }) : cat.state === "connected" ? tn("desktop.connectors.server_connected", cat.toolCount) : t("desktop.connectors.server_idle")}</span>
                </div>
              )}
              {list.length === 0 && <p className="t-body-sm muted">{t("desktop.connectors.none")}</p>}
              <div className="list">
                {list.map((c) => (
                  <ConnectionRow key={c.path} c={c} kind={kindOf(c.kind)} icon={iconOf(cat)} studio={studio} onOpen={() => showForm({ category: c.category, scope: c.scope === "global" ? "global" : "workspace", name: c.name }, formFromConnection(c, kindOf(c.kind)))} onEnable={(on) => void enable(c, on)} />
                ))}
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
            {shown && (
              <div className="card outlined row small-card t-body-sm" style={{ gap: 6 }}>
                <Icon path={mdiEarth} size="sm" />
                <span className="spacer">{t("desktop.connectors.global_shown")}</span>
              </div>
            )}
            {untrusted && (
              <div className="card outlined row small-card t-body-sm" style={{ gap: 6 }}>
                <Icon path={mdiShieldAlertOutline} size="sm" />
                <span className="spacer">{t("desktop.connectors.untrusted")}</span>
              </div>
            )}
            <fieldset className="worker agent-form rule-form" disabled={shown}>
              <ConnectorFields api={api} form={form} setForm={setForm} kind={kind} kinds={kindsOf(form.category)} prefix={prefix} folder={folder(form.scope)} onBuild={() => setDialog("build")} />
            </fieldset>
            {testable && (
              <div className="card outlined stack rule-try" style={{ gap: 8 }}>
                <div className="row wrap" style={{ gap: 8 }}>
                  <span className="stack spacer" style={{ gap: 0 }}>
                    <span className="t-title-sm">{t("desktop.connectors.test")}</span>
                    <span className="t-body-sm muted">{t("desktop.connectors.test_hint")}</span>
                  </span>
                  {can(kind, "schema") && (
                    <Button icon={schema.busy ? mdiLoading : mdiTableLarge} iconSpin={schema.busy} disabled={schema.busy || !form.name.trim()} onClick={() => void schema.run(() => api.readSchema(connectionFromForm(form)))}>
                      {t("desktop.connectors.schema_button")}
                    </Button>
                  )}
                  <Button variant="tonal" icon={tested.busy ? mdiLoading : mdiLanConnect} iconSpin={tested.busy} disabled={tested.busy || !form.name.trim()} onClick={() => void tested.run(() => api.test(connectionFromForm(form)))}>
                    {tested.busy ? t("desktop.connectors.testing") : t("desktop.connectors.test_button")}
                  </Button>
                </div>
                {tested.result && (
                  <Outcome
                    problem={tested.result.problem}
                    icon={mdiCheckCircleOutline}
                    said={tested.result.problem ? t("desktop.connectors.test_failed", { problem: tested.result.problem }) : tn("desktop.connectors.test_ok", tested.result.tools.length)}
                    rows={tested.result.tools.map((tl) => ({ key: tl.name, title: tl.description, name: agentTool(prefix, tl.name), detail: tl.description, end: tl.readOnly ? t("desktop.connectors.runs_unasked") : t("desktop.connectors.asks_first") }))}
                  />
                )}
                {schema.result && (
                  <Outcome
                    problem={schema.result.problem}
                    icon={mdiTableLarge}
                    said={schema.result.problem ? t("desktop.connectors.schema_failed", { problem: schema.result.problem }) : tn("desktop.connectors.schema_count", schema.result.tables.length)}
                    rows={schema.result.tables.map((tb) => ({ key: tb.name, title: tb.about, name: tb.name, detail: tb.columns.map((c) => c.name).join(", ") }))}
                  />
                )}
              </div>
            )}
            <ProblemList problems={problems} />
            <div className="row wrap agent-actions slide-actions">
              <span className="t-body-sm muted spacer ellipsis" title={where}>
                {t("desktop.agents.where", { path: where })}
              </span>
              {shown ? (
                <>
                  <Button onClick={close}>{t("desktop.connectors.close")}</Button>
                  <Button variant="filled" icon={mdiEarth} onClick={() => openSettings({ section: "connectors" })}>
                    {t("desktop.connectors.edit_in_settings")}
                  </Button>
                </>
              ) : (
                <>
                  {file && (
                    <Button danger icon={mdiDeleteOutline} disabled={busy} onClick={() => setDialog("delete")}>
                      {t("desktop.connectors.delete")}
                    </Button>
                  )}
                  {studio && file && (
                    <Button
                      icon={mdiEarth}
                      disabled={busy || dirty || untrusted}
                      title={dirty ? t("desktop.connectors.make_global_save_first") : untrusted ? t("desktop.connectors.make_global_trust_first") : t("desktop.connectors.make_global_hint")}
                      onClick={() => setDialog("promote")}
                    >
                      {t("desktop.connectors.make_global")}
                    </Button>
                  )}
                  {dirty && file && (
                    <Button icon={mdiUndoVariant} disabled={busy} onClick={() => setForm(base)}>
                      {t("desktop.agents.revert")}
                    </Button>
                  )}
                  <Button disabled={busy} onClick={close}>
                    {t("desktop.cancel")}
                  </Button>
                  <Button variant="filled" icon={busy ? mdiLoading : mdiContentSaveOutline} iconSpin={busy} disabled={busy || !form.name.trim() || (!!file && !dirty && !isNew && !untrusted)} onClick={() => void save()}>
                    {untrusted && !dirty ? t("desktop.connectors.trust_save") : t("desktop.connectors.save")}
                  </Button>
                </>
              )}
            </div>
          </>
        )}
      </section>
      {dialog === "build" && kind && (
        <QueryBuilder
          api={api}
          form={form}
          prefix={prefix}
          onClose={() => setDialog(null)}
          onUse={(q) => {
            setForm((f) => ({ ...f, queries: [...f.queries, q] }));
            setDialog(null);
          }}
        />
      )}
      {dialog === "promote" && file && (
        <ConfirmDialog title={t("desktop.connectors.make_global_title", { name: file.name })} icon={mdiEarth} confirm={t("desktop.connectors.make_global")} danger={false} onConfirm={() => void promote()} onClose={() => setDialog(null)}>
          <p>{t("desktop.connectors.make_global_body", { name: file.name, path: folder("global") })}</p>
        </ConfirmDialog>
      )}
      {dialog === "delete" && file && (
        <ConfirmDialog title={t("desktop.connectors.delete_title", { name: file.name })} icon={mdiDeleteOutline} confirm={t("desktop.connectors.delete")} onConfirm={() => void remove()} onClose={() => setDialog(null)}>
          <p>{t("desktop.connectors.delete_body", { path: file.path })}</p>
        </ConfirmDialog>
      )}
      {discard(title)}
    </div>
  );
}

/** A connection in the list: its kind, scope, trust, read-only or not, tools and description; its switch in Studio. */
function ConnectionRow({ c, kind, icon, studio, onOpen, onEnable }: { c: Connector; kind: ConnectorKind | undefined; icon: string; studio: boolean; onOpen: () => void; onEnable: (on: boolean) => void }) {
  const lang = language();
  const distrusted = c.scope === "workspace" && !c.trusted;
  const detail = [
    c.enabled ? "" : t("desktop.connectors.off_here"),
    c.custom || !kind ? t("desktop.connectors.custom") : textIn(kind.name, lang),
    studio ? (c.scope === "global" ? t("desktop.connectors.everywhere") : t("desktop.connectors.here")) : "",
    distrusted ? t("desktop.connectors.untrusted_short") : "",
    c.readOnly ? t("desktop.connectors.read_only") : c.custom ? "" : t("desktop.connectors.read_write"),
    c.custom ? "" : tn("desktop.connectors.tool_count", c.tools.length + c.queries.length),
    c.description,
  ]
    .filter(Boolean)
    .join(" · ");
  return (
    <div className="list-item">
      <button type="button" className="row-open" onClick={onOpen}>
        <Icon path={c.problem ? mdiAlertCircleOutline : !c.enabled ? mdiPowerPlugOffOutline : distrusted ? mdiShieldAlertOutline : c.readOnly ? mdiDatabaseLockOutline : icon} className={c.problem ? "error-text" : !c.enabled ? "muted" : undefined} />
        <span className="lines spacer">
          <span className="ellipsis">{c.name}</span>
          <small className={`ellipsis ${c.problem ? "error-text" : ""}`}>{c.problem || detail}</small>
        </span>
      </button>
      {studio && (
        <span className="trailing">
          <Switch label={t("desktop.connectors.enable", { name: c.name })} checked={c.enabled} onChange={onEnable} />
        </span>
      )}
    </div>
  );
}

/** A test's or schema read's outcome: what it found, a row each, or why it couldn't. */
function Outcome({ problem, icon, said, rows }: { problem: string; icon: string; said: string; rows: { key: string; title: string; name: string; detail: string; end?: string }[] }) {
  return (
    <div className={`stack small-card ${problem ? "error-text" : ""}`} style={{ gap: 4 }}>
      <span className="row t-body-sm" style={{ gap: 6 }}>
        <Icon path={problem ? mdiCloseCircleOutline : icon} size="sm" />
        {said}
      </span>
      {rows.map((r) => (
        <span key={r.key} className="row t-body-sm" style={{ gap: 6 }} title={r.title}>
          <span className="mono">{r.name}</span>
          <span className="muted ellipsis spacer">{r.detail}</span>
          {r.end && <span className="muted">{r.end}</span>}
        </span>
      ))}
    </div>
  );
}
