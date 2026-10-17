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
import { mdiCheckCircleOutline, mdiAlertCircleOutline, mdiDeleteOutline, mdiPencilOutline, mdiPlus, mdiRestart, mdiPowerPlugOutline, mdiPowerPlugOffOutline } from "@mdi/js";
import { config, workspaces } from "./api";
import { message } from "./errors";
import { configChanged } from "./events";
import type { MCPServerConfig } from "./gen/blitz/v1/config_pb";
import type { MCPServer } from "./gen/blitz/v1/workspace_pb";
import { t, tn } from "./i18n";
import { emptyForm, formProblems, fromForm, mcpRows, toForm, type McpForm, type McpRow } from "./mcpForm";
import { useApp } from "./state";
import { Button, Chip, Dialog, Icon, IconButton, Segmented, Switch, useSnackbar } from "./ui/controls";

/** How often the servers' states are asked again while the section is open. */
const refreshEvery = 5000;

/**
 * Settings › MCP servers (spec_mcp_009 MCP-46): the global settings' MCP
 * servers, each on or off, with how it's doing in the workspace shown
 * (connected and its tools, waiting for a turn, failed and why) and
 * Restart; added and edited in a dialog, removed after asking. The
 * workspace's own (its project's) are listed too, not edited here.
 */
export function McpSettings() {
  const snack = useSnackbar();
  const { prefs } = useApp();
  const active = prefs.workspaces.find((w) => w.dir === prefs.active && w.open)?.dir ?? "";
  const [configs, setConfigs] = useState<MCPServerConfig[]>([]);
  const [running, setRunning] = useState<MCPServer[]>([]);
  const [path, setPath] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  // The server being edited ("" a new one), and the one to remove.
  const [editing, setEditing] = useState<{ name: string; form: McpForm } | null>(null);
  const [removing, setRemoving] = useState("");

  const load = useCallback(async () => {
    try {
      const r = await config.listMCPServerConfigs({});
      setConfigs(r.servers);
      setPath(r.path);
      setRunning(active ? (await workspaces.listMCPServers({ workspace: active })).servers : []);
      setError("");
    } catch (e) {
      setError(message(e));
    }
  }, [active]);
  useEffect(() => {
    void load();
    const id = window.setInterval(() => void load(), refreshEvery);
    return () => window.clearInterval(id);
  }, [load]);

  // Runs a change, says how it went, tells the window and reloads.
  const act = async (run: () => Promise<string>) => {
    setBusy(true);
    try {
      snack(await run());
      configChanged({ dir: "" });
      await load();
      return true;
    } catch (e) {
      snack(message(e), { error: true });
      return false;
    } finally {
      setBusy(false);
    }
  };

  const rows = mcpRows(configs, running);
  return (
    <div className="stack mcp-settings" style={{ gap: 16 }}>
      <div className="stack" style={{ gap: 4 }}>
        <p className="t-body-sm muted">{t("desktop.mcp.intro")}</p>
        {path && (
          <p className="t-body-sm muted">
            {t("desktop.mcp.file")} <code>{path}</code>
          </p>
        )}
      </div>
      {error && <p className="error-text">{error}</p>}
      <div className="row">
        <span className="t-title-sm spacer">{t("desktop.mcp.servers")}</span>
        <Button variant="tonal" icon={mdiPlus} disabled={busy} onClick={() => setEditing({ name: "", form: emptyForm })}>
          {t("desktop.mcp.add")}
        </Button>
      </div>
      {rows.length === 0 ? (
        <p className="t-body-sm muted">{t("desktop.mcp.none")}</p>
      ) : (
        <div className="list">
          {rows.map((r) => (
            <ServerRow
              key={r.name}
              row={r}
              workspace={active}
              busy={busy}
              onEnable={(on) => act(async () => (await config.setMCPServerEnabled({ name: r.name, enabled: on }), t(on ? "desktop.mcp.enabled" : "desktop.mcp.disabled", { name: r.name })))}
              onRestart={() => act(async () => (await workspaces.restartMCPServer({ workspace: active, name: r.name }), t("desktop.mcp.restarted", { name: r.name })))}
              onEdit={() => r.config && setEditing({ name: r.name, form: toForm(r.config) })}
              onRemove={() => setRemoving(r.name)}
            />
          ))}
        </div>
      )}
      {!active && <p className="t-body-sm muted">{t("desktop.mcp.no_workspace")}</p>}
      {editing && (
        <ServerDialog
          editing={editing.name}
          initial={editing.form}
          taken={configs.map((c) => c.name).filter((n) => n !== editing.name)}
          busy={busy}
          onClose={() => setEditing(null)}
          onSave={async (f) => {
            const saved = await act(async () => {
              await config.saveMCPServer({ name: editing.name, server: fromForm(f) });
              return t(editing.name ? "desktop.mcp.saved" : "desktop.mcp.added", { name: f.name.trim() });
            });
            if (saved) setEditing(null);
          }}
        />
      )}
      {removing && (
        <Dialog
          title={t("desktop.mcp.remove_title", { name: removing })}
          icon={mdiDeleteOutline}
          onClose={() => setRemoving("")}
          footer={
            <>
              <Button onClick={() => setRemoving("")}>{t("desktop.cancel")}</Button>
              <Button
                variant="filled"
                danger
                onClick={() => {
                  const name = removing;
                  setRemoving("");
                  void act(async () => (await config.removeMCPServer({ name }), t("desktop.mcp.removed", { name })));
                }}
              >
                {t("desktop.mcp.remove")}
              </Button>
            </>
          }
        >
          <p>{t("desktop.mcp.remove_body", { name: removing })}</p>
        </Dialog>
      )}
    </div>
  );
}

