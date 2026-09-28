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

import { useCallback, useEffect, useState, type KeyboardEvent } from "react";
import { mdiAlertCircleOutline, mdiCheckCircleOutline, mdiKeyOutline, mdiLockOutline } from "@mdi/js";
import { config } from "./api";
import { message } from "./errors";
import { configChanged } from "./events";
import { KeySource, type ConfigChange, type DescribeConfigResponse, type ProviderConfig } from "./gen/blitz/v1/config_pb";
import { t } from "./i18n";
import { Button, Chip, Icon, useSnackbar } from "./ui/controls";

/** Providers llm.provider can name; the first three take API keys. */
const providers = ["gemini", "anthropic", "openai", "ollama"];
const providerNames: Record<string, string> = { gemini: "Gemini", anthropic: "Anthropic", openai: "OpenAI", ollama: "Ollama" };

const sourceKeys: Record<KeySource, string> = {
  [KeySource.UNSPECIFIED]: "none",
  [KeySource.NONE]: "none",
  [KeySource.KEYCHAIN]: "keychain",
  [KeySource.PLAIN]: "plain",
  [KeySource.OBFUSCATED]: "obfuscated",
  [KeySource.ENVIRONMENT]: "environment",
  [KeySource.INHERITED]: "inherited",
};

/** The key is written in this scope's file (so it can be removed here). */
const inFile = (p: ProviderConfig) => p.keySource === KeySource.KEYCHAIN || p.keySource === KeySource.PLAIN || p.keySource === KeySource.OBFUSCATED;

/** Where a provider's key comes from, when it has one. */
const hasKey = (p?: ProviderConfig) => !!p && !p.keyMissing && p.keySource !== KeySource.NONE && p.keySource !== KeySource.UNSPECIFIED;

/** The sign-ins each provider takes besides an API key. */
const authMethods: Record<string, string[]> = { gemini: ["adc"], anthropic: ["oauth", "adc"] };

/**
 * What the form saves as one change: the provider, the default model, and
 * how the provider signs in: an API key (a new one, or "" to keep the
 * current one), Google Cloud's ADC (a project and location) or an ant
 * OAuth profile.
 */
interface Choice {
  provider: string;
  model: string;
  method: string;
  key: string;
  projectId: string;
  location: string;
  profile: string;
}

/** The choice as saved in the scope, for provider. */
function savedChoice(d: DescribeConfigResponse, provider: string, model = d.defaultModel): Choice {
  const p = d.providers.find((x) => x.name === provider);
  return { provider, model, method: p?.auth || "api_key", key: "", projectId: p?.projectId ?? "", location: p?.location ?? "", profile: p?.profile ?? "" };
}

const sameChoice = (a: Choice, b: Choice) =>
  a.provider === b.provider &&
  a.model.trim() === b.model.trim() &&
  a.method === b.method &&
  a.key.trim() === "" &&
  a.projectId.trim() === b.projectId &&
  a.location.trim() === b.location &&
  a.profile.trim() === b.profile;

/**
 * A scope's providers and API keys: the global settings (workspace "") or
 * one workspace's own, which override the global ones. The provider, its
 * default model and its key are one change, saved together; below, each
 * provider's key. Keys go to the OS keychain; the page never sees them.
 */
