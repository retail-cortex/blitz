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

import { useId, useRef, type KeyboardEvent } from "react";
import { mdiDeleteOutline, mdiPlus } from "@mdi/js";
import { newHeader, scriptLanguages, tokenPlaceholder, usesToken, type ApiForm, type ApiHeaderRow } from "./apiForm";
import { t } from "./i18n";
import { Button, IconButton, Switch } from "./ui/controls";

/**
 * The api provider's fields in the provider card (DSK-61): its endpoint,
 * its headers (values never shown, the token's placeholder aside) and the
 * script that prints the token. The card saves them with the provider.
 */
export function ApiProviderForm({ form, onChange, problems, busy, onEnter }: { form: ApiForm; onChange: (f: ApiForm) => void; problems: string[]; busy: boolean; onEnter: (e: KeyboardEvent) => void }) {
  const id = useId();
  const problemsId = `${id}-problems`;
  const described = problems.length ? problemsId : undefined;
  const setRow = (i: number, row: Partial<ApiHeaderRow>) => onChange({ ...form, headers: form.headers.map((h, j) => (j === i ? { ...h, ...row } : h)) });
  const add = useRef<HTMLDivElement>(null);
  // The row's button goes with it: focus moves to Add, not to the page.
  const removeRow = (i: number) => {
    onChange({ ...form, headers: form.headers.filter((_, j) => j !== i) });
    add.current?.querySelector("button")?.focus();
  };
  return (
    <div className="stack api-provider" style={{ gap: 12 }}>
      <label className="field single">
        <span className="t-label">{t("desktop.keys.api.endpoint")}</span>
        <input
          className="input mono"
          type="url"
          value={form.baseUrl}
          placeholder={t("desktop.keys.api.endpoint_placeholder")}
          spellCheck={false}
          disabled={busy}
          aria-describedby={`${id}-endpoint-hint ${described ?? ""}`.trim()}
          onChange={(e) => onChange({ ...form, baseUrl: e.target.value })}
          onKeyDown={onEnter}
        />
        <small id={`${id}-endpoint-hint`} className="muted">
          {t("desktop.keys.api.endpoint_hint", { placeholder: tokenPlaceholder })}
        </small>
      </label>
      <fieldset className="field single api-headers" aria-describedby={described}>
        <legend className="t-label">{t("desktop.keys.api.headers")}</legend>
        {form.headers.map((h, i) => {
          const n = i + 1;
          return (
            <div key={i} className="row wrap" style={{ gap: 8 }}>
              <input
                className="input mono"
                aria-label={t("desktop.keys.api.header_name", { n })}
                value={h.name}
                placeholder={t("desktop.keys.api.name_placeholder")}
                spellCheck={false}
                disabled={busy}
                onChange={(e) => setRow(i, { name: e.target.value })}
                onKeyDown={onEnter}
              />
              {h.useToken ? (
                <input className="input mono" aria-label={t("desktop.keys.api.header_value", { n })} value={tokenPlaceholder} readOnly disabled={busy} />
              ) : (
                <input
                  className="input mono"
                  type="password"
                  autoComplete="off"
                  aria-label={t("desktop.keys.api.header_value", { n })}
                  value={h.value}
                  placeholder={t(h.hasValue ? "desktop.keys.api.value_keep" : "desktop.keys.api.value_placeholder")}
                  spellCheck={false}
                  disabled={busy}
                  onChange={(e) => setRow(i, { value: e.target.value })}
                  onKeyDown={onEnter}
                />
              )}
              <label className="row" style={{ gap: 6 }}>
                <Switch checked={h.useToken} disabled={busy} label={t("desktop.keys.api.use_token_for", { n })} onChange={(useToken) => setRow(i, { useToken, value: "" })} />
                <span className="t-body-sm" aria-hidden="true">
                  {t("desktop.keys.api.use_token")}
                </span>
              </label>
              <IconButton small icon={mdiDeleteOutline} label={t("desktop.keys.api.remove_header", { n })} disabled={busy} onClick={() => removeRow(i)} />
            </div>
          );
        })}
        <div ref={add}>
          <Button small icon={mdiPlus} disabled={busy} onClick={() => onChange({ ...form, headers: [...form.headers, newHeader()] })}>
            {t("desktop.keys.api.add_header")}
          </Button>
        </div>
      </fieldset>
      {usesToken(form) && (
        <div className="stack" style={{ gap: 8 }}>
          <label className="field single">
            <span className="t-label">{t("desktop.keys.api.language")}</span>
            <select className="select" value={form.language} disabled={busy} onChange={(e) => onChange({ ...form, language: e.target.value })}>
              {scriptLanguages.map((l) => (
                <option key={l} value={l}>
                  {t(`desktop.keys.api.language.${l}`)}
                </option>
              ))}
            </select>
          </label>
          <label className="field single">
            <span className="t-label">{t("desktop.keys.api.script")}</span>
            <textarea
              className="input mono"
              rows={4}
              value={form.tokenScript}
              placeholder={t(`desktop.keys.api.script_placeholder.${form.language}`)}
              spellCheck={false}
              disabled={busy}
              aria-describedby={`${id}-script-hint ${described ?? ""}`.trim()}
              onChange={(e) => onChange({ ...form, tokenScript: e.target.value })}
            />
            <small id={`${id}-script-hint`} className="muted">
              {t("desktop.keys.api.script_hint")}
            </small>
          </label>
        </div>
      )}
      {/* Always present, so a screen reader announces problems as they come. */}
      <ul id={problemsId} className="error-text t-body-sm" aria-live="polite">
        {problems.map((p) => (
          <li key={p}>{t(p)}</li>
        ))}
      </ul>
    </div>
  );
}