// One server: its name, what it runs, how it's doing, and what can be
// done with it.
function ServerRow({ row, workspace, busy, onEnable, onRestart, onEdit, onRemove }: { row: McpRow; workspace: string; busy: boolean; onEnable: (on: boolean) => void; onRestart: () => void; onEdit: () => void; onRemove: () => void }) {
  const enabled = row.config ? !row.config.disabled : row.running?.state !== "disabled";
  const state = row.running?.state ?? (enabled ? "" : "disabled");
  return (
    <div className="rule mcp-row">
      <Icon path={enabled ? mdiPowerPlugOutline : mdiPowerPlugOffOutline} className="muted" />
      <div className="stack spacer" style={{ gap: 2, minWidth: 0 }}>
        <span className="row" style={{ gap: 8 }}>
          <strong>{row.name}</strong>
          {state && <StateChip server={row.running} state={state} />}
          {!row.config && <span className="t-body-sm muted">{t("desktop.mcp.from_project")}</span>}
        </span>
        <code className="t-body-sm ellipsis" title={row.target}>
          {row.target}
        </code>
        {row.running?.error && <span className="t-body-sm error-text">{row.running.error}</span>}
      </div>
      {workspace && row.running && state !== "disabled" && <IconButton icon={mdiRestart} label={t("desktop.mcp.restart", { name: row.name })} small disabled={busy} onClick={onRestart} />}
      {row.config && (
        <>
          <IconButton icon={mdiPencilOutline} label={t("desktop.mcp.edit", { name: row.name })} small disabled={busy} onClick={onEdit} />
          <IconButton icon={mdiDeleteOutline} label={t("desktop.mcp.remove_named", { name: row.name })} small disabled={busy} onClick={onRemove} />
          <Switch label={t("desktop.mcp.enable", { name: row.name })} checked={enabled} disabled={busy} onChange={onEnable} />
        </>
      )}
    </div>
  );
}

// How a server is doing, as a chip: connected (and its tools), waiting
// for a turn, failed, or off.
function StateChip({ server, state }: { server?: MCPServer; state: string }) {
  const label = state === "connected" ? tn("desktop.mcp.state.connected", server?.tools ?? 0) : t(`desktop.mcp.state.${state}`);
  return (
    <Chip className="static small" tone={state === "failed" ? "danger" : undefined} icon={state === "connected" ? mdiCheckCircleOutline : state === "failed" ? mdiAlertCircleOutline : undefined} tabIndex={-1}>
      {label}
    </Chip>
  );
}