export function ProviderSettings({ workspace, compact }: { workspace: string; compact?: boolean }) {
  const snack = useSnackbar();
  const [desc, setDesc] = useState<DescribeConfigResponse>();
  const [error, setError] = useState("");
  const [modelError, setModelError] = useState("");
  const [busy, setBusy] = useState(false);
  const [choice, setChoice] = useState<Choice>({ provider: "", model: "", method: "api_key", key: "", projectId: "", location: "", profile: "" });

  // Reads the scope; fresh also starts the choice over from what's saved.
  const load = useCallback(
    async (fresh?: boolean) => {
      try {
        const d = await config.describeConfig({ workspace });
        setDesc(d);
        if (fresh) setChoice(savedChoice(d, d.provider));
        setError("");
      } catch (e) {
        setError(message(e));
      }
    },
    [workspace],
  );
  useEffect(() => {
    load(true);
  }, [load]);

  // Runs one change, reloads, and tells the workspaces. A model that still
  // can't be built is said in the form, where it stays until the next change.
  const act = async (f: () => Promise<{ change?: ConfigChange }>, done?: string, fresh?: boolean) => {
    setBusy(true);
    setError("");
    try {
      const { change } = await f();
      setModelError(change?.modelError ?? "");
      if (done && !change?.modelError) snack(done);
      configChanged({ dir: workspace });
      await load(fresh);
      return true;
    } catch (e) {
      setError(message(e));
      return false;
    } finally {
      setBusy(false);
    }
  };

  if (!desc) return <p className="muted">{error || t("desktop.checking")}</p>;
  const keyed = desc.providers.find((p) => p.name === choice.provider);
  const others = authMethods[choice.provider] ?? [];
  const dirty = !sameChoice(choice, savedChoice(desc, desc.provider));
  const save = () =>
    dirty &&
    !busy &&
    act(
      () =>
        config.setProvider({
          workspace,
          provider: choice.provider,
          defaultModel: choice.model.trim(),
          key: choice.method === "api_key" ? choice.key.trim() : "",
          auth: others.length ? { method: choice.method, projectId: choice.projectId.trim(), location: choice.location.trim(), profile: choice.profile.trim() } : undefined,
        }),
      t("desktop.keys.provider_saved"),
      true,
    );
  const onEnter = (e: KeyboardEvent) => e.key === "Enter" && save();
  const set = (field: keyof Choice) => (e: { target: { value: string } }) => setChoice({ ...choice, [field]: e.target.value });
  const text = (field: "projectId" | "location" | "profile", label: string, placeholder: string) => (
    <label className="field" style={{ flex: 1 }}>
      <span className="t-label">{t(label)}</span>
      <input className="input mono" value={choice[field]} placeholder={t(placeholder)} spellCheck={false} disabled={busy} onChange={set(field)} onKeyDown={onEnter} />
    </label>
  );
  return (
    <div className="stack provider-settings" style={{ gap: 12 }}>
      <p className="t-body-sm muted">
        <Icon path={mdiLockOutline} size="sm" /> {t(workspace ? "desktop.keys.intro_workspace" : "desktop.keys.intro", { store: desc.secretStore })}
      </p>
      <div className={compact ? "stack" : "row wrap"} style={{ gap: 12 }}>
        <label className="field">
          <span className="t-label">{t("desktop.keys.provider")}</span>
          <select className="select" value={choice.provider} disabled={busy} onChange={(e) => setChoice(savedChoice(desc, e.target.value, choice.model))}>
            <option value="">{t(workspace ? "desktop.keys.use_global" : "desktop.keys.provider_default")}</option>
            {providers.map((p) => (
              <option key={p} value={p}>
                {providerNames[p]}
              </option>
            ))}
          </select>
        </label>
        <label className="field" style={{ flex: 1 }}>
          <span className="t-label">{t("desktop.keys.default_model")}</span>
          <input
            className="input mono"
            value={choice.model}
            placeholder={t(workspace ? "desktop.keys.use_global" : "desktop.keys.model_placeholder")}
            disabled={busy}
            onChange={set("model")}
            onKeyDown={onEnter}
          />
        </label>
        {keyed && others.length > 0 && (
          <label className="field">
            <span className="t-label">{t("desktop.keys.sign_in")}</span>
            <select className="select" value={choice.method} disabled={busy} onChange={set("method")}>
              <option value="api_key">{t("desktop.keys.auth.api_key")}</option>
              {others.map((m) => (
                <option key={m} value={m}>
                  {t(`desktop.keys.auth.${m}`)}
                </option>
              ))}
            </select>
          </label>
        )}
        {keyed && choice.method === "adc" && (
          <>
            {text("projectId", "desktop.keys.project", "desktop.keys.project_placeholder")}
            {text("location", "desktop.keys.location", "desktop.keys.location_placeholder")}
          </>
        )}
        {keyed && choice.method === "oauth" && text("profile", "desktop.keys.profile", "desktop.keys.profile_placeholder")}
        {keyed && choice.method === "api_key" && (
          <label className="field" style={{ flex: 1 }}>
            <span className="t-label">{t("desktop.keys.key_label", { provider: providerNames[keyed.name] ?? keyed.name })}</span>
            <input
              className="input mono"
              type="password"
              autoComplete="off"
              spellCheck={false}
              value={choice.key}
              placeholder={t(hasKey(keyed) ? "desktop.keys.key_keep" : "desktop.keys.key_placeholder")}
              disabled={busy}
              onChange={set("key")}
              onKeyDown={onEnter}
            />
          </label>
        )}
      </div>
      {keyed && choice.method !== "api_key" && <p className="t-body-sm muted">{t(`desktop.keys.${choice.method}_hint`, { provider: providerNames[choice.provider] ?? choice.provider })}</p>}
      <div className="row" style={{ justifyContent: "flex-end", gap: 8 }}>
        <Button small disabled={!dirty || busy} onClick={() => setChoice(savedChoice(desc, desc.provider))}>
          {t("desktop.file.revert")}
        </Button>
        <Button small variant="filled" disabled={!dirty || busy} onClick={save}>
          {t("desktop.keys.save")}
        </Button>
      </div>
      {modelError && (
        <p className="card warn row t-body-sm">
          <Icon path={mdiAlertCircleOutline} size="sm" />
          <span>{t("desktop.keys.model_error", { reason: modelError })}</span>
        </p>
      )}
      <div className="provider-list">
        {desc.providers.map((p) => (
          <ProviderRow key={p.name} workspace={workspace} p={p} busy={busy} compact={compact} act={act} />
        ))}
      </div>
      {error && <p className="error-text">{error}</p>}
    </div>
  );
}

