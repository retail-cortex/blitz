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

// Settings › Providers › API (spec_desktop_024 DSK-61; spec_models_015
// §3.4): the api provider's fields, to and from ApiProviderSettings. A
// header's value is never shown (the service withholds it) unless it is
// the token placeholder; an empty value keeps the saved one.
import { create } from "@bufbuild/protobuf";
import { ApiProviderSettingsSchema, type ApiProviderSettings } from "./gen/blitz/v1/config_pb";

/** What a header value or the endpoint writes for the token script's output. */
export const tokenPlaceholder = "{{TOKEN_SCRIPT}}";

/** The languages a token script can be written in; shell is the default. */
export const scriptLanguages = ["shell", "python", "typescript"] as const;

/** One header row as typed. */
export interface ApiHeaderRow {
  name: string;
  /** A new value; "" keeps the saved one (hasValue) or is missing. */
  value: string;
  /** The saved settings have a value for it. */
  hasValue: boolean;
  /** The value is the token: {{TOKEN_SCRIPT}}. */
  useToken: boolean;
}

/** The form's fields, as typed. */
export interface ApiForm {
  baseUrl: string;
  headers: ApiHeaderRow[];
  tokenScript: string;
  language: string;
}

/** A new header row, carrying the token (the usual case). */
export const newHeader = (): ApiHeaderRow => ({ name: "", value: "", hasValue: false, useToken: true });

/** The settings as the form shows them; nothing saved gives one empty row. */
export function toApiForm(s?: ApiProviderSettings): ApiForm {
  const headers = (s?.headers ?? []).map((h) => ({ name: h.name, value: "", hasValue: h.hasValue, useToken: h.value === tokenPlaceholder }));
  return { baseUrl: s?.baseUrl ?? "", headers: headers.length ? headers : [newHeader()], tokenScript: s?.tokenScript ?? "", language: s?.tokenScriptLanguage || "shell" };
}

/** Whether a header or the endpoint uses the token, so the script is needed. */
export const usesToken = (f: ApiForm) => f.baseUrl.includes(tokenPlaceholder) || f.headers.some((h) => h.name.trim() && h.useToken);

/** The rows that name a header: an empty row is left out, not refused. */
const named = (f: ApiForm) => f.headers.filter((h) => h.name.trim());

/** An HTTP header name: a token (RFC 9110 §5.6.2), as the service checks. */
const headerName = /^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/;

/** What stops the form being saved, as message keys (none: it can be). */
export function apiFormProblems(f: ApiForm): string[] {
  const out = new Set<string>();
  if (!/^https?:\/\/[^\s/?#]+\S*$/.test(f.baseUrl.trim())) out.add("desktop.keys.api.problem.base_url");
  const rows = named(f);
  if (!rows.length) out.add("desktop.keys.api.problem.no_headers");
  const seen = new Set<string>();
  for (const h of rows) {
    const name = h.name.trim();
    if (!headerName.test(name)) out.add("desktop.keys.api.problem.header_name");
    if (seen.has(name.toLowerCase())) out.add("desktop.keys.api.problem.header_twice");
    seen.add(name.toLowerCase());
    if (!h.useToken && !h.value && !h.hasValue) out.add("desktop.keys.api.problem.header_value");
    if (/[\r\n]/.test(h.value)) out.add("desktop.keys.api.problem.header_value");
  }
  if (usesToken(f) && !f.tokenScript.trim()) out.add("desktop.keys.api.problem.script");
  return [...out];
}

/** The settings the form saves, as plain data (check apiFormProblems first). */
function settingsInit(f: ApiForm) {
  const token = usesToken(f);
  return {
    baseUrl: f.baseUrl.trim(),
    headers: named(f).map((h) => ({ name: h.name.trim(), value: h.useToken ? tokenPlaceholder : h.value })),
    // A script nothing uses is refused by the service: it isn't sent.
    tokenScript: token ? f.tokenScript.trim() : "",
    tokenScriptLanguage: token ? f.language : "",
  };
}

/** The form as the settings SetProvider saves. */
export const fromApiForm = (f: ApiForm): ApiProviderSettings => create(ApiProviderSettingsSchema, settingsInit(f));

/** Whether two forms save the same settings: a typed value is a change. */
export const sameApiForm = (a: ApiForm, b: ApiForm) => JSON.stringify(settingsInit(a)) === JSON.stringify(settingsInit(b));
