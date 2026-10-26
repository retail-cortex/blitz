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
import { mdiAlertCircleOutline, mdiCheckCircleOutline, mdiLoading, mdiMinusCircleOutline, mdiLockOutline, mdiOpenInNew } from "@mdi/js";
import { config } from "./api";
import { openURL } from "./desktop";
import { message } from "./errors";
import { configChanged, configChangedEvent } from "./events";
import { KeySource, type ConfigChange, type DescribeConfigResponse, type ProviderConfig, type SignInStatus } from "./gen/blitz/v1/config_pb";
import { t, tn } from "./i18n";
import { checkModelRef } from "./models";
import { ModelInput, refreshModelCatalog, useModelCatalog } from "./ModelInput";
import { Button, Chip, Icon, Segmented, useSnackbar } from "./ui/controls";
import { keyPages, listedProviders, noProviderReady, providerState, type OllamaProbe, type ProviderFacts, type ProviderState } from "./providerStatus";
import { useAdvanced } from "./state";
import { showsSignIn } from "./simpleMode";
import { SignInPanel } from "./SignInPanel";
import { signInProvider } from "./signin";
import { apiFormProblems, fromApiForm, sameApiForm, toApiForm, type ApiForm } from "./apiForm";
import { ApiProviderForm } from "./ApiProviderForm";

/** The providers that take API keys. */
const keyed3 = ["gemini", "anthropic", "openai"];
const providerNames: Record<string, string> = { gemini: "Gemini", anthropic: "Anthropic", openai: "OpenAI", ollama: "Ollama" };
/** A provider's name in the list (the api provider's is a description: a custom gateway). */
const nameOf = (p: string) => (p === "api" ? t("desktop.keys.provider.api") : (providerNames[p] ?? p));
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

/**
 * The sign-ins each provider takes besides an API key (config's
 * authMethods): Google Cloud ADC, and an account (OAuth). OpenAI and
 * Ollama take a key only.
 */
const authMethods: Record<string, string[]> = { gemini: ["adc", "oauth"], anthropic: ["oauth", "adc"] };

/** A sign-in method's name: an account's says whose (Google, Anthropic). */
const methodLabel = (provider: string, m: string) => t(m === "oauth" ? `desktop.keys.auth.oauth.${provider}` : `desktop.keys.auth.${m}`);

/** Whether a method signs in on Vertex AI (a project and location). */
const onVertex = (provider: string, m: string) => m === "adc" || (m === "oauth" && provider === "gemini");

/**
 * What the form saves as one change: the provider, the default model, and
 * how the provider signs in: an API key (a new one, or "" to keep the
 * current one), Google Cloud's ADC (a project and location) or an ant
 * OAuth profile; for the api provider, its endpoint, headers and script.
 */
interface Choice {
  provider: string;
  model: string;
  method: string;
  key: string;
  projectId: string;
  location: string;
  profile: string;
  api: ApiForm;
}

/**
 * The provider the scope uses: its own, else, globally, Gemini (the
 * default); a workspace with none follows the global settings ("").
 */
export const effectiveProvider = (provider: string, workspace: string) => provider || (workspace ? "" : "gemini");

/** The choice as saved in the scope, for provider. */
function savedChoice(d: DescribeConfigResponse, provider: string, model = d.defaultModel): Choice {
  const p = d.providers.find((x) => x.name === provider);
  return { provider, model, method: p?.auth || "api_key", key: "", projectId: p?.projectId ?? "", location: p?.location ?? "", profile: p?.profile ?? "", api: toApiForm(d.api) };
}

const sameChoice = (a: Choice, b: Choice) =>
  a.provider === b.provider &&
  a.model.trim() === b.model.trim() &&
  a.method === b.method &&
  a.key.trim() === "" &&
  a.projectId.trim() === b.projectId &&
  a.location.trim() === b.location &&
  a.profile.trim() === b.profile &&
  (a.provider !== "api" || sameApiForm(a.api, b.api));

