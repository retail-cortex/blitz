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
import { createPortal } from "react-dom";
import { mdiCreationOutline, mdiDatabaseCogOutline, mdiDatabaseOutline, mdiDatabasePlusOutline, mdiLoading, mdiPencilOutline, mdiPlay, mdiTableSearch } from "@mdi/js";
import { Button, Dialog, Field, IconButton, Menu, useSnackbar, type MenuEntry } from "@blitz/webui";
import { workspaces } from "./api";
import { databaseBlock, queryBlock, type WebBlock } from "./attachments";
import { DataTableView } from "./DataTableView";
import { message } from "./errors";
import { onDataMenu } from "./events";
import type { Connector, ConnectorQuery, DataTable } from "./gen/blitz/v1/workspace_pb";
import { t } from "./i18n";
import { paramValue } from "./studio/workflowForm";

/** The most rows a chat's query adds (spec_059_data DAT-42). */
const chatRows = 200;

/** SQL's start, as a placeholder: code, not text to translate. */
const sqlPlaceholder = "SELECT …";

/** The connections a chat may use: databases on here, readable, and trusted when they're the workspace's own (DAT-44). */
export const chatConnections = (list: Connector[]) => list.filter((c) => c.category === "databases" && c.enabled && !c.problem && c.trusted);

/** What the query dialog runs: a saved query (its parameters asked for), or SQL written or described. */
type Asking = { connection: string; query?: ConnectorQuery; describe?: boolean };

/**
 * The chat box's database menu (spec_059_data DAT-40): the workspace's
 * connections, each with its saved queries, then Write SQL… and Describe
 * what you need…, and Connect a database… at the end. A connection is
 * added as it is (its schema goes with the next message); a query runs
 * now, read-only, and its result is added. /data opens it.
 */
