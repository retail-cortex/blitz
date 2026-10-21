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

import { mdiCreationOutline, mdiPlus } from "@mdi/js";
import { language, t } from "../i18n";
import type { ConnectorKind } from "../gen/blitz/v1/workspace_pb";
import { Button, Field, Segmented, Switch } from "../ui/controls";
import type { ConnectorApi } from "./connectorApi";
import { can, newQueryForm, textIn, withKind, type ConnectorForm, type QueryForm } from "./connectorForm";
import { QueryCard } from "./QueryCard";

/**
 * A connection's form, drawn from its kind's descriptor: name and
 * description, the folder it's kept in, the category's kinds, the
 * fields, Read-only (when it can be), the ready-made tools and, as the
 * kind can, its own queries with Try it and Write a query (onBuild). A
 * file the form can't describe is edited as it is.
 */
export function ConnectorFields({
  api,
  form,
  setForm,
  kind,
  kinds,
  prefix,
  folder,
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
  onBuild: () => void;
}) {
  const lang = language();
  const set = <K extends keyof ConnectorForm>(k: K, v: ConnectorForm[K]) => setForm((f) => ({ ...f, [k]: v }));
  const setQuery = (key: string, q: Partial<QueryForm>) => setForm((f) => ({ ...f, queries: f.queries.map((x) => (x.key === key ? { ...x, ...q } : x)) }));
  const example = form.name.trim() || t("desktop.connectors.example_name");
  return (
    <>
      <div className="pair">
        <Field label={t("desktop.connectors.name")} supporting={t("desktop.connectors.name_hint", { prefix, name: example })}>
          {(id) => <input id={id} className="input mono" value={form.name} onChange={(e) => set("name", e.target.value)} />}
        </Field>
        <Field label={t("desktop.connectors.description")} supporting={t("desktop.connectors.description_hint")}>
          {(id) => <input id={id} className="input" value={form.description} disabled={form.custom} onChange={(e) => set("description", e.target.value)} />}
        </Field>
      </div>
      <span className="t-body-sm muted">{t(`desktop.connectors.scope_hint.${form.scope}`, { path: folder })}</span>
      {form.custom ? (
        <Field label={t("desktop.connectors.yaml")} supporting={t("desktop.connectors.custom_hint")}>
          {(id) => <textarea id={id} className="input mono" rows={16} value={form.text} onChange={(e) => set("text", e.target.value)} />}
        </Field>
      ) : (
        <>
          <div className="stack" style={{ gap: 6 }}>
            <span className="t-label">{t("desktop.connectors.kind")}</span>
            <Segmented
              value={form.kind}
              label={t("desktop.connectors.kind")}
              options={kinds.map((k) => ({ value: k.id, label: textIn(k.name, lang) }))}
              onChange={(v) => {
                const next = kinds.find((k) => k.id === v);
                if (next) setForm((f) => withKind(f, next));
              }}
            />
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
              <span className="t-body-sm muted">{t("desktop.connectors.tools_hint", { prefix, name: example })}</span>
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
          {kind && can(kind, "queries") && (
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