/**
 * What the providers' list needs besides DescribeConfig: the sign-ins on
 * the machine running the service (GetSignIn) and whether Ollama answers
 * there, each asked once per scope (again when asked to: after a change).
 */
export function useProviderFacts(workspace: string, desc: DescribeConfigResponse | undefined): [ProviderFacts | undefined, () => void] {
  const [signIns, setSignIns] = useState<Record<string, SignInStatus | undefined>>({});
  const [ollama, setOllama] = useState<OllamaProbe>();
  const [n, setN] = useState(0);
  useEffect(() => {
    let live = true;
    config.getSignIn({ workspace }).then(
      (r) => live && setSignIns(Object.fromEntries(r.statuses.map((x) => [x.provider, x]))),
      () => live && setSignIns({ google: undefined, anthropic: undefined }),
    );
    config.listModels({ workspace, providers: ["ollama"] }).then(
      (r) => {
        const o = r.providers.find((x) => x.provider === "ollama");
        if (live) setOllama({ running: !!o && !o.error, models: o?.ids.length ?? 0 });
      },
      () => live && setOllama({ running: false, models: 0 }),
    );
    return () => {
      live = false;
    };
  }, [workspace, n]);
  const refresh = useCallback(() => setN((x) => x + 1), []);
  return [desc && { desc, signIns, ollama }, refresh];
}

/** The outcome of trying a pasted key (CheckApiKey), for the key it was tried with. */
interface KeyCheck {
  key: string;
  busy: boolean;
  works?: boolean;
  error?: string;
  models?: number;
}

/**
 * Tries the key in the form a moment after it stops changing, so the
 * form says whether it works before it's saved.
 */
function useKeyCheck(workspace: string, provider: string, method: string, typed: string): KeyCheck | undefined {
  const [check, setCheck] = useState<KeyCheck>();
  useEffect(() => {
    const key = typed.trim();
    if (!key || method !== "api_key" || !keyed3.includes(provider)) {
      setCheck(undefined);
      return;
    }
    setCheck({ key, busy: true });
    const done = (c: KeyCheck) => setCheck((was) => (was?.key === key ? c : was));
    const id = setTimeout(() => {
      config.checkApiKey({ workspace, provider, key }).then(
        (r) => done({ key, busy: false, works: r.works, error: r.error, models: r.models.length }),
        (e) => done({ key, busy: false, works: false, error: message(e) }),
      );
    }, keyCheckDelay);
    return () => clearTimeout(id);
  }, [workspace, provider, method, typed]);
  return check;
}

