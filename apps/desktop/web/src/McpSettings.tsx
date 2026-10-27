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
import { mdiCogOutline, mdiDeleteOutline, mdiEarth, mdiPencilOutline, mdiPlus, mdiPowerPlugOutline, mdiPowerPlugOffOutline } from "@mdi/js";
import { config } from "./api";
import { message } from "./errors";
import { configChanged, openSettings } from "./events";
import type { MCPServerConfig } from "./gen/blitz/v1/config_pb";
import { t } from "./i18n";
import { emptyForm, formProblems, fromForm, targetOf, toForm, type McpForm } from "./mcpForm";
import { SuggestTextarea } from "./SecretSuggest";
import { Button, Dialog, Icon, IconButton, Segmented, Switch, useSnackbar } from "./ui/controls";

/**
 * Settings › Connectors › MCP servers (spec_mcp_009 MCP-46): the global settings' MCP
 * servers, each on or off; added and edited in a dialog, removed after
 * asking. Settings are global: how each does in a workspace is that
 * workspace's. Embedded, it's a section of Settings › Connectors, headed
 * as its categories are. With a workspace it's Studio › Connectors'
 * (spec_mcp_009 MCP-50): the servers that workspace runs, by scope. Its
 * own are added, edited, removed and made global there; a global one is
 * shown as it is, with a switch turning it off or on in this workspace
 * alone and Edit in Settings (Studio never changes what other workspaces
 * use); a trusted project's are shown as they are.
 */
export function McpSettings({ embedded, workspace = "" }: { embedded?: boolean; workspace?: string } = {}) {
  const snack = useSnackbar();
  const [configs, setConfigs] = useState<MCPServerConfig[]>([]);
  const [path, setPath] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  // The server being edited ("" a new one), and the one to remove.
  const [editing, setEditing] = useState<{ name: string; form: McpForm } | null>(null);
  const [removing, setRemoving] = useState("");
  const [promoting, setPromoting] = useState("");

  const load = useCallback(async () => {
    try {
      const r = await config.listMCPServerConfigs({ workspace });
      setConfigs(r.servers);
      setPath(r.path);
      setError("");
    } catch (e) {
      setError(message(e));
    }
  }, [workspace]);
  useEffect(() => void load(), [load]);

  // Runs a change, says how it went, tells the window and reloads.
  const act = async (run: () => Promise<string>) => {
    setBusy(true);
    try {
      snack(await run());
      configChanged({ dir: workspace });
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
      {t(workspace ? "desktop.mcp.intro_workspace" : "desktop.mcp.intro")}
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
              studio={!!workspace}
              onEnable={(on) =>
                act(async () => (await config.setMCPServerEnabled({ workspace, name: c.name, enabled: on }), t(on ? (workspace && c.scope === "global" ? "desktop.mcp.enabled_here" : "desktop.mcp.enabled") : workspace && c.scope === "global" ? "desktop.mcp.disabled_here" : "desktop.mcp.disabled", { name: c.name })))
              }
              onEdit={() => setEditing({ name: c.name, form: toForm(c) })}
              onRemove={() => setRemoving(c.name)}
              onPromote={() => setPromoting(c.name)}
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
              await config.saveMCPServer({ workspace, name: editing.name, server: fromForm(f) });
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
                  void act(async () => (await config.removeMCPServer({ workspace, name }), t("desktop.mcp.removed", { name })));
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
      {promoting && (
        <Dialog
          title={t("desktop.mcp.make_global_title", { name: promoting })}
          icon={mdiEarth}
          onClose={() => setPromoting("")}
          footer={
            <>
              <Button onClick={() => setPromoting("")}>{t("desktop.cancel")}</Button>
              <Button
                variant="filled"
                onClick={() => {
                  const name = promoting;
                  setPromoting("");
                  void act(async () => (await config.makeMCPServerGlobal({ workspace, name }), t("desktop.mcp.made_global", { name })));
                }}
              >
                {t("desktop.mcp.make_global")}
              </Button>
            </>
          }
        >
          <p>{t("desktop.mcp.make_global_body", { name: promoting })}</p>
        </Dialog>
      )}
    </div>
  );
}

// One server: its name, what it runs, and what can be done with it. In
// Studio (studio) it says whose it is; a global one has its switch (on
// or off here) and Edit in Settings, a project's nothing to change.
function ServerRow({
  server,
  busy,
  studio,
  onEnable,
  onEdit,
  onRemove,
  onPromote,
}: {
  server: MCPServerConfig;
  busy: boolean;
  studio: boolean;
  onEnable: (on: boolean) => void;
  onEdit: () => void;
  onRemove: () => void;
  onPromote: () => void;
}) {
  const enabled = !server.disabled && !server.offHere;
  const target = targetOf(server);
  const global = studio && server.scope === "global";
  const project = studio && server.scope === "project";
  const own = !global && !project;
  const whose = studio ? t(`desktop.mcp.scope.${server.scope || "workspace"}`) : "";
  const note = [whose, server.offHere ? t("desktop.mcp.off_here") : global && server.disabled ? t("desktop.mcp.off_everywhere") : ""].filter(Boolean).join(" · ");
  return (
    <div className="rule mcp-row">
      <Icon path={enabled ? mdiPowerPlugOutline : mdiPowerPlugOffOutline} className="muted" />
      <div className="stack spacer" style={{ gap: 2, minWidth: 0 }}>
        <strong>{server.name}</strong>
        <code className="t-body-sm ellipsis" title={target}>
          {target}
        </code>
        {note && <span className="t-body-sm muted">{note}</span>}
      </div>
      {own && <IconButton icon={mdiPencilOutline} label={t("desktop.mcp.edit", { name: server.name })} small disabled={busy} onClick={onEdit} />}
      {own && <IconButton icon={mdiDeleteOutline} label={t("desktop.mcp.remove_named", { name: server.name })} small disabled={busy} onClick={onRemove} />}
      {own && studio && <IconButton icon={mdiEarth} label={t("desktop.mcp.make_global_named", { name: server.name })} small disabled={busy} onClick={onPromote} />}
      {global && <IconButton icon={mdiCogOutline} label={t("desktop.mcp.edit_in_settings", { name: server.name })} small onClick={() => openSettings({ section: "mcp" })} />}
      {!project && (
        <Switch
          label={t(global ? "desktop.mcp.enable_here" : "desktop.mcp.enable", { name: server.name })}
          checked={enabled}
          disabled={busy || (global && server.disabled)}
          onChange={onEnable}
        />
      )}
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