export function DataMenu({ dir, onAdd, onConnect }: { dir: string; onAdd: (b: WebBlock) => void; onConnect: () => void }) {
  const [connections, setConnections] = useState<Connector[]>([]);
  const [asking, setAsking] = useState<Asking | null>(null);
  const [problem, setProblem] = useState("");
  const anchor = useRef<HTMLSpanElement>(null);
  const snack = useSnackbar();

  const load = () =>
    workspaces.listConnectors({ workspace: dir }).then(
      (r) => (setConnections(chatConnections(r.connections)), setProblem("")),
      (e) => setProblem(message(e)),
    );
  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dir]);
  useEffect(() => onDataMenu(dir, () => anchor.current?.querySelector("button")?.click()), [dir]);

  // A saved query without parameters runs at once; one with them asks for them first.
  const pick = async (c: Connector, q: ConnectorQuery) => {
    if (q.params.length) return setAsking({ connection: c.name, query: q });
    try {
      const r = await workspaces.runConnectorQuery({ workspace: dir, connection: c, query: q.name, maxRows: chatRows, readOnly: true });
      if (r.table) onAdd(queryBlock(c.name, q.name, q.statement, r.table));
      else snack(r.problem, { error: true });
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const items: MenuEntry[] = [];
  if (problem) items.push({ heading: problem });
  for (const c of connections) {
    items.push({ heading: c.name });
    items.push({ label: t("desktop.data.add_connection", { name: c.name }), detail: t("desktop.data.add_connection_detail", { kind: c.kind }), icon: mdiDatabaseOutline, onSelect: () => onAdd(databaseBlock(c.name)) });
    for (const q of c.queries) items.push({ label: q.name, detail: q.description, icon: mdiTableSearch, onSelect: () => void pick(c, q) });
  }
  if (connections.length) {
    items.push("divider");
    items.push({ label: t("desktop.data.write_sql"), icon: mdiPencilOutline, onSelect: () => setAsking({ connection: connections[0].name }) });
    items.push({ label: t("desktop.data.describe"), icon: mdiCreationOutline, onSelect: () => setAsking({ connection: connections[0].name, describe: true }) });
    items.push("divider");
  } else if (!problem) {
    items.push({ heading: t("desktop.data.none") });
  }
  items.push({ label: t("desktop.data.connect"), icon: mdiDatabaseCogOutline, onSelect: onConnect });

  return (
    <>
      <span ref={anchor} onPointerDown={() => void load()}>
        <Menu placement="up start" className="wide-menu" trigger={(p) => <IconButton title="" icon={mdiDatabasePlusOutline} label={t("desktop.composer.connect_data")} small {...p} />} items={items} />
      </span>
      {asking &&
        createPortal(
        <QueryDialog
          dir={dir}
          connections={connections}
          asking={asking}
          onClose={() => setAsking(null)}
          onAdd={(b) => {
            onAdd(b);
            setAsking(null);
          }}
        />,
          document.body,
        )}
    </>
  );
}

/**
 * A query to add to the chat (DAT-42): a saved one with its parameters,
 * or SQL, written or drafted from a description and shown for review. It
 * runs read-only; its first rows show before it's added.
 */
function QueryDialog({ dir, connections, asking, onClose, onAdd }: { dir: string; connections: Connector[]; asking: Asking; onClose: () => void; onAdd: (b: WebBlock) => void }) {
  const [connection, setConnection] = useState(asking.connection);
  const [sql, setSql] = useState("");
  const [describe, setDescribe] = useState("");
  const [args, setArgs] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState<"" | "run" | "draft">("");
  const [result, setResult] = useState<{ table?: DataTable; problem: string } | null>(null);
  const conn = connections.find((c) => c.name === connection);
  const saved = asking.query;
  const statement = saved ? saved.statement : sql;

  const run = async () => {
    if (!conn) return;
    setBusy("run");
    try {
      const r = saved
        ? await workspaces.runConnectorQuery({ workspace: dir, connection: conn, query: saved.name, paramsJson: JSON.stringify(Object.fromEntries(saved.params.map((p) => [p.name, paramValue(args[p.name] ?? "")]))), maxRows: chatRows, readOnly: true })
        : await workspaces.runConnectorQuery({ workspace: dir, connection: conn, statement: sql, maxRows: chatRows, readOnly: true });
      setResult({ table: r.table, problem: r.problem });
    } catch (e) {
      setResult({ problem: message(e) });
    } finally {
      setBusy("");
    }
  };
  const draft = async () => {
    if (!conn) return;
    setBusy("draft");
    try {
      const r = await workspaces.draftConnectorQuery({ workspace: dir, connection: conn, request: describe });
      if (r.query) setSql(r.query.statement);
      setResult(r.problem ? { problem: r.problem } : null);
    } catch (e) {
      setResult({ problem: message(e) });
    } finally {
      setBusy("");
    }
  };

  const ready = saved ? true : !!sql.trim();
  return (
    <Dialog
      title={saved ? `${connection} › ${saved.name}` : t(asking.describe ? "desktop.data.describe" : "desktop.data.write_sql")}
      icon={mdiTableSearch}
      wide
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>{t("desktop.cancel")}</Button>
          {result?.table ? (
            <Button variant="filled" onClick={() => onAdd(queryBlock(connection, saved?.name ?? "", statement, result.table!))}>
              {t("desktop.data.add_result")}
            </Button>
          ) : (
            <Button variant="filled" icon={busy === "run" ? mdiLoading : mdiPlay} iconSpin={busy === "run"} disabled={!conn || !ready || !!busy} onClick={() => void run()}>
              {t("desktop.data.run")}
            </Button>
          )}
        </>
      }
    >
      <div className="stack" style={{ gap: 12 }}>
        <span className="t-body-sm muted">{t("desktop.data.run_hint")}</span>
        {!saved && (
          <Field label={t("desktop.flows.data.connection")}>
            {(id) => (
              <select id={id} className="select" value={connection} onChange={(e) => (setConnection(e.target.value), setResult(null))}>
                {connections.map((c) => (
                  <option key={c.name} value={c.name}>
                    {c.name}
                  </option>
                ))}
              </select>
            )}
          </Field>
        )}
        {saved && saved.description && <span className="t-body-sm">{saved.description}</span>}
        {saved?.params.map((p) => (
          <Field key={p.name} label={p.name} supporting={p.description}>
            {(id) => <input id={id} className="input mono" value={args[p.name] ?? ""} onChange={(e) => (setArgs({ ...args, [p.name]: e.target.value }), setResult(null))} />}
          </Field>
        ))}
        {!saved && asking.describe && (
          <Field label={t("desktop.flows.data.describe")} supporting={t("desktop.data.draft_hint")}>
            {(id) => (
              <div className="stack" style={{ gap: 6 }}>
                <input id={id} className="input" value={describe} placeholder={t("desktop.flows.data.describe_placeholder")} onChange={(e) => setDescribe(e.target.value)} />
                <div className="row" style={{ justifyContent: "flex-end" }}>
                  <Button variant="tonal" icon={busy === "draft" ? mdiLoading : mdiCreationOutline} iconSpin={busy === "draft"} disabled={!conn || !!busy || !describe.trim()} onClick={() => void draft()}>
                    {t("desktop.flows.data.draft")}
                  </Button>
                </div>
              </div>
            )}
          </Field>
        )}
        {!saved && (
          <Field label={t("desktop.flows.data.sql")}>
            {(id) => <textarea id={id} className="input mono" rows={6} spellCheck={false} value={sql} placeholder={sqlPlaceholder} onChange={(e) => (setSql(e.target.value), setResult(null))} />}
          </Field>
        )}
        {result?.problem && <p className="error-text t-body-sm">{result.problem}</p>}
        {result?.table && <DataTableView table={result.table} />}
      </div>
    </Dialog>
  );
}

/** A query chip's result, opened from the chip (DAT-42), over the page (the chat box's dock would clip it). */
export function DataResultDialog({ block, onClose }: { block: WebBlock; onClose: () => void }) {
  return createPortal(
    <Dialog title={block.title} icon={mdiTableSearch} wide closeButton onClose={onClose}>
      <div className="stack" style={{ gap: 12 }}>
        {block.statement && <pre className="mono t-body-sm data-sql">{block.statement}</pre>}
        {block.table && <DataTableView table={block.table} />}
      </div>
    </Dialog>,
    document.body,
  );
}
