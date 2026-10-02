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
import { checkModelRef } from "./models";
import { ModelInput, refreshModelCatalog, useModelCatalog } from "./ModelInput";
import { Button, Chip, Icon, Segmented, useSnackbar } from "./ui/controls";

/** Providers llm.provider can name; the first three take API keys. */
const providers = ["gemini", "anthropic", "openai", "ollama"];
const providerNames: Record<string, string> = { gemini: "Gemini", anthropic: "Anthropic", openai: "OpenAI", ollama: "Ollama" };
/** The models a provider's sign-in notes speak of. */
const modelNames: Record<string, string> = { gemini: "Gemini", anthropic: "Claude" };

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

/**
 * The provider the scope uses: its own, else, globally, Gemini (the
 * default); a workspace with none follows the global settings ("").
 */
export const effectiveProvider = (provider: string, workspace: string) => provider || (workspace ? "" : "gemini");

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
 * A scope's provider and its sign-in: the global settings (workspace "")
 * or one workspace's own, which override the global ones. The provider,
 * its default model and its key or sign-in are one change, saved together;
 * the chosen provider's form also says where its key comes from, and can
 * secure, remove or point it elsewhere (base URL). Keys go to the OS
 * keychain; the page never sees them.
 */
export function ProviderSettings({ workspace, compact }: { workspace: string; compact?: boolean }) {
  const snack = useSnackbar();
  const [desc, setDesc] = useState<DescribeConfigResponse>();
  const [error, setError] = useState("");
  const [modelError, setModelError] = useState("");
  const [busy, setBusy] = useState(false);
  const [choice, setChoice] = useState<Choice>({ provider: "", model: "", method: "api_key", key: "", projectId: "", location: "", profile: "" });
  const catalog = useModelCatalog(workspace);

  // Reads the scope; fresh also starts the choice over from what's saved.
  const load = useCallback(
    async (fresh?: boolean) => {
      try {
        const d = await config.describeConfig({ workspace });
        setDesc(d);
        if (fresh) setChoice(savedChoice(d, effectiveProvider(d.provider, workspace)));
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
      refreshModelCatalog(workspace); // a new provider or key lists other models
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
  const saved = savedChoice(desc, effectiveProvider(desc.provider, workspace));
  const dirty = !sameChoice(choice, saved);
  const modelCheck = checkModelRef(choice.model, catalog, choice.provider || desc.provider);
  const save = () =>
    dirty &&
    !busy &&
    !modelCheck.error &&
    act(
      () =>
        config.setProvider({
          workspace,
          provider: choice.provider,
          defaultModel: modelCheck.value,
          key: choice.method === "api_key" ? choice.key.trim() : "",
          auth: others.length ? { method: choice.method, projectId: choice.projectId.trim(), location: choice.location.trim(), profile: choice.profile.trim() } : undefined,
        }),
      t("desktop.keys.provider_saved"),
      true,
    );
  const onEnter = (e: KeyboardEvent) => e.key === "Enter" && save();
  const set = (field: keyof Choice) => (e: { target: { value: string } }) => setChoice({ ...choice, [field]: e.target.value });
  const text = (field: "projectId" | "location" | "profile", label: string, placeholder: string) => (
    <label className="field">
      <span className="t-label">{t(label)}</span>
      <input className="input mono" value={choice[field]} placeholder={t(placeholder)} spellCheck={false} disabled={busy} onChange={set(field)} onKeyDown={onEnter} />
    </label>
  );
  return (
    <div className="stack provider-settings" style={{ gap: 12 }}>
      <p className="t-body-sm muted">
        <Icon path={mdiLockOutline} size="sm" /> {t(workspace ? "desktop.keys.intro_workspace" : "desktop.keys.intro", { store: desc.secretStore })}
      </p>
      <section className={`card provider-choice ${compact ? "compact" : ""}`}>
        <div className="pair">
          <label className="field">
            <span className="t-label">{t("desktop.keys.provider")}</span>
            <select className="select" value={choice.provider} disabled={busy} onChange={(e) => setChoice(savedChoice(desc, e.target.value, choice.model))}>
              {workspace && <option value="">{t("desktop.keys.use_global")}</option>}
              {providers.map((p) => (
                <option key={p} value={p}>
                  {providerNames[p]}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <span className="t-label">{t("desktop.keys.default_model")}</span>
            <ModelInput
              value={choice.model}
              onChange={(model) => setChoice((c) => ({ ...c, model }))}
              catalog={catalog}
              provider={choice.provider || desc.provider}
              placeholder={t(workspace ? "desktop.keys.use_global" : "desktop.keys.model_placeholder")}
              disabled={busy}
              onKeyDown={onEnter}
            />
          </label>
        </div>
        {keyed && others.length > 0 && (
          <div className="field">
            <span className="t-label">{t("desktop.keys.sign_in")}</span>
            {compact ? (
              <select className="select" aria-label={t("desktop.keys.sign_in")} value={choice.method} disabled={busy} onChange={set("method")}>
                {["api_key", ...others].map((m) => (
                  <option key={m} value={m}>
                    {t(`desktop.keys.auth.${m}`)}
                  </option>
                ))}
              </select>
            ) : (
              <Segmented
                small
                label={t("desktop.keys.sign_in")}
                value={choice.method}
                options={["api_key", ...others].map((m) => ({ value: m, label: t(`desktop.keys.auth.${m}`) }))}
                onChange={(method) => !busy && setChoice({ ...choice, method })}
              />
            )}
          </div>
        )}
        {keyed && choice.method === "adc" && (
          <div className="pair even">
            {text("projectId", "desktop.keys.project", "desktop.keys.project_placeholder")}
            {text("location", "desktop.keys.location", "desktop.keys.location_placeholder")}
          </div>
        )}
        {keyed && choice.method === "oauth" && <div className="single">{text("profile", "desktop.keys.profile", "desktop.keys.profile_placeholder")}</div>}
        {keyed && choice.method === "api_key" && (
          <label className="field single">
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
        {keyed && choice.method !== "api_key" && <p className="t-body-sm muted">{t(`desktop.keys.${choice.method}_hint`, { provider: modelNames[choice.provider] ?? choice.provider })}</p>}
        {keyed && choice.method === "api_key" && <KeyStatus workspace={workspace} p={keyed} busy={busy} act={act} />}
        {keyed && !compact && keyed.name !== "gemini" && <BaseURL workspace={workspace} p={keyed} busy={busy} act={act} />}
        <div className="row" style={{ justifyContent: "flex-end", gap: 8 }}>
          <Button small disabled={!dirty || busy} onClick={() => setChoice(saved)}>
            {t("desktop.file.revert")}
          </Button>
          <Button small variant="filled" disabled={!dirty || busy || !!modelCheck.error} onClick={save}>
            {t("desktop.keys.save")}
          </Button>
        </div>
      </section>
      {modelError && (
        <p className="card warn row t-body-sm">
          <Icon path={mdiAlertCircleOutline} size="sm" />
          <span>{t("desktop.keys.model_error", { reason: modelError })}</span>
        </p>
      )}
      {error && <p className="error-text">{error}</p>}
    </div>
  );
}

type Act = (f: () => Promise<{ change?: ConfigChange }>, done?: string) => Promise<boolean>;

// Where the chosen provider's key comes from, and what can be done about
// it: secure one written in the file, or remove it.
function KeyStatus({ workspace, p, busy, act }: { workspace: string; p: ProviderConfig; busy: boolean; act: Act }) {
  const source = p.keyMissing ? "missing" : sourceKeys[p.keySource];
  const good = hasKey(p);
  const insecure = p.keySource === KeySource.PLAIN || p.keySource === KeySource.OBFUSCATED;
  const name = providerNames[p.name] ?? p.name;
  return (
    <div className="key-status">
      <span className="row wrap" style={{ gap: 8 }}>
        <Icon path={mdiKeyOutline} size="sm" />
        <Chip className="static" icon={good ? (insecure ? mdiAlertCircleOutline : mdiCheckCircleOutline) : undefined} tone={p.keyMissing || insecure ? "danger" : undefined} selected={good && !insecure}>
          {t(`desktop.keys.source.${source}`)}
        </Chip>
        <span className="spacer" />
        {insecure && (
          <Button small variant="tonal" disabled={busy} onClick={() => act(() => config.secureApiKey({ workspace, provider: p.name }), t("desktop.keys.secured", { provider: name }))}>
            {t("desktop.keys.secure")}
          </Button>
        )}
        {inFile(p) && (
          <Button small danger disabled={busy} onClick={() => act(() => config.removeApiKey({ workspace, provider: p.name }), t("desktop.keys.removed", { provider: name }))}>
            {t("desktop.keys.remove")}
          </Button>
        )}
      </span>
      <small className="muted">{t(`desktop.keys.source.${source}.detail`, { provider: name, project: p.projectId || t("desktop.keys.project_from_env"), profile: p.profile || t("desktop.keys.profile_placeholder") })}</small>
    </div>
  );
}

// The provider's address, for a proxy or a compatible server.
function BaseURL({ workspace, p, busy, act }: { workspace: string; p: ProviderConfig; busy: boolean; act: Act }) {
  const [baseURL, setBaseURL] = useState(p.baseUrl);
  useEffect(() => setBaseURL(p.baseUrl), [p.baseUrl]);
  return (
    <label className="field single">
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
  );
}
