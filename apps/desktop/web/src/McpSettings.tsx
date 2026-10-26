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
import { mdiDeleteOutline, mdiPencilOutline, mdiPlus, mdiPowerPlugOutline, mdiPowerPlugOffOutline } from "@mdi/js";
import { config } from "./api";
import { message } from "./errors";
import { configChanged } from "./events";
import type { MCPServerConfig } from "./gen/blitz/v1/config_pb";
import { t } from "./i18n";
import { emptyForm, formProblems, fromForm, targetOf, toForm, type McpForm } from "./mcpForm";
import { SuggestTextarea } from "./SecretSuggest";
import { Button, Dialog, Icon, IconButton, Segmented, Switch, useSnackbar } from "./ui/controls";

/**
 * Settings › MCP servers (spec_mcp_009 MCP-46): the global settings' MCP
 * servers, each on or off; added and edited in a dialog, removed after
 * asking. Settings are global: how each does in a workspace is that
 * workspace's. Embedded, it's a section of Settings › Connectors, headed
 * as its categories are.
 */
export function McpSettings({ embedded }: { embedded?: boolean } = {}) {
  const snack = useSnackbar();
  const [configs, setConfigs] = useState<MCPServerConfig[]>([]);
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
      setError("");
    } catch (e) {
      setError(message(e));
    }
  }, []);
  useEffect(() => void load(), [load]);

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

  const intro = (
    <>
      {t("desktop.mcp.intro")}
      {path && (
        <>
          {" "}
          {t("desktop.mcp.file")} <code>{path}</code>.
        </>
      )}
    </>
  );
  const add = () => setEditing({ name: "", form: emptyForm });
  return (
    <div className={`stack mcp-settings ${embedded ? "rule-section" : ""}`} style={{ gap: embedded ? 4 : 16 }}>
      {embedded ? (
        <>
          <div className="row" style={{ gap: 6 }}>
            <Icon path={mdiPowerPlugOutline} size="sm" />
            <span className="t-title-sm spacer">{t("desktop.settings.mcp")}</span>
            <IconButton icon={mdiPlus} label={t("desktop.mcp.add")} small disabled={busy} onClick={add} />
          </div>
          <span className="t-body-sm muted">{intro}</span>
        </>
      ) : (
        <p className="t-body-sm muted">{intro}</p>
      )}
      {error && <p className="error-text">{error}</p>}
      {!embedded && (
        <div className="row">
          <span className="t-title-sm spacer">{t("desktop.mcp.servers")}</span>
          <Button variant="tonal" icon={mdiPlus} disabled={busy} onClick={add}>
            {t("desktop.mcp.add")}
          </Button>
        </div>
      )}
      {configs.length === 0 ? (
        <p className="t-body-sm muted">{t("desktop.mcp.none")}</p>
      ) : (
        <div className="list">
          {configs.map((c) => (
            <ServerRow
              key={c.name}
              server={c}
              busy={busy}
              onEnable={(on) => act(async () => (await config.setMCPServerEnabled({ name: c.name, enabled: on }), t(on ? "desktop.mcp.enabled" : "desktop.mcp.disabled", { name: c.name })))}
              onEdit={() => setEditing({ name: c.name, form: toForm(c) })}
              onRemove={() => setRemoving(c.name)}
            />
          ))}
        </div>
      )}
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

// One server: its name, what it runs, and what can be done with it.
function ServerRow({ server, busy, onEnable, onEdit, onRemove }: { server: MCPServerConfig; busy: boolean; onEnable: (on: boolean) => void; onEdit: () => void; onRemove: () => void }) {
  const enabled = !server.disabled;
  const target = targetOf(server);
  return (
    <div className="rule mcp-row">
      <Icon path={enabled ? mdiPowerPlugOutline : mdiPowerPlugOffOutline} className="muted" />
      <div className="stack spacer" style={{ gap: 2, minWidth: 0 }}>
        <strong>{server.name}</strong>
        <code className="t-body-sm ellipsis" title={target}>
          {target}
        </code>
      </div>
      <IconButton icon={mdiPencilOutline} label={t("desktop.mcp.edit", { name: server.name })} small disabled={busy} onClick={onEdit} />
      <IconButton icon={mdiDeleteOutline} label={t("desktop.mcp.remove_named", { name: server.name })} small disabled={busy} onClick={onRemove} />
      <Switch label={t("desktop.mcp.enable", { name: server.name })} checked={enabled} disabled={busy} onChange={onEnable} />
    </div>
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
      {k === "args" ? (
        <textarea className="input mono" rows={3} value={f[k]} placeholder={placeholder} spellCheck={false} onChange={(e) => set(k, e.target.value)} />
      ) : (
        // Environment and header values may name global secrets.
        <SuggestTextarea className="input mono" rows={3} value={f[k]} placeholder={placeholder} spellCheck={false} onValue={(v) => set(k, v)} workspace="" />
      )}
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