function ProviderRow({
  workspace,
  p,
  busy,
  compact,
  act,
}: {
  workspace: string;
  p: ProviderConfig;
  busy: boolean;
  compact?: boolean;
  act: (f: () => Promise<{ change?: ConfigChange }>, done?: string) => Promise<boolean>;
}) {
  const [entering, setEntering] = useState(false);
  const [key, setKey] = useState("");
  const [baseURL, setBaseURL] = useState(p.baseUrl);
  useEffect(() => setBaseURL(p.baseUrl), [p.baseUrl]);
  const source = p.auth === "adc" || p.auth === "oauth" ? p.auth : p.keyMissing ? "missing" : sourceKeys[p.keySource];
  const signsIn = p.auth === "adc" || p.auth === "oauth";
  const good = signsIn || hasKey(p);
  const insecure = !signsIn && (p.keySource === KeySource.PLAIN || p.keySource === KeySource.OBFUSCATED);
  const name = providerNames[p.name] ?? p.name;

  const save = async () => {
    const k = key;
    setKey("");
    if (await act(() => config.setApiKey({ workspace, provider: p.name, key: k }), t("desktop.keys.saved", { provider: name }))) setEntering(false);
  };
  return (
    <div className="provider-row">
      <div className="stack" style={{ gap: 2 }}>
        <span className="row wrap" style={{ gap: 8 }}>
          <Icon path={mdiKeyOutline} size="sm" />
          <span className="t-title-sm">{name}</span>
          <Chip className="static" icon={good ? (insecure ? mdiAlertCircleOutline : mdiCheckCircleOutline) : undefined} tone={p.keyMissing || insecure ? "danger" : undefined} selected={good && !insecure}>
            {t(`desktop.keys.source.${source}`)}
          </Chip>
          {!entering && (
            <span className="provider-actions">
              {insecure && (
                <Button small variant="tonal" disabled={busy} onClick={() => act(() => config.secureApiKey({ workspace, provider: p.name }), t("desktop.keys.secured", { provider: name }))}>
                  {t("desktop.keys.secure")}
                </Button>
              )}
              <Button small variant={good ? undefined : "tonal"} disabled={busy} onClick={() => setEntering(true)}>
                {t(good && p.keySource !== KeySource.INHERITED && p.keySource !== KeySource.ENVIRONMENT ? "desktop.keys.replace" : "desktop.keys.set")}
              </Button>
              {inFile(p) && (
                <Button small danger disabled={busy} onClick={() => act(() => config.removeApiKey({ workspace, provider: p.name }), t("desktop.keys.removed", { provider: name }))}>
                  {t("desktop.keys.remove")}
                </Button>
              )}
            </span>
          )}
        </span>
        <small className="muted">
          {t(`desktop.keys.source.${source}.detail`, {
            provider: name,
            project: p.projectId || t("desktop.keys.project_from_env"),
            profile: p.profile || t("desktop.keys.profile_placeholder"),
          })}
        </small>
        {entering && (
          <span className="row" style={{ gap: 8, marginTop: 8 }}>
            <input
              className="input mono"
              type="password"
              autoFocus
              autoComplete="off"
              spellCheck={false}
              aria-label={t("desktop.keys.key_label", { provider: name })}
              placeholder={t("desktop.keys.key_placeholder")}
              value={key}
              onChange={(e) => setKey(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && key.trim()) save();
                if (e.key === "Escape") {
                  e.stopPropagation();
                  setEntering(false);
                  setKey("");
                }
              }}
              style={{ flex: 1 }}
            />
            <Button small variant="filled" disabled={busy || !key.trim()} onClick={save}>
              {t("desktop.keys.save")}
            </Button>
            <Button small onClick={() => (setEntering(false), setKey(""))}>
              {t("desktop.cancel")}
            </Button>
          </span>
        )}
        {!compact && p.name !== "gemini" && (
          <label className="field" style={{ marginTop: 8 }}>
            <span className="t-label">{t("desktop.keys.base_url")}</span>
            <input
              className="input mono"
              value={baseURL}
              placeholder={t(workspace ? "desktop.keys.use_global" : "desktop.keys.base_url_placeholder")}
              disabled={busy}
              onChange={(e) => setBaseURL(e.target.value)}
              onBlur={() => baseURL !== p.baseUrl && act(() => config.setConfigValue({ workspace, key: `llm.${p.name}.base_url`, value: baseURL.trim() }))}
              onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()}
            />
          </label>
        )}
      </div>
    </div>
  );
}

