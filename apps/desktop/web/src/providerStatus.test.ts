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
import { DescribeConfigResponseSchema, KeySource, SignInStatusSchema, type ProviderConfig } from "./gen/blitz/v1/config_pb";
import { noProviderReady, plainReason, providerState, type OllamaProbe, type ProviderFacts } from "./providerStatus";

const facts = (providers: Partial<ProviderConfig>[], more: { signedIn?: string[]; checking?: boolean; ollama?: OllamaProbe; gateway?: string } = {}): ProviderFacts => ({
  desc: create(DescribeConfigResponseSchema, { providers: providers as ProviderConfig[], api: { baseUrl: more.gateway ?? "" } }),
  signIns: more.checking
    ? {}
    : Object.fromEntries(["google", "anthropic"].map((p) => [p, create(SignInStatusSchema, { provider: p, signedIn: !!more.signedIn?.includes(p), detail: "you@example.com" })])),
  ollama: "ollama" in more ? more.ollama : { running: false, models: 0 },
});

describe("providerState", () => {
  it.each([
    ["a key in the keychain", "gemini", facts([{ name: "gemini", keySource: KeySource.KEYCHAIN }]), { key: "keychain", tone: "ok", ready: true }],
    ["a key from the environment", "anthropic", facts([{ name: "anthropic", keySource: KeySource.ENVIRONMENT }]), { key: "environment", tone: "ok", ready: true }],
    ["the global key, in a workspace", "openai", facts([{ name: "openai", keySource: KeySource.INHERITED }]), { key: "inherited", tone: "ok", ready: true }],
    ["a key in the file", "anthropic", facts([{ name: "anthropic", keySource: KeySource.PLAIN }]), { key: "plain", tone: "warn", ready: true }],
    ["a key the keychain lost", "gemini", facts([{ name: "gemini", keySource: KeySource.KEYCHAIN, keyMissing: true }]), { key: "key_missing", tone: "danger", ready: false }],
    ["nothing", "openai", facts([{ name: "openai", keySource: KeySource.NONE }]), { key: "not_set", tone: "none", ready: false }],
    ["a gcloud sign-in found", "gemini", facts([{ name: "gemini", keySource: KeySource.NONE }], { signedIn: ["google"] }), { key: "found_google", tone: "none", ready: false, found: "adc" }],
    ["an ant sign-in found", "anthropic", facts([{ name: "anthropic", keySource: KeySource.NONE }], { signedIn: ["anthropic"] }), { key: "found_anthropic", tone: "none", ready: false, found: "oauth" }],
    ["signed in with ADC", "gemini", facts([{ name: "gemini", auth: "adc" }], { signedIn: ["google"] }), { key: "signed_in", params: { detail: "you@example.com" }, tone: "ok", ready: true }],
    ["set to sign in, signed out", "anthropic", facts([{ name: "anthropic", auth: "oauth" }]), { key: "signed_out", tone: "warn", ready: false }],
    ["a sign-in still being asked", "anthropic", facts([{ name: "anthropic", auth: "oauth" }], { checking: true }), { key: "checking", tone: "checking", ready: false }],
    ["Ollama running", "ollama", facts([], { ollama: { running: true, models: 3 } }), { key: "ollama_running", params: { count: 3 }, tone: "ok", ready: true }],
    ["Ollama not running", "ollama", facts([]), { key: "ollama_off", tone: "none", ready: false }],
    ["Ollama still being asked", "ollama", facts([], { ollama: undefined }), { key: "checking", tone: "checking", ready: false }],
    ["a gateway", "api", facts([], { gateway: "https://gw.example.com" }), { key: "gateway", params: { url: "https://gw.example.com" }, tone: "ok", ready: true }],
    ["no gateway", "api", facts([]), { key: "not_set", tone: "none", ready: false }],
  ])("%s", (_, provider, f, want) => {
    expect(providerState(provider, f)).toEqual(want);
  });
});

describe("noProviderReady", () => {
  it.each([
    ["nothing set up", facts([{ name: "gemini", keySource: KeySource.NONE }]), true],
    ["a key", facts([{ name: "gemini", keySource: KeySource.ENVIRONMENT }]), false],
    ["Ollama running", facts([], { ollama: { running: true, models: 1 } }), false],
    ["Ollama still being asked", facts([], { ollama: undefined }), false],
    ["only a sign-in found (it still needs setting up)", facts([{ name: "gemini", keySource: KeySource.NONE }], { signedIn: ["google"] }), true],
  ])("%s", (_, f, want) => {
    expect(noProviderReady(f)).toBe(want);
  });
});

describe("plainReason", () => {
  it.each([
    ["the Gemini API key is missing (set GEMINI_API_KEY)", "the Gemini API key is missing"],
    ["no key (set ANTHROPIC_API_KEY or ANTHROPIC_AUTH_TOKEN), try again", "no key, try again"],
    ["401 Unauthorized", "401 Unauthorized"],
  ])("%s", (reason, want) => {
    expect(plainReason(reason)).toBe(want);
  });
});
