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
import { apiFormProblems, fromApiForm, newHeader, sameApiForm, toApiForm, tokenPlaceholder, usesToken, type ApiForm } from "./apiForm";

const saved = create(ApiProviderSettingsSchema, {
  baseUrl: "https://gw.example/v1",
  headers: [
    { name: "Authorization", value: tokenPlaceholder, hasValue: true },
    { name: "X-Team", value: "", hasValue: true },
  ],
  tokenScript: "print-token",
  tokenScriptLanguage: "",
});

const fine: ApiForm = { baseUrl: "https://gw.example/v1", headers: [{ name: "Authorization", value: "", hasValue: false, useToken: true }], tokenScript: "print-token", language: "shell" };

describe("the api form", () => {
  it("shows the saved settings, withholding values", () => {
    expect(toApiForm(saved)).toEqual({
      baseUrl: "https://gw.example/v1",
      headers: [
        { name: "Authorization", value: "", hasValue: true, useToken: true },
        { name: "X-Team", value: "", hasValue: true, useToken: false },
      ],
      tokenScript: "print-token",
      language: "shell",
    });
  });

  it("starts with one empty row carrying the token", () => {
    expect(toApiForm()).toEqual({ baseUrl: "", headers: [newHeader()], tokenScript: "", language: "shell" });
  });

  it("saves the token's placeholder, keeps a withheld value and leaves out empty rows", () => {
    const f = toApiForm(saved);
    const s = fromApiForm({ ...f, baseUrl: " https://gw.example/v1 ", headers: [...f.headers, newHeader()] });
    expect(s).toMatchObject({
      baseUrl: "https://gw.example/v1",
      headers: [
        { name: "Authorization", value: tokenPlaceholder },
        { name: "X-Team", value: "" },
      ],
      tokenScript: "print-token",
      tokenScriptLanguage: "shell",
    });
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
