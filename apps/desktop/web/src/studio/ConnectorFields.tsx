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

import { mdiCreationOutline, mdiFileOutline, mdiPlus, mdiSwapHorizontal } from "@mdi/js";
import { useState } from "react";
import { chooseFile } from "../desktop";
import { language, t } from "../i18n";
import type { ConnectorKind } from "../gen/blitz/v1/workspace_pb";
import { useAdvanced } from "../state";
import { Button, Field, Switch } from "../ui/controls";
import type { ConnectorApi } from "./connectorApi";
import { can, newQueryForm, parseConnectionURL, relativeTo, startFolder, takesURL, typedName, textIn, withKind, withSetting, type ConnectorForm, type QueryForm } from "./connectorForm";
import { QueryCard } from "./QueryCard";

/**
 * A connection's form, drawn from its kind's descriptor, in the order
 * it's filled: the kind (tiles, simplest first, until one is picked), a
 * connection URL to paste where the kind has a server, its fields (a
 * file's with Choose…, opening in the workspace and kept relative to it
 * when it's inside), the name (its database's until one is typed) and
 * description, and Read-only. Its ready-made tools and own queries, with
 * Try it and Write a query (onBuild), come once it's saved (saved); a new
 * one is connected first. A file the form can't describe is edited as it
 * is. The tool names and where the file is kept show with advanced
 * settings.
 */
