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
import { PolicySettingSchema } from "./gen/blitz/v1/config_pb";
import { policyDecided, policyLabel, policyValue } from "./OrgPolicy";

// The person's effective policy as Settings › Providers › Organization
// shows it (spec_053 EN-39): each setting, its value, who decided it.
describe("organization policy", () => {
  it("names settings, a provider's and a rule's included", () => {
    expect(policyLabel("connectors.own")).toBe("Connections of your own");
    expect(policyLabel("models.own.ollama.allowed")).toBe("Ollama on your computer");
    expect(policyLabel("models.own.ollama.models")).toBe("Ollama on your computer: models allowed");
    expect(policyLabel("rules.tested")).toContain("tested");
  });

  it("reads values as people do: on, off, a list, every one when empty", () => {
    expect(policyValue("connectors.own", "true")).toBe("On");
    expect(policyValue("models.own.ollama.models", '["llama3*","qwen*"]')).toBe("llama3*, qwen*");
    expect(policyValue("models.own.ollama.models", "[]")).toBe("Every one");
    expect(policyValue("tools.off", "[]")).toBe("None");
    expect(policyValue("sharing.max_link_days", "30")).toBe("30");
    expect(policyValue("refresh", "not json")).toBe("not json");
  });

  it("says who decided, and when it's locked", () => {
    const s = (level: string, by: string[] = [], locked = false) => policyDecided(create(PolicySettingSchema, { path: "connectors.own", level, by, locked }));
    expect(s("group", ["Data", "Ops"])).toBe("Your group: Data, Ops");
    expect(s("person", ["ana@acme.com"])).toBe("Set for you by an admin");
    expect(s("default")).toBe("The default");
    expect(s("global", [], true)).toBe("Your organization · locked");
    expect(s("")).toBe("Your organization");
  });
});