// The form for a server, new or (editing, its name) changed.
function ServerDialog({ editing, initial, taken, busy, onClose, onSave }: { editing: string; initial: McpForm; taken: string[]; busy: boolean; onClose: () => void; onSave: (f: McpForm) => void }) {
  const [f, setF] = useState(initial);
  const [tried, setTried] = useState(false);
  const set = <K extends keyof McpForm>(k: K, v: McpForm[K]) => setF((x) => ({ ...x, [k]: v }));
  const problems = formProblems(f, taken);
  const save = () => {
    setTried(true);
    if (!problems.length) onSave(f);
  };
  const field = (k: "name" | "command" | "url" | "timeout" | "agents", props: { mono?: boolean; placeholder?: string; inputMode?: "numeric" } = {}) => (
    <label className="field">
      <span className="t-label">{t(`desktop.mcp.field.${k}`)}</span>
      <input className={`input ${props.mono ? "mono" : ""}`} value={f[k]} placeholder={props.placeholder} inputMode={props.inputMode} spellCheck={false} onChange={(e) => set(k, e.target.value)} onKeyDown={(e) => e.key === "Enter" && save()} />
    </label>
  );
  const area = (k: "args" | "env" | "headers", placeholder: string) => (
    <label className="field">
      <span className="t-label">{t(`desktop.mcp.field.${k}`)}</span>
      <textarea className="input mono" rows={3} value={f[k]} placeholder={placeholder} spellCheck={false} onChange={(e) => set(k, e.target.value)} />
    </label>
  );
  return (
    <Dialog
      title={editing ? t("desktop.mcp.edit_title", { name: editing }) : t("desktop.mcp.add_title")}
      icon={mdiPowerPlugOutline}
      onClose={onClose}
      wide
      footer={
        <>
          <Button onClick={onClose}>{t("desktop.cancel")}</Button>
          <Button variant="filled" disabled={busy} onClick={save}>
            {t(editing ? "desktop.mcp.save" : "desktop.mcp.add")}
          </Button>
        </>
      }
    >
      <div className="stack mcp-form" style={{ gap: 12 }}>
        {field("name", { placeholder: "github" })}
        <Segmented<McpForm["kind"]>
          equal
          label={t("desktop.mcp.field.kind")}
          value={f.kind}
          onChange={(kind) => set("kind", kind)}
          options={[
            { value: "command", label: t("desktop.mcp.kind.command") },
            { value: "url", label: t("desktop.mcp.kind.url") },
          ]}
        />
        {f.kind === "command" ? (
          <>
            {field("command", { mono: true, placeholder: "npx" })}
            {area("args", "-y\n@modelcontextprotocol/server-github")}
            {area("env", "GITHUB_TOKEN=…")}
            <label className="row" style={{ gap: 12 }}>
              <span className="spacer t-body">{t("desktop.mcp.field.sandbox")}</span>
              <Switch label={t("desktop.mcp.field.sandbox")} checked={f.sandbox} onChange={(v) => set("sandbox", v)} />
            </label>
          </>
        ) : (
          <>
            {field("url", { mono: true, placeholder: "https://mcp.example.com/mcp" })}
            {area("headers", "Authorization: Bearer …")}
            <p className="t-body-sm muted">{t("desktop.mcp.signin")}</p>
          </>
        )}
        <label className="row" style={{ gap: 12 }}>
          <span className="spacer t-body">{t("desktop.mcp.field.auto_approve")}</span>
          <Switch label={t("desktop.mcp.field.auto_approve")} checked={f.autoApprove} onChange={(v) => set("autoApprove", v)} />
        </label>
        <div className="row mcp-form-pair" style={{ gap: 12 }}>
          {field("timeout", { inputMode: "numeric", placeholder: "300" })}
          {field("agents", { placeholder: t("desktop.mcp.field.agents_placeholder") })}
        </div>
        {tried && problems.length > 0 && (
          <ul className="error-text t-body-sm">
            {problems.map((p) => (
              <li key={p}>{t(p)}</li>
            ))}
          </ul>
        )}
      </div>
    </Dialog>
  );
}
