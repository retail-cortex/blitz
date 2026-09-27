import { useCallback, useEffect, useState } from "react";
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

/**
 * A scope's providers and API keys: the global settings (workspace "") or
 * one workspace's own, which override the global ones. Keys go to the OS
 * keychain; the page never sees them.
 */
export function ProviderSettings({ workspace, compact }: { workspace: string; compact?: boolean }) {
  const snack = useSnackbar();
  const [desc, setDesc] = useState<DescribeConfigResponse>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [model, setModel] = useState("");

  const load = useCallback(async () => {
    try {
      const d = await config.describeConfig({ workspace });
      setDesc(d);
      setModel(d.defaultModel);
      setError("");
    } catch (e) {
      setError(message(e));
    }
  }, [workspace]);
  useEffect(() => {
    load();
  }, [load]);

  // Runs one change, reloads, and tells the workspaces.
  const act = async (f: () => Promise<{ change?: ConfigChange }>, done?: string) => {
    setBusy(true);
    setError("");
    try {
      const { change } = await f();
      if (change?.modelError) snack(t("desktop.keys.model_error", { reason: change.modelError }), { error: true });
      else if (done) snack(done);
      configChanged({ dir: workspace });
      await load();
      return true;
    } catch (e) {
      setError(message(e));
      return false;
    } finally {
      setBusy(false);
    }
  };
  const setValue = (key: string, value: string) => act(() => config.setConfigValue({ workspace, key, value }));

  if (!desc) return <p className="muted">{error || t("desktop.checking")}</p>;
  return (
    <div className="stack provider-settings" style={{ gap: 12 }}>
      <p className="t-body-sm muted">
        <Icon path={mdiLockOutline} size="sm" /> {t(workspace ? "desktop.keys.intro_workspace" : "desktop.keys.intro", { store: desc.secretStore })}
      </p>
      <div className={compact ? "stack" : "row wrap"} style={{ gap: 12 }}>
        <label className="field">
          <span className="t-label">{t("desktop.keys.provider")}</span>
          <select className="select" value={desc.provider} disabled={busy} onChange={(e) => setValue("llm.provider", e.target.value)}>
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
            value={model}
            placeholder={t(workspace ? "desktop.keys.use_global" : "desktop.keys.model_placeholder")}
            disabled={busy}
            onChange={(e) => setModel(e.target.value)}
            onBlur={() => model !== desc.defaultModel && setValue("blitz.default_model", model.trim())}
            onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()}
          />
        </label>
      </div>
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
  const source = p.keyMissing ? "missing" : sourceKeys[p.keySource];
  const good = !p.keyMissing && p.keySource !== KeySource.NONE && p.keySource !== KeySource.UNSPECIFIED;
  const insecure = p.keySource === KeySource.PLAIN || p.keySource === KeySource.OBFUSCATED;
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
        <small className="muted">{t(`desktop.keys.source.${source}.detail`, { provider: name })}</small>
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
