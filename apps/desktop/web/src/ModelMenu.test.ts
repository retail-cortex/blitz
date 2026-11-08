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
import type { MenuEntry } from "@blitz/webui";
import { ListModelsResponseSchema, ListedModelSchema } from "./gen/blitz/v1/config_pb";
import { modelDetail, modelEntries } from "./ModelMenu";

const list = create(ListModelsResponseSchema, {
  providers: [
    {
      provider: "gemini",
      models: [
        { id: "gemini-4-carbon", hasPrice: true, inputPerMtok: 4, outputPerMtok: 20, capabilities: { tools: true, context: 2_000_000 } },
        { id: "gemini-3.8-flash", hasPrice: true, inputPerMtok: 0.75, outputPerMtok: 3.75, capabilities: { tools: true, context: 1_000_000 } },
      ],
    },
    { provider: "anthropic", error: "no API key" },
    { provider: "ollama", models: [{ id: "gemma3:12b", capabilities: { tools: false, context: 128_000 } }] },
  ],
  notSetUp: ["openai"],
});

const shown = (es: MenuEntry[]) => es.map((e) => (e === "divider" ? "—" : "heading" in e ? `# ${e.heading}` : `${e.on ? "✓ " : ""}${e.label}`));
const noop = { choose: () => {}, makeDefault: () => {}, refresh: () => {}, setUp: () => {} };

describe("the model menu (spec_061 MOD-12)", () => {
  it("lists each provider's models under its name, the current one checked, then the actions", () => {
    expect(shown(modelEntries(list, "gemini/gemini-3.8-flash", null, noop))).toEqual([
      "# Gemini",
      "gemini-4-carbon",
      "✓ gemini-3.8-flash",
      "# Anthropic",
      "no API key",
      "# Ollama",
      "gemma3:12b",
      "—",
      "Use for new chats in this workspace",
      "Refresh the list",
      "Set up OpenAI",
    ]);
  });

  it("chooses a model by its reference, and says when the agent's pin decides", () => {
    const chosen: string[] = [];
    const es = modelEntries(list, "gemini-4-carbon", { agent: "Coder", model: "claude-haiku-4-5" }, { ...noop, choose: (r) => chosen.push(r) });
    expect(shown(es)[0]).toBe("Coder runs on claude-haiku-4-5: its pin decides");
    expect(shown(es)).toContain("✓ gemini-4-carbon"); // a bare name matches too
    const item = es.find((e) => e !== "divider" && !("heading" in e) && e.label === "gemma3:12b");
    (item as { onSelect: () => void }).onSelect();
    expect(chosen).toEqual(["ollama/gemma3:12b"]);
  });

  it("says it's listing before the list comes", () => {
    expect(shown(modelEntries(null, "x", null, noop))).toEqual(["# Listing models…"]);
  });

  it("describes a model: price, context, no tools", () => {
    expect(modelDetail(create(ListedModelSchema, { id: "a", hasPrice: true, inputPerMtok: 0.75, outputPerMtok: 3.75, capabilities: { tools: true, context: 1_000_000 } }))).toBe("$0.75 in, $3.75 out per 1M tokens · 1M context");
    expect(modelDetail(create(ListedModelSchema, { id: "b", capabilities: { tools: false, context: 128_000 } }))).toBe("128K context · no tools");
    expect(modelDetail(create(ListedModelSchema, { id: "c", displayName: "Fast" }))).toBe("c");
  });
});
