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

import { useEffect, useId, useState, type KeyboardEvent } from "react";
import { config } from "./api";
import { t } from "./i18n";
import { catalogFrom, checkModelRef, emptyCatalog, modelOptions, type ModelCatalog } from "./models";

// The models each workspace's providers list, asked once per window (by
// workspace, "" for the user's settings, and the one provider asked for,
// "" for all) and kept until refreshed.
const catalogs = new Map<string, Promise<ModelCatalog>>();
const catalogKey = (workspace: string, provider: string) => `${workspace}\n${provider}`;

/** Asks for the workspace's models again (after a provider or key changes). */
export function refreshModelCatalog(workspace: string) {
  for (const k of [...catalogs.keys()]) if (k.startsWith(catalogKey(workspace, ""))) catalogs.delete(k);
}

function loadCatalog(workspace: string, provider: string): Promise<ModelCatalog> {
  const key = catalogKey(workspace, provider);
  let p = catalogs.get(key);
  if (!p) {
    p = config.listModels({ workspace, providers: provider ? [provider] : [] }).then(
      (r) => catalogFrom(provider ? { ...r, defaultProvider: provider } : r),
      () => {
        catalogs.delete(key); // asked again next time
        return emptyCatalog;
      },
    );
    catalogs.set(key, p);
  }
  return p;
}

/**
 * The models the workspace's providers list, or with provider only that
 * one's (whether or not it's the one in use); empty until (or unless)
 * they answer.
 */
export function useModelCatalog(workspace: string, provider = ""): ModelCatalog {
  const [catalog, setCatalog] = useState<ModelCatalog>(emptyCatalog);
  useEffect(() => {
    let live = true;
    setCatalog(emptyCatalog);
    void loadCatalog(workspace, provider).then((c) => live && setCatalog(c));
    return () => {
      live = false;
    };
  }, [workspace, provider]);
  return catalog;
}

/**
 * A model field: what's typed is kept as typed (no capitals or corrections
 * from the system), offered the models the providers list, read as the
 * service reads it when the field is left (lowercased for gemini and
 * anthropic, which onCommit receives), and checked: an error under it
 * blocks saving (callers check checkModelRef too), a warning doesn't.
 */
export function ModelInput({
  id,
  value,
  onChange,
  onCommit,
  catalog,
  provider,
  extras,
  placeholder,
  hint,
  disabled,
  onKeyDown,
}: {
  id?: string;
  value: string;
  onChange: (v: string) => void;
  /** Called when the field is left, with the value as it will be saved. */
  onCommit?: (v: string) => void;
  catalog: ModelCatalog;
  /**
   * The provider a bare name belongs to, when not the catalog's default;
   * the suggestions are then its models only, by their bare names.
   */
  provider?: string;
  /** More suggestions: pinned models, models with settings. */
  extras?: string[];
  placeholder?: string;
  hint?: string;
  disabled?: boolean;
  onKeyDown?: (e: KeyboardEvent<HTMLInputElement>) => void;
}) {
  const own = useId();
  const inputId = id ?? own;
  const check = checkModelRef(value, catalog, provider || undefined);
  const message = check.error ? t(check.error) : check.warning ? t(check.warning.key, check.warning.params) : hint;
  return (
    <>
      <input
        id={inputId}
        className="input mono"
        list={`${inputId}-models`}
        value={value}
        placeholder={placeholder}
        disabled={disabled}
        autoCapitalize="off"
        autoCorrect="off"
        autoComplete="off"
        spellCheck={false}
        aria-invalid={!!check.error}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={onKeyDown}
        onBlur={() => {
          if (check.value !== value) onChange(check.value);
          onCommit?.(check.value);
        }}
      />
      <datalist id={`${inputId}-models`}>
        {modelOptions(catalog, [value, ...(extras ?? [])], provider).map((m) => (
          <option key={m} value={m} />
        ))}
      </datalist>
      {message && <span className={`supporting t-body-sm ${check.error ? "error-text" : check.warning ? "warn-text" : "muted"}`}>{message}</span>}
    </>
  );
}
