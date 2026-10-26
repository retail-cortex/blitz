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

// Where each provider stands, for Providers & keys' list: set up (and
// how), found on this machine but not set up yet (an environment key, a
// gcloud or ant sign-in, Ollama running), or neither. Worked out from
// DescribeConfig, GetSignIn and an Ollama listing, so the list shows
// every provider's state at once instead of one at a time.
import { KeySource, type DescribeConfigResponse, type SignInStatus } from "./gen/blitz/v1/config_pb";
import { signInProvider } from "./signin";

/** The providers the list offers, in its order. */
export const listedProviders = ["gemini", "anthropic", "openai", "ollama", "api"] as const;

/** Ollama on this machine: unknown until asked, then running (with its models) or not. */
export type OllamaProbe = { running: boolean; models: number } | undefined;

/** What the state is worked out from. */
export interface ProviderFacts {
  desc: DescribeConfigResponse;
  /** GetSignIn's statuses by their provider (google, anthropic); missing while asked. */
  signIns: Record<string, SignInStatus | undefined>;
  ollama: OllamaProbe;
}

/**
 * A provider's state: its line (desktop.keys.status.<key>, with params),
 * its tone, whether it can be used as it is (Use saves it, no more
 * fields), and, for one found but not set up, the sign-in method that
 * would use what was found (Set up opens the form on it).
 */
export interface ProviderState {
  key: string;
  params?: Record<string, string | number>;
  tone: "ok" | "warn" | "danger" | "none" | "checking";
  ready: boolean;
  found?: string;
}

const signInOf = (f: ProviderFacts, provider: string) => f.signIns[provider];

/** Where the provider stands in the scope f describes. */
export function providerState(provider: string, f: ProviderFacts): ProviderState {
  if (provider === "api") return f.desc.api?.baseUrl ? { key: "gateway", params: { url: f.desc.api.baseUrl }, tone: "ok", ready: true } : { key: "not_set", tone: "none", ready: false };
  if (provider === "ollama") {
    if (!f.ollama) return { key: "checking", tone: "checking", ready: false };
    return f.ollama.running ? { key: "ollama_running", params: { count: f.ollama.models }, tone: "ok", ready: true } : { key: "ollama_off", tone: "none", ready: false };
  }
  const p = f.desc.providers.find((x) => x.name === provider);
  const method = p?.auth ?? "";
  if (method && method !== "api_key") {
    const s = signInOf(f, signInProvider(method, provider));
    if (!s) return { key: "checking", tone: "checking", ready: false };
    if (!s.signedIn) return { key: "signed_out", tone: "warn", ready: false };
    return s.detail ? { key: "signed_in", params: { detail: s.detail }, tone: "ok", ready: true } : { key: "signed_in_plain", tone: "ok", ready: true };
  }
  if (p?.keyMissing) return { key: "key_missing", tone: "danger", ready: false };
  switch (p?.keySource) {
    case KeySource.KEYCHAIN:
      return { key: "keychain", tone: "ok", ready: true };
    case KeySource.ENVIRONMENT:
      return { key: "environment", tone: "ok", ready: true };
    case KeySource.INHERITED:
      return { key: "inherited", tone: "ok", ready: true };
    case KeySource.PLAIN:
    case KeySource.OBFUSCATED:
      return { key: "plain", tone: "warn", ready: true };
  }
  // No key: a sign-in on this machine that would do instead.
  if (provider === "gemini" && signInOf(f, "google")?.signedIn) return { key: "found_google", tone: "none", ready: false, found: "adc" };
  if (provider === "anthropic" && signInOf(f, "anthropic")?.signedIn) return { key: "found_anthropic", tone: "none", ready: false, found: "oauth" };
  return { key: "not_set", tone: "none", ready: false };
}

/**
 * Whether no provider can be used as it is, as far as is known: the
 * welcome page then asks for one. While one is still being checked the
 * answer is no, so the card doesn't flash up and away.
 */
export function noProviderReady(f: ProviderFacts): boolean {
  const states = listedProviders.map((p) => providerState(p, f));
  return states.every((s) => !s.ready && s.tone !== "checking");
}

/** Where to get a provider's API key, or Ollama itself ("" for none). */
export const keyPages: Record<string, string> = {
  gemini: "https://aistudio.google.com/apikey",
  anthropic: "https://console.anthropic.com/settings/keys",
  openai: "https://platform.openai.com/api-keys",
  ollama: "https://ollama.com/download",
};

/**
 * A model problem as the window says it: without the service's hint to
 * set an environment variable (the CLI's way; the window's is Set up
 * provider, beside it).
 */
export const plainReason = (reason: string) => reason.replace(/\s*\(set [A-Z0-9_]+(?: or [A-Z0-9_]+)*\)/g, "");
