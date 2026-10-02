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

// Model names as typed in a form: read as the service reads them
// (runtime.ParseModelRef), lowercased where the provider's names always
// are, and checked against the models the providers list.
import type { ListModelsResponse } from "./gen/blitz/v1/config_pb";

/** Providers a "provider/" prefix may name. */
export const knownProviders = ["gemini", "anthropic", "openai", "ollama", "bedrock", "azure", "vertex-anthropic"];

/** Providers whose model names are always lowercase. */
const lowercaseProviders = new Set(["gemini", "anthropic", "vertex-anthropic"]);

/**
 * Splits "provider/model" as the service does: a prefix counts only when
 * it's a known provider (any case) with a name after it; otherwise the
 * whole reference is a model of defaultProvider ("meta-llama/llama-4" on
 * openai). Names of providers that are always lowercase are lowercased.
 */
export function parseModelRef(ref: string, defaultProvider: string): { provider: string; name: string; prefixed: boolean } {
  const r = ref.trim();
  let provider = defaultProvider.toLowerCase();
  let name = r;
  let prefixed = false;
  const slash = r.indexOf("/");
  if (slash > 0 && knownProviders.includes(r.slice(0, slash).toLowerCase()) && slash < r.length - 1) {
    provider = r.slice(0, slash).toLowerCase();
    name = r.slice(slash + 1);
    prefixed = true;
  }
  if (lowercaseProviders.has(provider)) name = name.toLowerCase();
  return { provider, name, prefixed };
}

/** The models each provider listed; providers that couldn't list, or list only part, aren't in it. */
export interface ModelCatalog {
  defaultProvider: string;
  /** Provider to its model IDs, for providers whose list is complete. */
  listed: Map<string, Set<string>>;
  /** Every model as "provider/id", for suggesting. */
  all: string[];
}

/** An empty catalog: format checks only. */
export const emptyCatalog: ModelCatalog = { defaultProvider: "", listed: new Map(), all: [] };

/** Makes a catalog from ListModels. */
export function catalogFrom(r: Pick<ListModelsResponse, "providers" | "defaultProvider">): ModelCatalog {
  const listed = new Map<string, Set<string>>();
  const all: string[] = [];
  for (const p of r.providers) {
    if (p.error) continue;
    for (const id of p.ids) all.push(`${p.provider}/${id}`);
    if (!p.note && p.ids.length > 0) listed.set(p.provider, new Set(p.ids));
  }
  return { defaultProvider: r.defaultProvider, listed, all };
}

/** What a model field holds: the reference to save, and what's wrong with it. */
export interface ModelCheck {
  /** Trimmed and, for gemini and anthropic, lowercased. */
  value: string;
  /** An i18n key: the reference can't be a model name. Blocks saving. */
  error?: string;
  /** An i18n key and its parameters: the provider doesn't list it. */
  warning?: { key: string; params: Record<string, string> };
}

/**
 * Checks a typed model reference. Empty is fine (the field's default). A
 * name with spaces, or a known provider with no name after it, is an error;
 * a name the provider's list lacks is a warning (it's sent as written).
 * defaultProvider, when given, wins over the catalog's.
 */
export function checkModelRef(ref: string, catalog: ModelCatalog, defaultProvider = catalog.defaultProvider): ModelCheck {
  const trimmed = ref.trim();
  if (!trimmed) return { value: "" };
  if (/\s/.test(trimmed)) return { value: trimmed, error: "desktop.model.spaces" };
  const slash = trimmed.indexOf("/");
  if (slash > 0 && slash === trimmed.length - 1 && knownProviders.includes(trimmed.slice(0, slash).toLowerCase())) {
    return { value: trimmed, error: "desktop.model.no_name" };
  }
  const { provider, name, prefixed } = parseModelRef(trimmed, defaultProvider);
  const value = prefixed ? `${provider}/${name}` : name;
  const ids = catalog.listed.get(provider);
  if (ids && !ids.has(name)) return { value, warning: { key: "desktop.model.unlisted", params: { provider, model: name } } };
  return { value };
}

/** The suggestions for a model field: the catalog's, then extras, without repeats. */
export function modelOptions(catalog: ModelCatalog, extras: string[] = []): string[] {
  return [...new Set([...extras.filter(Boolean), ...catalog.all])];
}
