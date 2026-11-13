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

// Settings › Providers › API (spec_024_desktop DSK-61; spec_015_models
// §3.4): the api provider's fields, to and from ApiProviderSettings. A
// header's value is never shown (the service withholds it) unless it uses
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
  /**
   * The value written while useToken: the placeholder with the saved text
   * around it ("Bearer {{TOKEN_SCRIPT}}"); unset for the bare placeholder.
   */
  token?: string;
}

/** The APIs a model behind the gateway can speak (spec_015_models MDL-112). */
export const gatewayAPIs = ["responses", "chat_completions", "messages", "gemini", "vertex"] as const;

/** Where each API's requests go when the form doesn't say (config's DefaultGatewayPaths). */
export const defaultPaths: Record<string, string> = {
  responses: "/responses",
  chat_completions: "/chat/completions",
  messages: "/responses",
  gemini: "/models/{model}:{method}",
  vertex: "/publishers/google/models/{model}:{method}",
};

/** Where the gateway lists its models when the form doesn't say (config's DefaultModelsPath). */
export const defaultModelsPath = "/models";

/** One model's API, as typed. */
export interface ApiModelRow {
  model: string;
  api: string;
}

/** The form's fields, as typed. */
export interface ApiForm {
  baseUrl: string;
  headers: ApiHeaderRow[];
  tokenScript: string;
  language: string;
  /** Whether the gateway lists its models (models_path isn't "none"), and where: a path ("" for /models) or another server's URL (MDL-115). */
  listsModels: boolean;
  modelsPath: string;
  /** The list's own headers, in place of headers (none: the gateway's, on its server). */
  modelsHeaders: ApiHeaderRow[];
  /** Each model's API (MDL-112), and every other model's ("" goes by its name). */
  models: ApiModelRow[];
  otherApi: string;
  /** Each API's path, by API ("" for its default, MDL-113). */
  paths: Record<string, string>;
}

/** A model's API as Blitz guesses it from its name when the form doesn't say (config's APIOf). */
export function guessedAPI(model: string): string {
  const name = model.toLowerCase();
  if (name.startsWith("claude")) return "messages";
  if (name.startsWith("gemini")) return "gemini";
  return "responses";
}

/** A new header row, carrying the token (the usual case). */
export const newHeader = (): ApiHeaderRow => ({ name: "", value: "", hasValue: false, useToken: true });

/** The settings as the form shows them; nothing saved gives one empty row. */
export function toApiForm(s?: ApiProviderSettings): ApiForm {
  const headers = headerRows(s?.headers ?? []);
  const apis = s?.apis ?? {};
  return {
    baseUrl: s?.baseUrl ?? "",
    headers: headers.length ? headers : [newHeader()],
    tokenScript: s?.tokenScript ?? "",
    language: s?.tokenScriptLanguage || "shell",
    listsModels: s?.modelsPath !== "none",
    modelsPath: s?.modelsPath === "none" ? "" : (s?.modelsPath ?? ""),
    modelsHeaders: headerRows(s?.modelsHeaders ?? []),
    models: Object.keys(apis)
      .filter((m) => m !== "*")
      .sort()
      .map((model) => ({ model, api: apis[model] })),
    otherApi: apis["*"] ?? "",
    paths: { ...(s?.paths ?? {}) },
  };
}

// Saved headers as rows: a value using the placeholder anywhere is the
// token, as the service has it (config's UsesToken); its text around the
// placeholder is kept.
function headerRows(saved: ApiProviderSettings["headers"]): ApiHeaderRow[] {
  return saved.map((h): ApiHeaderRow => {
    const useToken = h.value.includes(tokenPlaceholder);
    return { name: h.name, value: "", hasValue: h.hasValue, useToken, ...(useToken && h.value !== tokenPlaceholder ? { token: h.value } : {}) };
  });
}

/** Whether models_path is another server's URL rather than a path under the endpoint (MDL-115). */
export const isModelsURL = (p: string) => /^https?:\/\//.test(p.trim());

/** The models the gateway lists that the form doesn't name yet, each with its guessed API. */
export const listedRows = (f: ApiForm, listed: string[]): ApiModelRow[] =>
  listed.filter((m) => !f.models.some((r) => r.model.trim() === m)).map((model) => ({ model, api: guessedAPI(model) }));

/** What a header using the token writes: its saved text, else the placeholder. */
export const tokenValue = (h: ApiHeaderRow) => h.token || tokenPlaceholder;

