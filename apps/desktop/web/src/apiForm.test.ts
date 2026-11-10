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

import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { ApiProviderSettingsSchema } from "./gen/blitz/v1/config_pb";
import { apiFormProblems, fromApiForm, guessedAPI, listedRows, newHeader, sameApiForm, toApiForm, tokenPlaceholder, usesToken, type ApiForm } from "./apiForm";

const saved = create(ApiProviderSettingsSchema, {
  baseUrl: "https://gw.example/v1",
  headers: [
    { name: "Authorization", value: tokenPlaceholder, hasValue: true },
    { name: "X-Team", value: "", hasValue: true },
    { name: "X-Gateway", value: `Bearer ${tokenPlaceholder}`, hasValue: true },
  ],
  tokenScript: "print-token",
  tokenScriptLanguage: "",
});

const routes = { listsModels: true, modelsPath: "", models: [], otherApi: "", paths: {} };
const fine: ApiForm = { baseUrl: "https://gw.example/v1", headers: [{ name: "Authorization", value: "", hasValue: false, useToken: true }], tokenScript: "print-token", language: "shell", ...routes };

describe("the api form", () => {
  it("shows the saved settings, withholding values", () => {
    expect(toApiForm(saved)).toEqual({
      baseUrl: "https://gw.example/v1",
      headers: [
        { name: "Authorization", value: "", hasValue: true, useToken: true },
        { name: "X-Team", value: "", hasValue: true, useToken: false },
        { name: "X-Gateway", value: "", hasValue: true, useToken: true, token: `Bearer ${tokenPlaceholder}` },
      ],
      tokenScript: "print-token",
      language: "shell",
      ...routes,
    });
  });

  it("starts with one empty row carrying the token", () => {
    expect(toApiForm()).toEqual({ baseUrl: "", headers: [newHeader()], tokenScript: "", language: "shell", ...routes });
  });

  // Issue #105 (spec_015_models MDL-110–112).
  it("shows and saves each model's API, every other model's, the paths and the list", () => {
    const f = toApiForm(create(ApiProviderSettingsSchema, { ...saved, modelsPath: "/v1/models", apis: { "gemini-3-pro": "vertex", "*": "chat_completions", "claude-sonnet-5-5": "messages" }, paths: { messages: "/v1/messages" } }));
    expect(f).toMatchObject({
      listsModels: true,
      modelsPath: "/v1/models",
      models: [
        { model: "claude-sonnet-5-5", api: "messages" },
        { model: "gemini-3-pro", api: "vertex" },
      ],
      otherApi: "chat_completions",
      paths: { messages: "/v1/messages" },
    });
    const edited = { ...f, models: [{ model: " glm-5 ", api: "messages" }, ...f.models, { model: "", api: "gemini" }], paths: { messages: " /v1/messages ", responses: "/responses", gemini: "" } };
    expect(fromApiForm(edited)).toMatchObject({
      modelsPath: "/v1/models",
      apis: { "*": "chat_completions", "claude-sonnet-5-5": "messages", "gemini-3-pro": "vertex", "glm-5": "messages" },
      paths: { messages: "/v1/messages" },
    });
    expect(Object.keys(fromApiForm(edited).apis)).toEqual(["*", "claude-sonnet-5-5", "gemini-3-pro", "glm-5"]);
    expect(fromApiForm({ ...f, listsModels: false }).modelsPath).toBe("none");
    expect(toApiForm(create(ApiProviderSettingsSchema, { modelsPath: "none" }))).toMatchObject({ listsModels: false, modelsPath: "" });
    expect(sameApiForm(f, { ...f, models: [...f.models].reverse() }), "the rows' order isn't a change").toBe(true);
    expect(sameApiForm(f, { ...f, otherApi: "" })).toBe(false);
  });

  it("guesses an API from the name, and adds the gateway's models it doesn't name", () => {
    expect(["claude-opus-5", "Gemini-3-pro", "gpt-6-luna", "llama-4"].map(guessedAPI)).toEqual(["messages", "gemini", "responses", "responses"]);
    const f = { ...fine, models: [{ model: "gpt-6-luna", api: "chat_completions" }] };
    expect(listedRows(f, ["gpt-6-luna", "claude-sonnet-5-5", "gemini-3-pro"])).toEqual([
      { model: "claude-sonnet-5-5", api: "messages" },
      { model: "gemini-3-pro", api: "gemini" },
    ]);
  });

  it("refuses a model twice and paths the service would", () => {
    const twice = { ...fine, models: [{ model: "m", api: "responses" }, { model: " m ", api: "messages" }] };
    expect(apiFormProblems(twice)).toEqual(["desktop.keys.api.problem.model_twice"]);
    const bad: Record<string, string>[] = [{ responses: "responses" }, { responses: "/r?x=1" }, { chat_completions: "/{deployment}/chat" }, { messages: "/{method}" }, { gemini: "/models/{model}" }, { responses: `/${tokenPlaceholder}` }];
    for (const paths of bad) {
      expect(apiFormProblems({ ...fine, paths }), JSON.stringify(paths)).toEqual(["desktop.keys.api.problem.path"]);
    }
    expect(apiFormProblems({ ...fine, modelsPath: "models" })).toEqual(["desktop.keys.api.problem.path"]);
    expect(apiFormProblems({ ...fine, listsModels: false, modelsPath: "models" }), "a path not used isn't checked").toEqual([]);
    expect(apiFormProblems({ ...fine, paths: { vertex: "/v1/projects/p/locations/global/publishers/google/models/{model}:{method}", chat_completions: "/v1/chat/completions" } })).toEqual([]);
  });

  it("saves the token's placeholder, keeps a withheld value and leaves out empty rows", () => {
    const f = toApiForm(saved);
    const s = fromApiForm({ ...f, baseUrl: " https://gw.example/v1 ", headers: [...f.headers, newHeader()] });
    expect(s).toMatchObject({
      baseUrl: "https://gw.example/v1",
      headers: [
        { name: "Authorization", value: tokenPlaceholder },
        { name: "X-Team", value: "" },
        { name: "X-Gateway", value: `Bearer ${tokenPlaceholder}` },
      ],
      tokenScript: "print-token",
      tokenScriptLanguage: "shell",
    });
  });

  // N26 desktop-1#2: "Bearer {{TOKEN_SCRIPT}}" uses the token, as the
  // service has it, so the script is kept and sent.
  it("takes a header using the placeholder inside its value as the token", () => {
    const f = toApiForm(create(ApiProviderSettingsSchema, { baseUrl: "https://gw.example/v1", headers: [{ name: "Authorization", value: `Bearer ${tokenPlaceholder}`, hasValue: true }], tokenScript: "print-token" }));
    expect(usesToken(f)).toBe(true);
    expect(fromApiForm(f)).toMatchObject({ headers: [{ name: "Authorization", value: `Bearer ${tokenPlaceholder}` }], tokenScript: "print-token" });
  });

  it("doesn't send a script nothing uses", () => {
    const s = fromApiForm({ ...fine, headers: [{ name: "X-Key", value: "k", hasValue: false, useToken: false }], language: "python" });
    expect(s).toMatchObject({ tokenScript: "", tokenScriptLanguage: "" });
  });

  it("knows when the token is used", () => {
    expect(usesToken(fine)).toBe(true);
    expect(usesToken({ ...fine, headers: [{ ...newHeader(), name: " " }] })).toBe(false);
    expect(usesToken({ ...fine, headers: [], baseUrl: `https://gw.example/${tokenPlaceholder}` })).toBe(true);
  });

  it("is unchanged until something saved would differ", () => {
    const f = toApiForm(saved);
    expect(sameApiForm(f, toApiForm(saved))).toBe(true);
    expect(sameApiForm({ ...f, headers: [...f.headers, newHeader()] }, f)).toBe(true);
    expect(sameApiForm({ ...f, headers: f.headers.map((h, i) => (i === 1 ? { ...h, value: "new" } : h)) }, f)).toBe(false);
    expect(sameApiForm({ ...f, tokenScript: "other" }, f)).toBe(false);
  });

  it.each([
    { name: "fine", f: fine, want: [] },
    { name: "no endpoint", f: { ...fine, baseUrl: "" }, want: ["desktop.keys.api.problem.base_url"] },
    { name: "not http", f: { ...fine, baseUrl: "ftp://gw" }, want: ["desktop.keys.api.problem.base_url"] },
    { name: "no headers", f: { ...fine, headers: [newHeader()] }, want: ["desktop.keys.api.problem.no_headers"] },
    { name: "bad name", f: { ...fine, headers: [{ ...newHeader(), name: "X Key" }] }, want: ["desktop.keys.api.problem.header_name"] },
    { name: "twice", f: { ...fine, headers: [{ ...newHeader(), name: "Auth" }, { ...newHeader(), name: "auth" }] }, want: ["desktop.keys.api.problem.header_twice"] },
    { name: "no value", f: { ...fine, headers: [{ name: "X-Key", value: "", hasValue: false, useToken: false }] }, want: ["desktop.keys.api.problem.header_value"] },
    { name: "kept value", f: { ...fine, headers: [{ name: "X-Key", value: "", hasValue: true, useToken: false }] }, want: [] },
    { name: "newline", f: { ...fine, headers: [{ name: "X-Key", value: "a\nb", hasValue: false, useToken: false }] }, want: ["desktop.keys.api.problem.header_value"] },
    { name: "no script", f: { ...fine, tokenScript: " " }, want: ["desktop.keys.api.problem.script"] },
  ])("$name", ({ f, want }) => expect(apiFormProblems(f)).toEqual(want));
});