/** How long after the last keystroke a pasted or typed key is tried (ms). */
export const keyCheckDelay = 500;

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
  const advanced = useAdvanced();
  const [desc, setDesc] = useState<DescribeConfigResponse>();
  const [error, setError] = useState("");
  const [modelError, setModelError] = useState("");
  const [busy, setBusy] = useState(false);
  const [choice, setChoice] = useState<Choice>({ provider: "", model: "", method: "api_key", key: "", projectId: "", location: "", profile: "", api: toApiForm() });
  const catalog = useModelCatalog(workspace);
  const [facts, refreshFacts] = useProviderFacts(workspace, desc);
  const keyCheck = useKeyCheck(workspace, choice.provider, choice.method, choice.key);

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
      refreshFacts();
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
  const isAPI = choice.provider === "api";
  const apiProblems = isAPI ? apiFormProblems(choice.api) : [];
  // The gateway's problems show once its form is changed, not on opening it.
  const apiTouched = isAPI && !sameApiForm(choice.api, saved.api);
  const blocked = !!modelCheck.error || apiProblems.length > 0;
  const save = () =>
    dirty &&
    !busy &&
    !blocked &&
    act(
      () =>
        config.setProvider({
          workspace,
          provider: choice.provider,
          defaultModel: modelCheck.value,
          key: choice.method === "api_key" ? choice.key.trim() : "",
          auth: others.length ? { method: choice.method, projectId: choice.projectId.trim(), location: choice.location.trim(), profile: choice.profile.trim() } : undefined,
          api: isAPI ? fromApiForm(choice.api) : undefined,
        }),
      t("desktop.keys.provider_saved"),
      true,
    );
  // A row picked: its form, as saved (the model kept only for the provider it's for).
  const pick = (p: string, method?: string) => !busy && setChoice({ ...savedChoice(desc, p, p === saved.provider ? saved.model : ""), ...(method ? { method } : {}) });
  // Use: a provider ready as it is becomes the scope's, on the provider's
  // own default model (the one saved may be another provider's).
  const use = (p: string) =>
    act(() => config.setProvider({ workspace, provider: p, defaultModel: "" }), p ? t("desktop.keys.now_using", { provider: nameOf(p) }) : t("desktop.keys.now_global"), true);
  const onEnter = (e: KeyboardEvent) => e.key === "Enter" && save();
  const set = (field: keyof Choice) => (e: { target: { value: string } }) => setChoice({ ...choice, [field]: e.target.value });
  const text = (field: "projectId" | "location" | "profile", label: string, placeholder: string) => (
    <label className="field">
      <span className="t-label">{t(label)}</span>
      <input className="input mono" value={choice[field]} placeholder={t(placeholder)} spellCheck={false} disabled={busy} onChange={set(field)} onKeyDown={onEnter} />
    </label>
  );
  // Simple mode signs in with an API key: the other ways (Google Cloud, an
  // Anthropic account) and the base URL show with advanced settings, and
  // whenever the provider already signs in another way.
  const signIn = showsSignIn(advanced, choice.method);
  return (
    <div className="stack provider-settings" style={{ gap: 12 }}>
      <p className="t-body-sm muted">
        <Icon path={mdiLockOutline} size="sm" /> {t(workspace ? "desktop.keys.intro_workspace" : "desktop.keys.intro", { store: desc.secretStore })}
      </p>
      <div className="list provider-list" role="radiogroup" aria-label={t("desktop.keys.provider")}>
        {workspace && (
          <ProviderRow
            name={t("desktop.keys.use_global")}
            state={{ key: "global", tone: "none", ready: true }}
            selected={choice.provider === ""}
            inUse={saved.provider === ""}
            busy={busy}
            onSelect={() => pick("")}
            onUse={() => use("")}
          />
        )}
        {listedProviders.map((p) => (
          <ProviderRow
            key={p}
            name={nameOf(p)}
            state={facts ? providerState(p, facts) : { key: "checking", tone: "checking", ready: false }}
            selected={choice.provider === p}
            inUse={saved.provider === p}
            busy={busy}
            onSelect={() => pick(p)}
            onUse={() => use(p)}
            onSetUp={(method) => pick(p, method)}
          />
        ))}
      </div>
      <section className={`card provider-choice ${compact ? "compact" : ""}`}>
        <div className="row" style={{ gap: 8 }}>
          <span className="t-title-sm spacer">{choice.provider ? nameOf(choice.provider) : t("desktop.keys.use_global")}</span>
          {keyPages[choice.provider] && (choice.provider === "ollama" ? !facts?.ollama?.running : choice.method === "api_key" && !hasKey(keyed)) && (
            <Button small icon={mdiOpenInNew} onClick={() => void openURL(keyPages[choice.provider])}>
              {t(choice.provider === "ollama" ? "desktop.keys.get_ollama" : "desktop.keys.get_key")}
            </Button>
          )}
        </div>
        <label className="field single">
          <span className="t-label">{t("desktop.keys.default_model")}</span>
          <ModelInput
            value={choice.model}
            onChange={(model) => setChoice((c) => ({ ...c, model }))}
            catalog={catalog}
            provider={choice.provider || desc.provider}
            placeholder={t(workspace && !choice.provider ? "desktop.keys.use_global" : "desktop.keys.model_placeholder")}
            disabled={busy}
            onKeyDown={onEnter}
          />
        </label>
        {keyed && others.length > 0 && signIn && (
          <div className="field">
            <span className="t-label">{t("desktop.keys.sign_in")}</span>
            {compact ? (
              <select className="select" aria-label={t("desktop.keys.sign_in")} value={choice.method} disabled={busy} onChange={set("method")}>
                {["api_key", ...others].map((m) => (
                  <option key={m} value={m}>
                    {methodLabel(choice.provider, m)}
                  </option>
                ))}
              </select>
            ) : (
              <Segmented
                small
                label={t("desktop.keys.sign_in")}
                value={choice.method}
                options={["api_key", ...others].map((m) => ({ value: m, label: methodLabel(choice.provider, m) }))}
                onChange={(method) => !busy && setChoice({ ...choice, method })}
              />
            )}
          </div>
        )}
        {keyed && onVertex(choice.provider, choice.method) && (
          <div className="pair even">
            {text("projectId", "desktop.keys.project", "desktop.keys.project_placeholder")}
            {text("location", "desktop.keys.location", "desktop.keys.location_placeholder")}
          </div>
        )}
        {keyed && choice.method === "oauth" && choice.provider === "anthropic" && <div className="single">{text("profile", "desktop.keys.profile", "desktop.keys.profile_placeholder")}</div>}
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
            {keyCheck && <KeyCheckLine check={keyCheck} />}
          </label>
        )}
        {keyed && choice.method !== "api_key" && (
          <SignInPanel workspace={workspace} provider={signInProvider(choice.method, choice.provider)} profile={choice.method === "oauth" ? choice.profile : ""} />
        )}
        {!keyed && signInProvider("", saved.provider) && <SignInPanel workspace={workspace} provider={signInProvider("", saved.provider)} />}
        {keyed && choice.method !== "api_key" && (
          <p className="t-body-sm muted">{t(choice.method === "oauth" && choice.provider === "gemini" ? "desktop.keys.google_oauth_hint" : `desktop.keys.${choice.method}_hint`, { provider: modelNames[choice.provider] ?? choice.provider })}</p>
        )}
        {choice.provider === "openai" && <p className="t-body-sm muted">{t("desktop.keys.openai_key_only")}</p>}
        {isAPI && <ApiProviderForm form={choice.api} onChange={(api) => setChoice((c) => ({ ...c, api }))} problems={apiTouched ? apiProblems : []} busy={busy} onEnter={onEnter} />}
        {keyed && choice.method === "api_key" && <KeyStatus workspace={workspace} p={keyed} busy={busy} act={act} />}
        {keyed && !compact && advanced && keyed.name !== "gemini" && <BaseURL workspace={workspace} p={keyed} busy={busy} act={act} />}
        <div className="row" style={{ justifyContent: "flex-end", gap: 8 }}>
          <Button small disabled={!dirty || busy} onClick={() => setChoice(saved)}>
            {t("desktop.file.revert")}
          </Button>
          <Button small variant="filled" disabled={!dirty || busy || blocked} onClick={save}>
            {t(choice.provider !== saved.provider ? "desktop.keys.save_use" : "desktop.keys.save")}
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