export function ConnectorFields({
  api,
  form,
  setForm,
  kind,
  kinds,
  prefix,
  folder,
  saved,
  base = "",
  onBuild,
}: {
  api: ConnectorApi;
  form: ConnectorForm;
  setForm: (f: (old: ConnectorForm) => ConnectorForm) => void;
  kind: ConnectorKind | undefined;
  /** The category's usable kinds. */
  kinds: ConnectorKind[];
  prefix: string;
  /** Where its file is kept. */
  folder: string;
  /** It has a file: its tools and queries can be chosen. */
  saved: boolean;
  /** The workspace a file path is chosen from and kept relative to ("" for a global connection). */
  base?: string;
  onBuild: () => void;
}) {
  const lang = language();
  const advanced = useAdvanced();
  const [pasted, setPasted] = useState("");
  // A URL read into the fields is cleared (it holds the password): what it filled is said instead.
  const [filled, setFilled] = useState(false);
  const set = <K extends keyof ConnectorForm>(k: K, v: ConnectorForm[K]) => setForm((f) => ({ ...f, [k]: v }));
  const setQuery = (key: string, q: Partial<QueryForm>) => setForm((f) => ({ ...f, queries: f.queries.map((x) => (x.key === key ? { ...x, ...q } : x)) }));
  const example = form.name.trim() || t("desktop.connectors.example_name");
  const parsed = kind && pasted.trim() ? parseConnectionURL(pasted, kind) : undefined;
  const paste = (text: string) => {
    setPasted(text);
    setFilled(false);
    const parts = kind && parseConnectionURL(text, kind);
    if (!parts) return;
    setPasted("");
    setFilled(true);
    setForm((f) => {
      let next = f;
      for (const [k, v] of Object.entries(parts.settings)) next = withSetting(next, k, v);
      return { ...next, secretValues: { ...next.secretValues, ...parts.secrets } };
    });
  };
  const choose = async (key: string, label: string) => {
    const path = await chooseFile(label, startFolder(base, form.settings[key] ?? ""));
    if (path) setForm((f) => withSetting(f, key, relativeTo(base, path)));
  };
  if (!form.custom && !kind)
    return (
      <div className="stack" style={{ gap: 8 }}>
        <span className="t-label">{t("desktop.connectors.pick_kind")}</span>
        <div className="recent-grid kind-tiles">
          {kinds.map((k) => (
            <button key={k.id} type="button" className="recent-card" onClick={() => setForm((f) => withKind(f, k))}>
              <span className="t-title-sm">{textIn(k.name, lang)}</span>
              <span className="t-body-sm muted">{textIn(k.hint, lang)}</span>
            </button>
          ))}
        </div>
      </div>
    );
  return (
    <>
      {form.custom ? (
        <Field label={t("desktop.connectors.yaml")} supporting={t("desktop.connectors.custom_hint")}>
          {(id) => <textarea id={id} className="input mono" rows={16} value={form.text} onChange={(e) => set("text", e.target.value)} />}
        </Field>
      ) : (
        <>
          {kind && (
            <div className="row wrap" style={{ gap: 8 }}>
              <span className="stack spacer" style={{ gap: 2 }}>
                <span className="t-label">{textIn(kind.name, lang)}</span>
                <span className="t-body-sm muted">{textIn(kind.hint, lang)}</span>
              </span>
              {kinds.length > 1 && (
                <Button small icon={mdiSwapHorizontal} onClick={() => set("kind", "")}>
                  {t("desktop.connectors.change_kind")}
                </Button>
              )}
            </div>
          )}
          {takesURL(kind) && (
            <Field label={t("desktop.connectors.paste_url")} supporting={pasted.trim() && !parsed ? t("desktop.connectors.paste_url_bad") : filled ? t("desktop.connectors.paste_url_filled") : t("desktop.connectors.paste_url_hint")}>
              {(id) => <input id={id} className="input mono" value={pasted} placeholder={t("desktop.connectors.paste_url_placeholder")} spellCheck={false} autoComplete="off" onChange={(e) => paste(e.target.value)} />}
            </Field>
          )}
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
                    {(id) =>
                      fl.type === "path" ? (
                        <span className="row" style={{ gap: 8 }}>
                          <input id={id} className="input mono spacer" value={form.settings[fl.key] ?? ""} placeholder={fl.defaultValue} onChange={(e) => setForm((f) => withSetting(f, fl.key, e.target.value))} />
                          <Button icon={mdiFileOutline} onClick={() => void choose(fl.key, textIn(fl.label, lang))}>
                            {t("desktop.connectors.choose_file")}
                          </Button>
                        </span>
                      ) : (
                        <input
                          id={id}
                          className="input"
                          inputMode={fl.type === "number" ? "numeric" : undefined}
                          value={form.settings[fl.key] ?? ""}
                          placeholder={fl.defaultValue}
                          onChange={(e) => setForm((f) => withSetting(f, fl.key, e.target.value))}
                        />
                      )
                    }
                  </Field>
                ),
              )}
            </div>
          )}
        </>
      )}
      <div className="pair">
        <Field label={t("desktop.connectors.name")} supporting={advanced ? t("desktop.connectors.name_hint", { prefix, name: example }) : t("desktop.connectors.name_simple_hint")}>
          {(id) => <input id={id} className="input mono" value={form.name} onChange={(e) => set("name", typedName(e.target.value))} />}
        </Field>
        <Field label={t("desktop.connectors.description")} supporting={t("desktop.connectors.description_hint")}>
          {(id) => <input id={id} className="input" value={form.description} disabled={form.custom} onChange={(e) => set("description", e.target.value)} />}
        </Field>
      </div>
      {advanced && <span className="t-body-sm muted">{t(`desktop.connectors.scope_hint.${form.scope}`, { path: folder })}</span>}
      {!form.custom && (
        <>
          {kind?.canReadOnly && (
            <label className="row agent-switch">
              <Switch label={t("desktop.connectors.read_only")} checked={form.readOnly} onChange={(v) => set("readOnly", v)} />
              <span className="stack" style={{ gap: 0 }}>
                <span className="t-label">{t("desktop.connectors.read_only")}</span>
                <span className="t-body-sm muted">{form.readOnly ? textIn(kind.readOnlyHint, lang) : t("desktop.connectors.read_write_hint")}</span>
              </span>
            </label>
          )}
          {!saved && kind && (kind.tools.length > 0 || can(kind, "queries")) && <p className="t-body-sm muted">{t("desktop.connectors.tools_after_save")}</p>}
          {saved && kind && kind.tools.length > 0 && (
            <div className="stack" style={{ gap: 6 }}>
              <span className="t-label">{t("desktop.connectors.tools")}</span>
              {advanced && <span className="t-body-sm muted">{t("desktop.connectors.tools_hint", { prefix, name: example })}</span>}
              <div className="list">
                {kind.tools.map((rt) => (
                  <label key={rt.id} className="list-item row agent-switch">
                    <Switch label={rt.id} checked={form.tools.includes(rt.id)} onChange={(v) => set("tools", v ? [...form.tools, rt.id] : form.tools.filter((x) => x !== rt.id))} />
                    <span className="stack spacer" style={{ gap: 0 }}>
                      <span className="mono">{rt.id}</span>
                      <span className="t-body-sm muted">{rt.writes && form.readOnly ? t("desktop.connectors.tool_hidden") : textIn(rt.about, lang)}</span>
                    </span>
                  </label>
                ))}
              </div>
            </div>
          )}
          {saved && kind && can(kind, "queries") && (
            <div className="stack" style={{ gap: 6 }}>
              <div className="row wrap" style={{ gap: 8 }}>
                <span className="t-label spacer">{t("desktop.connectors.queries")}</span>
                {can(kind, "query-builder") && (
                  <Button small icon={mdiCreationOutline} disabled={!form.name.trim()} title={t("desktop.connectors.builder_hint")} onClick={onBuild}>
                    {t("desktop.connectors.builder")}
                  </Button>
                )}
                <Button icon={mdiPlus} onClick={() => set("queries", [...form.queries, newQueryForm()])}>
                  {t("desktop.connectors.add_query")}
                </Button>
              </div>
              <span className="t-body-sm muted">{textIn(kind.queriesHint, lang)}</span>
              {form.queries.map((q) => (
                <QueryCard
                  key={q.key}
                  api={api}
                  form={form}
                  kind={kind}
                  prefix={prefix}
                  query={q}
                  onChange={(x) => setQuery(q.key, x)}
                  onRemove={() => set("queries", form.queries.filter((x) => x.key !== q.key))}
                />
              ))}
            </div>
          )}
        </>
      )}
    </>
  );
}
