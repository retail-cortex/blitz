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

import { OrgUsageCard } from "./OrgUsage";
import { useCallback, useEffect, useState, type KeyboardEvent } from "react";
import { mdiAlertCircleOutline, mdiCheckCircleOutline, mdiLoading, mdiMinusCircleOutline, mdiLockOutline, mdiOpenInNew } from "@mdi/js";
import { config } from "./api";
import { openURL } from "./desktop";
import { message } from "./errors";
import { configChanged, configChangedEvent } from "./events";
import { KeySource, type ConfigChange, type DescribeConfigResponse, type EnterpriseState, type ListedModel, type ListModelsResponse, type ProviderConfig, type SignInStatus } from "./gen/blitz/v1/config_pb";
import { modelDetail, providerName } from "./ModelMenu";
import { language, t, tn } from "./i18n";
import { checkModelRef } from "./models";
import { ModelInput, refreshModelCatalog, useModelCatalog } from "./ModelInput";
import { Button, Chip, Icon, MultiSelect, Segmented, useSnackbar } from "@blitz/webui";
import { keyPages, listedProviders, noProviderReady, providerState, type OllamaProbe, type ProviderFacts, type ProviderState } from "./providerStatus";
import { useAdvanced } from "./state";
import { showsSignIn } from "./simpleMode";
import { SignInPanel } from "./SignInPanel";
import { signInProvider } from "./signin";
import { apiFormProblems, fromApiForm, sameApiForm, toApiForm, type ApiForm } from "./apiForm";
import { ApiProviderForm } from "./ApiProviderForm";

/** The providers that take API keys. */
const keyed3 = ["gemini", "anthropic", "openai"];
/** The providers that list their models, activated rather than used (spec_061 MOD-40, MOD-42). */
const menuProviders = ["gemini", "anthropic", "openai", "ollama"];
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
const effectiveProvider = (provider: string, workspace: string) => provider || (workspace ? "" : "gemini");

/** The model saved for provider: its own, or, for the one in use, a scope-wide one over it. */
const savedModel = (d: DescribeConfigResponse, provider: string, inUse: boolean) => (inUse && d.defaultModel) || d.models[provider] || "";

