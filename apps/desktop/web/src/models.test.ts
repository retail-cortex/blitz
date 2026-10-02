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

import { describe, expect, it } from "vitest";
import { catalogFrom, checkModelRef, emptyCatalog, modelOptions, parseModelRef } from "./models";

describe("parseModelRef", () => {
  it.each([
    { ref: "anthropic/claude-sonnet-5", def: "gemini", provider: "anthropic", name: "claude-sonnet-5", prefixed: true },
    { ref: "Gemini/gemini-3.8-flash", def: "openai", provider: "gemini", name: "gemini-3.8-flash", prefixed: true },
    { ref: "Gemini-3.8-flash", def: "gemini", provider: "gemini", name: "gemini-3.8-flash", prefixed: false },
    { ref: "  gemini-3.5-flash-lite ", def: "Gemini", provider: "gemini", name: "gemini-3.5-flash-lite", prefixed: false },
    { ref: "anthropic/Claude-Sonnet-5", def: "gemini", provider: "anthropic", name: "claude-sonnet-5", prefixed: true },
    { ref: "meta-llama/llama-4", def: "openai", provider: "openai", name: "meta-llama/llama-4", prefixed: false },
    { ref: "openai/Qwen/Qwen3-Coder", def: "gemini", provider: "openai", name: "Qwen/Qwen3-Coder", prefixed: true },
    { ref: "ollama/MyModel:Latest", def: "gemini", provider: "ollama", name: "MyModel:Latest", prefixed: true },
    { ref: "anthropic/", def: "gemini", provider: "gemini", name: "anthropic/", prefixed: false },
  ])("reads $ref on $def", ({ ref, def, provider, name, prefixed }) => {
    expect(parseModelRef(ref, def)).toEqual({ provider, name, prefixed });
  });
});

describe("checkModelRef", () => {
  const catalog = catalogFrom({
    defaultProvider: "gemini",
    providers: [
      { provider: "gemini", ids: ["gemini-3.8-flash", "gemini-3.8-pro"], error: "", note: "" },
      { provider: "anthropic", ids: [], error: "no key", note: "" },
      { provider: "bedrock", ids: ["anthropic.claude-x"], error: "", note: "the configured model" },
    ] as never,
  });
  it.each([
    { name: "empty: the default", ref: "  ", value: "", error: undefined, warning: undefined },
    { name: "capitals lowered", ref: "Gemini-3.8-flash", value: "gemini-3.8-flash", error: undefined, warning: undefined },
    { name: "a prefix kept, lowered", ref: "GEMINI/Gemini-3.8-Pro", value: "gemini/gemini-3.8-pro", error: undefined, warning: undefined },
    { name: "spaces", ref: "gemini 3", value: "gemini 3", error: "desktop.model.spaces", warning: undefined },
    { name: "a provider and no name", ref: "anthropic/", value: "anthropic/", error: "desktop.model.no_name", warning: undefined },
    { name: "not listed", ref: "gemini-nope", value: "gemini-nope", error: undefined, warning: "desktop.model.unlisted" },
    { name: "a provider that couldn't list", ref: "anthropic/claude-nope", value: "anthropic/claude-nope", error: undefined, warning: undefined },
    { name: "a partial list", ref: "bedrock/other", value: "bedrock/other", error: undefined, warning: undefined },
  ])("$name", ({ ref, value, error, warning }) => {
    const c = checkModelRef(ref, catalog);
    expect(c.value).toBe(value);
    expect(c.error).toBe(error);
    expect(c.warning?.key).toBe(warning);
  });
  it("names the provider and model it doesn't know", () => {
    expect(checkModelRef("gemini-nope", catalog).warning?.params).toEqual({ provider: "gemini", model: "gemini-nope" });
  });
  it("reads a bare name as the given provider's", () => {
    expect(checkModelRef("Claude-X", catalog, "anthropic").value).toBe("claude-x");
    expect(checkModelRef("Claude-X", emptyCatalog).value).toBe("Claude-X");
  });
  it("offers extras first, then every listed model, once", () => {
    expect(modelOptions(catalog, ["gemini/gemini-3.8-pro", "", "mine"])).toEqual(["gemini/gemini-3.8-pro", "mine", "gemini/gemini-3.8-flash", "bedrock/anthropic.claude-x"]);
  });
});