// What can be done about the chosen provider's key, when anything can:
// secure one written in the file, or remove one kept here (the list says
// where it comes from).
function KeyStatus({ workspace, p, busy, act }: { workspace: string; p: ProviderConfig; busy: boolean; act: Act }) {
  const insecure = p.keySource === KeySource.PLAIN || p.keySource === KeySource.OBFUSCATED;
  const name = providerNames[p.name] ?? p.name;
  if (!insecure && !inFile(p) && !p.keyMissing) return null;
  const source = p.keyMissing ? "missing" : sourceKeys[p.keySource];
  return (
    <div className="row wrap key-status" style={{ gap: 8 }}>
      <small className={`spacer ${insecure || p.keyMissing ? "warn-text" : "muted"}`}>
        {t(`desktop.keys.source.${source}.detail`, { provider: name, project: p.projectId || t("desktop.keys.project_from_env"), profile: p.profile || t("desktop.keys.profile_placeholder") })}
      </small>
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
    </div>
  );
}

const toneIcons: Record<ProviderState["tone"], string> = { ok: mdiCheckCircleOutline, warn: mdiAlertCircleOutline, danger: mdiAlertCircleOutline, none: mdiMinusCircleOutline, checking: mdiLoading };
const toneClasses: Record<ProviderState["tone"], string> = { ok: "signin-ok", warn: "warn-text", danger: "error-text", none: "muted", checking: "muted" };