/** The choice as saved in the scope, for provider (the one in use unless model is given). */
function savedChoice(d: DescribeConfigResponse, provider: string, model = savedModel(d, provider, true)): Choice {
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
function useProviderFacts(workspace: string, desc: DescribeConfigResponse | undefined): [ProviderFacts | undefined, () => void] {
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
const keyCheckDelay = 500;

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
  // The chosen provider's own models: the field never offers another's.
  const catalog = useModelCatalog(workspace, choice.provider);
  const [facts, refreshFacts] = useProviderFacts(workspace, desc);
  const keyCheck = useKeyCheck(workspace, choice.provider, choice.method, choice.key);
  // Connected to an organization, its models are the models (spec_053
  // EN-20): the page offers its models, and the person's own providers
  // wait, kept, until they disconnect.
  const [org, setOrg] = useState<EnterpriseState | null>(null);
  useEffect(() => {
    config.getEnterprise({ check: false }).then(
      (r) => setOrg(r.state ?? null),
      () => setOrg(null),
    );
  }, [workspace]);

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
  if (org?.url) return <OrganizationModels workspace={workspace} org={org} desc={desc} onSaved={() => void load()} />;
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
  // A provider that lists its models is saved alone and made active; the
  // default model is the top's (spec_061 MOD-40, MOD-41). The others (a
  // gateway, a cloud's deployment) are saved with their model, and used.
  const listsModels = menuProviders.includes(choice.provider);
  const save = () =>
    dirty &&
    !busy &&
    !blocked &&
    act(
      () =>
        config.setProvider({
          workspace,
          provider: choice.provider,
          keepDefault: listsModels,
          defaultModel: listsModels ? "" : modelCheck.value,
          key: choice.method === "api_key" ? choice.key.trim() : "",
          auth: others.length ? { method: choice.method, projectId: choice.projectId.trim(), location: choice.location.trim(), profile: choice.profile.trim() } : undefined,
          api: isAPI ? fromApiForm(choice.api) : undefined,
        }),
      t("desktop.keys.provider_saved"),
      true,
    );
  // A row picked: its form, as saved, with its own model.
  const pick = (p: string, method?: string) => !busy && setChoice({ ...savedChoice(desc, p, savedModel(desc, p, p === saved.provider)), ...(method ? { method } : {}) });
  // Use: a provider ready as it is becomes the scope's, on the model saved
  // for it (each provider keeps its own), else its default.
  const use = (p: string) =>
    act(() => config.setProvider({ workspace, provider: p, defaultModel: desc.models[p] ?? "" }), p ? t("desktop.keys.now_using", { provider: nameOf(p) }) : t("desktop.keys.now_global"), true);
  // Activate makes a provider available; Deactivate takes it back, unless
  // the default model is its (spec_061 MOD-40). With no list, every one
  // set up is active, so the first change lists them.
  const ready = (p: string) => !!facts && providerState(p, facts).ready;
  const isActive = (p: string) => (desc.activeProviders.length ? desc.activeProviders.includes(p) : ready(p));
  const defaultProvider = effectiveProvider(desc.provider, workspace);
  const setActive = (p: string, on: boolean) => {
    if (!on && p === defaultProvider) {
      snack(t("desktop.keys.default_is_theirs", { provider: nameOf(p) }), { error: true });
      return;
    }
    const base = desc.activeProviders.length ? desc.activeProviders : listedProviders.filter((x) => ready(x) && menuProviders.includes(x));
    const next = on ? [...new Set([...base, p])] : base.filter((x) => x !== p);
    void act(() => config.setConfigValue({ workspace, key: "llm.providers", value: next.join(",") }), t(on ? "desktop.keys.activated" : "desktop.keys.deactivated", { provider: nameOf(p) }));
  };
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
      <DefaultModel workspace={workspace} desc={desc} busy={busy} act={act} />
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
            inUse={menuProviders.includes(p) ? isActive(p) : saved.provider === p}
            active={menuProviders.includes(p)}
            busy={busy}
            onSelect={() => pick(p)}
            onUse={() => (menuProviders.includes(p) ? setActive(p, true) : use(p))}
            onDeactivate={menuProviders.includes(p) ? () => setActive(p, false) : undefined}
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
        {listsModels ? (
          <MenuModels key={choice.provider} workspace={workspace} provider={choice.provider} desc={desc} busy={busy} act={act} />
        ) : (
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
        )}
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
        {choice.provider === "ollama" && <p className="t-body-sm muted">{t("desktop.keys.ollama_limits")}</p>}
        {isAPI && (
          <ApiProviderForm
            form={choice.api}
            onChange={(api) => setChoice((c) => ({ ...c, api }))}
            problems={apiTouched ? apiProblems : []}
            busy={busy}
            onEnter={onEnter}
            advanced={advanced}
            listed={saved.api.baseUrl && saved.api.listsModels ? () => gatewayModels(workspace) : undefined}
          />
        )}
        {isAPI && saved.provider === "api" && saved.api.baseUrl && <MenuModels workspace={workspace} provider="api" desc={desc} busy={busy} act={act} />}
        {keyed && choice.method === "api_key" && <KeyStatus workspace={workspace} p={keyed} busy={busy} act={act} />}
        {keyed && !compact && advanced && keyed.name !== "gemini" && <BaseURL workspace={workspace} p={keyed} busy={busy} act={act} />}
        <div className="row" style={{ justifyContent: "flex-end", gap: 8 }}>
          <Button small disabled={!dirty || busy} onClick={() => setChoice(saved)}>
            {t("desktop.file.revert")}
          </Button>
          <Button small variant="filled" disabled={!dirty || busy || blocked} onClick={save}>
            {t(choice.provider !== saved.provider && !listsModels ? "desktop.keys.save_use" : "desktop.keys.save")}
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

type Act = (f: () => Promise<{ change?: ConfigChange }>, done?: string, fresh?: boolean) => Promise<boolean>;

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
  active,
  busy,
  onSelect,
  onUse,
  onDeactivate,
  onSetUp,
}: {
  name: string;
  state: ProviderState;
  selected: boolean;
  /** In use, or for a provider that lists its models (active), active. */
  inUse: boolean;
  /** Its buttons say Activate and Active, not Use and In use (spec_061 MOD-40). */
  active?: boolean;
  busy: boolean;
  onSelect: () => void;
  onUse: () => void;
  onDeactivate?: () => void;
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
          <>
            <Chip className="static" selected icon={mdiCheckCircleOutline}>
              {t(active ? "desktop.keys.active" : "desktop.keys.in_use")}
            </Chip>
            {onDeactivate && (
              <Button small variant="text" disabled={busy} onClick={onDeactivate}>
                {t("desktop.keys.deactivate")}
              </Button>
            )}
          </>
        ) : state.ready ? (
          <Button small variant="tonal" disabled={busy} onClick={onUse}>
            {t(active ? "desktop.keys.activate" : "desktop.keys.use")}
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

/**
 * The organization's models, while Blitz is connected to its service: the
 * organization as the one provider, and its models to choose the default
 * from ("" for the organization's own default).
 */
function OrganizationModels({ workspace, org, desc, onSaved }: { workspace: string; org: EnterpriseState; desc: DescribeConfigResponse; onSaved: () => void }) {
  const snack = useSnackbar();
  const [models, setModels] = useState<ListedModel[] | null>(null);
  const [listError, setListError] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    config.listModels({ workspace, providers: ["enterprise"] }).then(
      (r) => {
        const p = r.providers.find((x) => x.provider === "enterprise");
        setModels(p?.models ?? []);
        setListError(p?.error ?? "");
      },
      (e) => setListError(message(e)),
    );
  }, [workspace, org.signedIn]);
  const name = org.name || org.url;
  const current = desc.defaultModel.startsWith("enterprise/") ? desc.defaultModel.slice("enterprise/".length) : "";
  const choose = async (id: string) => {
    setBusy(true);
    setError("");
    try {
      const { change } = await config.setConfigValue({ workspace, key: "blitz.default_model", value: id ? `enterprise/${id}` : "" });
      if (change?.modelError) setError(change.modelError);
      else snack(t("desktop.keys.provider_saved"));
      configChanged({ dir: workspace });
      onSaved();
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="stack provider-settings" style={{ gap: 12 }}>
      <p className="t-body-sm muted">
        <Icon path={mdiLockOutline} size="sm" /> {t("desktop.org.models_intro", { org: name })}
      </p>
      <div className="list provider-list" role="radiogroup" aria-label={t("desktop.keys.provider")}>
        <ProviderRow
          name={name}
          state={org.signedIn ? { key: "org_signed_in", params: { email: org.email }, tone: "ok", ready: true } : { key: "org_signed_out", tone: "warn", ready: false }}
          selected
          inUse
          busy={busy}
          onSelect={() => {}}
          onUse={() => {}}
        />
      </div>
      <section className="card provider-choice">
        <span className="t-title-sm">{name}</span>
        <label className="field single">
          <span className="t-label">{t("desktop.keys.default_model")}</span>
          <select className="select" value={current} disabled={busy || !org.signedIn || models === null} onChange={(e) => void choose(e.target.value)}>
            <option value="">{t("desktop.org.model_default")}</option>
            {current && !models?.some((m) => m.id === current) && <option value={current}>{current}</option>}
            {models?.map((m) => (
              <option key={m.id} value={m.id}>
                {m.isDefault ? t("desktop.org.model_is_default", { name: m.displayName || m.id }) : m.displayName || m.id}
              </option>
            ))}
          </select>
        </label>
        {(() => {
          const m = models?.find((x) => (current ? x.id === current : x.isDefault));
          const facts = m ? modelFacts(m) : "";
          return facts ? <p className="t-body-sm muted org-model-facts">{facts}</p> : null;
        })()}
        {!org.signedIn ? (
          <p className="t-body-sm muted">{t("desktop.org.models_sign_in")}</p>
        ) : listError ? (
          <p className="error-text t-body-sm">{listError}</p>
        ) : models?.length === 0 ? (
          <p className="t-body-sm muted">{t("desktop.org.no_models")}</p>
        ) : null}
        {error && <p className="error-text t-body-sm">{error}</p>}
      </section>
      {org.signedIn && <OrgUsageCard />}
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

/**
 * What the organization says one of its models can do, and its price
 * (spec_053 EN-21): "Uses tools · Reads images · 200K context · …".
 */
function modelFacts(m: ListedModel): string {
  const c = m.capabilities;
  const parts: string[] = [];
  if (c?.tools) parts.push(t("desktop.org.can.tools"));
  if (c?.images) parts.push(t("desktop.org.can.images"));
  if (c?.thinking) parts.push(t("desktop.org.can.thinking"));
  if (c?.context) parts.push(t("desktop.org.can.context", { k: Math.round(c.context / 1000).toLocaleString(language()) }));
  if (m.hasPrice) parts.push(t("desktop.org.model_price", { input: m.inputPerMtok.toFixed(2), output: m.outputPerMtok.toFixed(2) }));
  return parts.join(" · ");
}

/**
 * The default model (spec_061 MOD-41): one choice of every active
 * provider's models, by provider, which sets the provider and the model
 * together. A model chosen elsewhere that isn't listed stays shown.
 */
function DefaultModel({ workspace, desc, busy, act }: { workspace: string; desc: DescribeConfigResponse; busy: boolean; act: Act }) {
  const [list, setList] = useState<ListModelsResponse | null>(null);
  useEffect(() => {
    config.listModels({ workspace, chatOnly: true, menu: true }).then(setList, () => setList(null));
  }, [workspace, desc]);
  const provider = effectiveProvider(desc.provider, workspace);
  // The model as provider/name; "" when the provider's own default is used.
  const name = desc.defaultModel || desc.models[provider] || "";
  const current = name.includes("/") || !name ? name : `${provider}/${name}`;
  const refs = list?.providers.flatMap((p) => p.models.map((m) => `${p.provider}/${m.id}`)) ?? [];
  return (
    <label className="field single default-model">
      <span className="t-label">{t("desktop.keys.default_model")}</span>
      <select
        className="select"
        value={current}
        disabled={busy || !list}
        onChange={(e) => {
          const [p, ...rest] = e.target.value.split("/");
          void act(() => config.setProvider({ workspace, provider: p, defaultModel: rest.join("/") }), t("desktop.keys.default_set", { model: e.target.value }), true);
        }}
      >
        {!refs.includes(current) && (
          <option value={current}>{current || (provider ? `${providerName(provider)}: ${t("desktop.keys.model_placeholder")}` : t("desktop.keys.use_global"))}</option>
        )}
        {list?.providers.map((p) => (
          <optgroup key={p.provider} label={providerName(p.provider)}>
            {p.models.map((m) => (
              <option key={m.id} value={`${p.provider}/${m.id}`}>
                {m.displayName || m.id}
                {modelDetail(m) ? ` · ${modelDetail(m)}` : ""}
              </option>
            ))}
          </optgroup>
        ))}
      </select>
      <small className="muted">{t("desktop.keys.default_model_hint")}</small>
    </label>
  );
}

/** The models the gateway lists with the saved settings (MDL-114); its error when it can't. */
async function gatewayModels(workspace: string): Promise<string[]> {
  const pm = (await config.listModels({ workspace, providers: ["api"] })).providers.find((p) => p.provider === "api");
  if (pm?.error) throw new Error(pm.error);
  return pm?.models.map((m) => m.id) ?? [];
}

/**
 * A provider's models in the menus (spec_061 MOD-42): several of all its
 * chat models; none chosen, its latest.
 */
function MenuModels({ workspace, provider, desc, busy, act }: { workspace: string; provider: string; desc: DescribeConfigResponse; busy: boolean; act: Act }) {
  const [models, setModels] = useState<ListedModel[]>([]);
  useEffect(() => {
    config.listModels({ workspace, providers: [provider], chatOnly: true }).then(
      (r) => setModels(r.providers.find((p) => p.provider === provider)?.models ?? []),
      () => setModels([]),
    );
  }, [workspace, provider]);
  const chosen = desc.menuModels.find((m) => m.provider === provider)?.models ?? [];
  return (
    <div className="field single">
      <span className="t-label">{t("desktop.keys.menu_models")}</span>
      <MultiSelect
        label={t("desktop.keys.menu_models")}
        options={models.map((m) => ({ value: m.id, detail: modelDetail(m) || undefined }))}
        chosen={chosen}
        none={t("desktop.keys.menu_models_latest")}
        onChange={(v) => !busy && void act(() => config.setConfigValue({ workspace, key: `llm.${provider}.models`, value: v.join(",") }))}
      />
      <small className="muted">{t("desktop.keys.menu_models_hint")}</small>
    </div>
  );
}