/** Whether a header, the endpoint or the model list uses the token, so the script is needed. */
export const usesToken = (f: ApiForm) =>
  f.baseUrl.includes(tokenPlaceholder) || f.headers.some((h) => h.name.trim() && h.useToken) || (f.listsModels && (f.modelsPath.includes(tokenPlaceholder) || f.modelsHeaders.some((h) => h.name.trim() && h.useToken)));

/** The rows that name a header: an empty row is left out, not refused. */
const named = (rows: ApiHeaderRow[]) => rows.filter((h) => h.name.trim());

/** An HTTP header name: a token (RFC 9110 §5.6.2), as the service checks. */
const headerName = /^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/;

/** What stops the form being saved, as message keys (none: it can be). */
export function apiFormProblems(f: ApiForm): string[] {
  const out = new Set<string>();
  if (!/^https?:\/\/[^\s/?#]+\S*$/.test(f.baseUrl.trim())) out.add("desktop.keys.api.problem.base_url");
  if (!named(f.headers).length) out.add("desktop.keys.api.problem.no_headers");
  for (const rows of f.listsModels ? [f.headers, f.modelsHeaders] : [f.headers]) {
    const seen = new Set<string>();
    for (const h of named(rows)) {
      const name = h.name.trim();
      if (!headerName.test(name)) out.add("desktop.keys.api.problem.header_name");
      if (seen.has(name.toLowerCase())) out.add("desktop.keys.api.problem.header_twice");
      seen.add(name.toLowerCase());
      if (!h.useToken && !h.value && !h.hasValue) out.add("desktop.keys.api.problem.header_value");
      if (/[\r\n]/.test(h.value)) out.add("desktop.keys.api.problem.header_value");
    }
  }
  if (usesToken(f) && !f.tokenScript.trim()) out.add("desktop.keys.api.problem.script");
  const models = new Set<string>();
  for (const r of f.models) {
    const m = r.model.trim();
    if (m && models.has(m)) out.add("desktop.keys.api.problem.model_twice");
    models.add(m);
  }
  const paths = Object.entries(f.paths).filter(([, p]) => p.trim());
  if (f.listsModels && isModelsURL(f.modelsPath)) {
    // Another server's URL: its host plain, the token only after it.
    if (!/^https?:\/\/[^\s/?#{}]+(?:[/?][^\s#]*)?$/.test(f.modelsPath.trim())) out.add("desktop.keys.api.problem.models_url");
  } else if (f.listsModels && f.modelsPath.trim()) paths.push(["", f.modelsPath]);
  for (const [api, raw] of paths) {
    const p = raw.trim();
    const google = api === "gemini" || api === "vertex";
    const fills = (p.match(/\{[^}]*\}/g) ?? []).every((x) => x === "{model}" || (google && x === "{method}"));
    if (!p.startsWith("/") || /[?#\s]/.test(p) || p.includes(tokenPlaceholder) || !fills || (google && !p.includes("{method}"))) out.add("desktop.keys.api.problem.path");
  }
  return [...out];
}

/** The settings the form saves, as plain data (check apiFormProblems first). */
function settingsInit(f: ApiForm) {
  const token = usesToken(f);
  return {
    baseUrl: f.baseUrl.trim(),
    headers: named(f.headers).map((h) => ({ name: h.name.trim(), value: h.useToken ? tokenValue(h) : h.value })),
    // A script nothing uses is refused by the service: it isn't sent.
    tokenScript: token ? f.tokenScript.trim() : "",
    tokenScriptLanguage: token ? f.language : "",
    modelsPath: f.listsModels ? f.modelsPath.trim() : "none",
    // Only while something lists the models: a header nothing sends isn't kept.
    modelsHeaders: f.listsModels ? named(f.modelsHeaders).map((h) => ({ name: h.name.trim(), value: h.useToken ? tokenValue(h) : h.value })) : [],
    // A row without a model isn't saved; sorted, so the order rows were
    // added in isn't a change.
    apis: Object.fromEntries(
      [...f.models.map((r) => [r.model.trim(), r.api] as const).filter(([m]) => m), ...(f.otherApi ? [["*", f.otherApi] as const] : [])].sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)),
    ),
    paths: Object.fromEntries(Object.entries(f.paths).flatMap(([api, p]) => (p.trim() && p.trim() !== defaultPaths[api] ? [[api, p.trim()]] : []))),
  };
}

/** The form as the settings SetProvider saves. */
export const fromApiForm = (f: ApiForm): ApiProviderSettings => create(ApiProviderSettingsSchema, settingsInit(f));

/** Whether two forms save the same settings: a typed value is a change. */
export const sameApiForm = (a: ApiForm, b: ApiForm) => JSON.stringify(settingsInit(a)) === JSON.stringify(settingsInit(b));