/**
 * A provider in the list: where it stands, and what's quickest from
 * there: In use, Use (ready as it is) or Set up (found on this machine).
 * Picking the row opens its form below.
 */
function ProviderRow({
  name,
  state,
  selected,
  inUse,
  busy,
  onSelect,
  onUse,
  onSetUp,
}: {
  name: string;
  state: ProviderState;
  selected: boolean;
  inUse: boolean;
  busy: boolean;
  onSelect: () => void;
  onUse: () => void;
  onSetUp?: (method: string) => void;
}) {
  // The one in use but not set up is what to fix first.
  const tone = inUse && state.tone === "none" ? "warn" : state.tone;
  return (
    <div className={`list-item provider-row ${selected ? "active" : ""}`}>
      <button type="button" role="radio" aria-checked={selected} className="row-open" onClick={onSelect}>
        <Icon path={toneIcons[tone]} spin={tone === "checking"} className={toneClasses[tone]} />
        <span className="lines spacer">
          <span className="ellipsis">{name}</span>
          <small className="ellipsis">{typeof state.params?.count === "number" ? tn(`desktop.keys.status.${state.key}`, state.params.count) : t(`desktop.keys.status.${state.key}`, state.params)}</small>
        </span>
      </button>
      <span className="trailing">
        {inUse ? (
          <Chip className="static" selected icon={mdiCheckCircleOutline}>
            {t("desktop.keys.in_use")}
          </Chip>
        ) : state.ready ? (
          <Button small variant="tonal" disabled={busy} onClick={onUse}>
            {t("desktop.keys.use")}
          </Button>
        ) : state.found && onSetUp ? (
          <Button small disabled={busy} onClick={() => onSetUp(state.found!)}>
            {t("desktop.keys.set_up")}
          </Button>
        ) : null}
      </span>
    </div>
  );
}

/** Whether the key being typed works, once it's been tried. */
function KeyCheckLine({ check }: { check: KeyCheck }) {
  if (check.busy) return <span className="supporting t-body-sm muted">{t("desktop.keys.check.busy")}</span>;
  return check.works ? (
    <span className="supporting t-body-sm ok-text">{tn("desktop.keys.check.works", check.models ?? 0)}</span>
  ) : (
    <span className="supporting t-body-sm error-text">{t("desktop.keys.check.failed", { error: check.error ?? "" })}</span>
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

/**
 * The welcome page's card when no provider can be used yet: Blitz needs
 * one before the first message, so it says so before then, with the way
 * to set one up. It goes once any provider is ready (a change in Settings
 * asks again).
 */
export function ConnectModelCard({ onSetUp }: { onSetUp: () => void }) {
  const [desc, setDesc] = useState<DescribeConfigResponse>();
  const [n, setN] = useState(0);
  useEffect(() => {
    let live = true;
    config.describeConfig({ workspace: "" }).then(
      (d) => live && setDesc(d),
      () => undefined, // no card when the settings can't be read: the service says why elsewhere
    );
    return () => {
      live = false;
    };
  }, [n]);
  const [facts, refresh] = useProviderFacts("", desc);
  useEffect(() => {
    const on = () => {
      setN((x) => x + 1);
      refresh();
    };
    window.addEventListener(configChangedEvent, on);
    return () => window.removeEventListener(configChangedEvent, on);
  }, [refresh]);
  if (!facts || !noProviderReady(facts)) return null;
  return (
    <div className="card warn row connect-model">
      <Icon path={mdiAlertCircleOutline} />
      <span className="stack spacer" style={{ gap: 2 }}>
        <span className="t-title-sm">{t("desktop.welcome.connect_title")}</span>
        <span className="t-body-sm">{t("desktop.welcome.connect_body")}</span>
      </span>
      <Button variant="filled" onClick={onSetUp}>
        {t("desktop.welcome.connect_button")}
      </Button>
    </div>
  );
}