/**
 * A settings file as text, for what the forms don't cover. The service
 * checks it before saving, and warns about unknown settings and keys
 * written as plain text.
 */
export function SettingsFile({ scopes }: { scopes: { dir: string; name: string }[] }) {
  const snack = useSnackbar();
  const [workspace, setWorkspace] = useState("");
  const [path, setPath] = useState("");
  const [text, setText] = useState("");
  const [saved, setSaved] = useState("");
  const [warnings, setWarnings] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let live = true;
    setWarnings([]);
    setError("");
    config.getConfigFile({ workspace }).then(
      (f) => {
        if (!live) return;
        setPath(f.path);
        setText(f.text);
        setSaved(f.text);
      },
      (e) => live && setError(message(e)),
    );
    return () => {
      live = false;
    };
  }, [workspace]);

  const save = async () => {
    setBusy(true);
    setError("");
    try {
      const res = await config.saveConfigFile({ workspace, text });
      setSaved(text);
      setWarnings(res.warnings);
      if (res.change?.modelError) snack(t("desktop.keys.model_error", { reason: res.change.modelError }), { error: true });
      else snack(t("desktop.file.saved"));
      configChanged({ dir: workspace });
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  };
  const dirty = text !== saved;
  return (
    <div className="stack settings-file" style={{ gap: 12 }}>
      <div className="row" style={{ gap: 12 }}>
        <label className="field" style={{ flex: 1 }}>
          <span className="t-label">{t("desktop.file.scope")}</span>
          <select className="select" value={workspace} onChange={(e) => setWorkspace(e.target.value)} disabled={dirty}>
            <option value="">{t("desktop.file.global")}</option>
            {scopes.map((s) => (
              <option key={s.dir} value={s.dir}>
                {s.name}
              </option>
            ))}
          </select>
        </label>
      </div>
      <code className="t-body-sm muted ellipsis">{path}</code>
      {warnings.map((w) => (
        <p key={w} className="card warn row t-body-sm">
          <Icon path={mdiAlertCircleOutline} size="sm" />
          <span>{w}</span>
        </p>
      ))}
      {error && <p className="error-text">{error}</p>}
      <textarea
        className="input mono settings-text"
        aria-label={t("desktop.file.text")}
        spellCheck={false}
        value={text}
        placeholder={t("desktop.file.empty")}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if ((e.metaKey || e.ctrlKey) && e.key === "s") {
            e.preventDefault();
            if (dirty && !busy) save();
          }
        }}
        rows={12}
      />
      <p className="t-body-sm muted">{t("desktop.file.hint")}</p>
      <div className="row" style={{ justifyContent: "flex-end", gap: 8 }}>
        <Button small disabled={!dirty || busy} onClick={() => setText(saved)}>
          {t("desktop.file.revert")}
        </Button>
        <Button small variant="filled" disabled={!dirty || busy} onClick={save}>
          {t("desktop.keys.save")}
        </Button>
      </div>
    </div>
  );
}
