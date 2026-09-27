import { useCallback, useEffect, useState, type ReactNode } from "react";
import {
  mdiAlertCircleOutline,
  mdiChevronDown,
  mdiChevronRight,
  mdiClose,
  mdiDeleteOutline,
  mdiPlus,
  mdiTuneVariant,
} from "@mdi/js";
import { sessions, workspaces } from "./api";
import { message, reason } from "./errors";
import type { Usage } from "./gen/blitz/v1/turn_pb";
import type { AgentInfo, Approval, GetSettingsResponse, LocaleInfo, ModelSettingsInfo, PermissionRule } from "./gen/blitz/v1/workspace_pb";
import { agencies, efforts, modes } from "./options";
import { useApp } from "./state";
import { Button, Field, Icon, IconButton, Switch, useSnackbar } from "./ui/controls";

function Section({ title, children, open: initial = true }: { title: string; children: ReactNode; open?: boolean }) {
  const [open, setOpen] = useState(initial);
  return (
    <section className="panel-section">
      <button className="panel-section-head" onClick={() => setOpen((o) => !o)} aria-expanded={open}>
        <span className="t-title-sm">{title}</span>
        <Icon path={open ? mdiChevronDown : mdiChevronRight} size="sm" />
      </button>
      {open && <div className="panel-section-body">{children}</div>}
    </section>
  );
}

/**
 * The workspace's run settings, beside the conversation: agent and model,
 * how hard it thinks, generation settings, permissions, and context.
 */
export function RunSettings({ dir, settings, error, onChanged }: { dir: string; settings?: GetSettingsResponse; error: string; onChanged: () => void }) {
  const { update } = useApp();
  const snack = useSnackbar();
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const [models, setModels] = useState<string[]>([]);
  const [modelInfo, setModelInfo] = useState<ModelSettingsInfo>();
  const [locales, setLocales] = useState<LocaleInfo[]>([]);
  const [rules, setRules] = useState<PermissionRule[]>([]);
  const [approvals, setApprovals] = useState<Approval[]>([]);
  const [usage, setUsage] = useState<Usage>();
  const [modelRef, setModelRef] = useState("");

  const model = settings ? (settings.provider ? `${settings.provider}/${settings.model}` : settings.model) : "";
  const load = useCallback(async () => {
    try {
      const [a, all, l, r, ap, u] = await Promise.all([
        workspaces.listAgents({ workspace: dir }),
        workspaces.getModelSettings({ workspace: dir }),
        workspaces.listLocales({ workspace: dir }),
        workspaces.listPermissionRules({ workspace: dir }),
        workspaces.listApprovals({ workspace: dir }),
        sessions.getUsage({ workspace: dir }).catch(() => undefined),
      ]);
      setAgents(a.agents);
      setModels([...new Set([...a.agents.map((x) => x.pinnedModel).filter(Boolean), ...Object.keys(all.all)])]);
      setLocales(l.locales);
      setRules(r.rules);
      setApprovals(ap.approvals);
      setUsage(u?.usage);
    } catch (e) {
      snack(message(e), { error: true });
    }
  }, [dir, snack]);
  useEffect(() => {
    load();
  }, [load]);
  useEffect(() => {
    setModelRef(model);
    if (!settings?.model) return;
    workspaces
      .getModelSettings({ workspace: dir, ref: model })
      .then((r) => setModelInfo(r.model))
      .catch(() => setModelInfo(undefined));
  }, [dir, model, settings?.model]);

  const act = async (f: () => Promise<unknown>, done?: string) => {
    try {
      await f();
      onChanged();
      await load();
      if (done) snack(done);
    } catch (e) {
      snack(reason(e) === "BYPASS_NEEDS_SANDBOX" ? "Bypass needs the OS sandbox, which isn't active." : message(e), { error: true });
    }
  };

  const setModelSetting = (key: string, value: string) =>
    act(async () => {
      const r = await workspaces.updateModelSettings({ workspace: dir, ref: model, changes: [{ key, value }] });
      setModelInfo(r.model);
      if (r.unsupported.length) snack(`${model} doesn't use ${r.unsupported.join(", ")}: it's saved but not sent.`);
    });

  const s = modelInfo?.settings;
  return (
    <aside className="run-settings" aria-label="Run settings">
      <div className="panel-head">
        <Icon path={mdiTuneVariant} />
        <span className="t-title">Run settings</span>
        <span className="spacer" />
        <IconButton icon={mdiClose} label="Close the panel" small onClick={() => update((p) => ({ ...p, run_settings: false }))} />
      </div>
      {error && (
        <div className="card error row small-card">
          <Icon path={mdiAlertCircleOutline} size="sm" /> {error}
        </div>
      )}
      <div className="panel-scroll">
        <Section title="Agent and model">
          <Field label="Agent">
            {(id) => (
              <select id={id} className="select" value={settings?.agent ?? ""} onChange={(e) => act(() => workspaces.setAgent({ workspace: dir, name: e.target.value }), "Agent switched.")}>
                {agents.map((a) => (
                  <option key={a.name} value={a.name}>
                    {a.displayName}
                    {a.pinnedModel ? ` (${a.pinnedModel})` : ""}
                  </option>
                ))}
              </select>
            )}
          </Field>
          <Field label="Model" supporting="A model name, or provider/model (gemini, anthropic, openai, ollama).">
            {(id) => (
              <>
                <input
                  id={id}
                  className="input mono"
                  list={`${id}-models`}
                  value={modelRef}
                  onChange={(e) => setModelRef(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()}
                  onBlur={() => modelRef.trim() && modelRef.trim() !== model && act(() => workspaces.setModel({ workspace: dir, ref: modelRef.trim() }), `Model set to ${modelRef.trim()}.`)}
                />
                <datalist id={`${id}-models`}>
                  {[model, ...models].filter(Boolean).map((m) => (
                    <option key={m} value={m} />
                  ))}
                </datalist>
              </>
            )}
          </Field>
        </Section>

        <Section title="Thinking">
          <Field label="Reasoning effort" supporting="For this session, over each model's own setting.">
            {(id) => (
              <select id={id} className="select" value={settings?.effort ?? ""} onChange={(e) => act(() => workspaces.setSetting({ workspace: dir, key: "effort", value: e.target.value || "auto" }))}>
                {efforts.map((e) => (
                  <option key={e.value} value={e.value}>
                    {e.label} — {e.detail}
                  </option>
                ))}
              </select>
            )}
          </Field>
          <NumberSetting label="Thinking budget (tokens)" help="0 turns thinking off where the model allows it." value={s?.thinkingBudget} min={0} step={1024} onCommit={(v) => setModelSetting("thinking_budget", v)} />
        </Section>

        <Section title={`Generation${settings?.model ? ` · ${settings.model}` : ""}`}>
          <SliderSetting label="Temperature" value={s?.temperature} fallback={modelInfo?.globalTemperature} min={0} max={2} step={0.05} onCommit={(v) => setModelSetting("temperature", v)} />
          <SliderSetting label="Top P" value={s?.topP} fallback={1} min={0.01} max={1} step={0.01} onCommit={(v) => setModelSetting("top_p", v)} />
          <NumberSetting label="Max output tokens" value={s?.maxTokens} placeholder={modelInfo?.globalMaxTokens ? String(modelInfo.globalMaxTokens) : ""} min={1} step={256} onCommit={(v) => setModelSetting("max_tokens", v)} />
          <p className="t-body-sm muted">Saved to this model's settings in your configuration; empty uses the defaults.</p>
          <Button small onClick={() => act(async () => setModelInfo((await workspaces.updateModelSettings({ workspace: dir, ref: model, reset: true })).model), "Settings reset.")}>
            Reset to defaults
          </Button>
        </Section>

        <Section title="Behaviour">
          <Field label="Permission mode" supporting={modes.find((m) => m.value === settings?.permissionMode)?.detail}>
            {(id) => (
              <select id={id} className="select" value={settings?.permissionMode ?? "default"} onChange={(e) => act(() => workspaces.setPermissionMode({ workspace: dir, mode: e.target.value }))}>
                {modes.map((m) => (
                  <option key={m.value} value={m.value}>
                    {m.label}
                  </option>
                ))}
              </select>
            )}
          </Field>
          <Field label="Agency" supporting={agencies.find((a) => a.value === settings?.agency)?.detail}>
            {(id) => (
              <select id={id} className="select" value={settings?.agency ?? "high"} onChange={(e) => act(() => workspaces.setSetting({ workspace: dir, key: "agency", value: e.target.value }))}>
                {agencies.map((a) => (
                  <option key={a.value} value={a.value}>
                    {a.label}
                  </option>
                ))}
              </select>
            )}
          </Field>
          <Field label="Replies in">
            {(id) => (
              <select id={id} className="select" value={settings?.locale ?? ""} onChange={(e) => act(() => workspaces.setLocale({ workspace: dir, input: e.target.value }), "Language changed.")}>
                {settings?.locale && !locales.some((l) => l.tag === settings.locale) && <option value={settings.locale}>{settings.locale}</option>}
                {locales.map((l) => (
                  <option key={l.tag} value={l.tag}>
                    {l.name} ({l.tag})
                  </option>
                ))}
              </select>
            )}
          </Field>
        </Section>

        <Section title={`Permission rules (${rules.length})`} open={false}>
          <Rules dir={dir} rules={rules} act={act} />
        </Section>

        <Section title={`Standing approvals (${approvals.length})`} open={false}>
          {approvals.length === 0 && <p className="t-body-sm muted">None. Choosing “allow this session” or “always allow” adds one.</p>}
          <div className="list">
            {approvals.map((a) => (
              <div key={a.key} className="rule">
                <span className="chip static">{a.kind}</span>
                <code className="ellipsis" title={a.subject}>
                  {a.subject}
                </code>
                <span className="t-body-sm muted">{a.always ? "always" : "session"}</span>
                <IconButton icon={mdiDeleteOutline} label="Revoke" small onClick={() => act(() => workspaces.revokeApprovals({ workspace: dir, keys: [a.key] }), "Revoked.")} />
              </div>
            ))}
          </div>
          {approvals.length > 1 && (
            <Button small danger onClick={() => act(() => workspaces.revokeApprovals({ workspace: dir, all: true }), "All approvals revoked.")}>
              Revoke all
            </Button>
          )}
        </Section>

        <Section title="Context" open={false}>
          {usage && usage.calls > 0 ? (
            <dl className="facts">
              <dt>Context now</dt>
              <dd>{Number(usage.lastPrompt).toLocaleString()} tokens</dd>
              <dt>Sent / received</dt>
              <dd>
                {Number(usage.input).toLocaleString()} / {Number(usage.output).toLocaleString()}
              </dd>
              {usage.priced && (
                <>
                  <dt>Cost</dt>
                  <dd>${usage.costUsd.toFixed(4)}</dd>
                </>
              )}
            </dl>
          ) : (
            <p className="t-body-sm muted">Nothing sent in this session yet.</p>
          )}
          <Button
            small
            variant="tonal"
            onClick={() =>
              act(async () => {
                const r = await sessions.compact({ workspace: dir });
                snack(`Summarized ${r.eventsCompacted} events into ${r.summaryChars} characters.`);
              })
            }
          >
            Compact the context
          </Button>
        </Section>
      </div>
    </aside>
  );
}

function SliderSetting({
  label,
  value,
  fallback,
  min,
  max,
  step,
  onCommit,
}: {
  label: string;
  value?: number;
  fallback?: number;
  min: number;
  max: number;
  step: number;
  onCommit: (v: string) => void;
}) {
  const [v, setV] = useState(value ?? fallback ?? min);
  useEffect(() => setV(value ?? fallback ?? min), [value, fallback, min]);
  return (
    <div className="field">
      <label className="row">
        <span className="spacer">{label}</span>
        <span className="muted">{value === undefined ? "default" : ""}</span>
        <input
          className="input slider-value"
          type="number"
          min={min}
          max={max}
          step={step}
          value={Number(v.toFixed(2))}
          onChange={(e) => setV(Number(e.target.value))}
          onBlur={() => onCommit(String(v))}
        />
      </label>
      <input type="range" aria-label={label} min={min} max={max} step={step} value={v} onChange={(e) => setV(Number(e.target.value))} onPointerUp={() => onCommit(String(v))} onKeyUp={() => onCommit(String(v))} />
    </div>
  );
}

function NumberSetting({
  label,
  help,
  value,
  placeholder,
  min,
  step,
  onCommit,
}: {
  label: string;
  help?: string;
  value?: number;
  placeholder?: string;
  min: number;
  step: number;
  onCommit: (v: string) => void;
}) {
  const [v, setV] = useState(value === undefined ? "" : String(value));
  useEffect(() => setV(value === undefined ? "" : String(value)), [value]);
  return (
    <Field label={label} supporting={help}>
      {(id) => (
        <input
          id={id}
          className="input"
          type="number"
          min={min}
          step={step}
          value={v}
          placeholder={placeholder || "default"}
          onChange={(e) => setV(e.target.value)}
          onBlur={() => v !== (value === undefined ? "" : String(value)) && onCommit(v)}
          onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()}
        />
      )}
    </Field>
  );
}

function Rules({ dir, rules, act }: { dir: string; rules: PermissionRule[]; act: (f: () => Promise<unknown>, done?: string) => Promise<void> }) {
  const [effect, setEffect] = useState("allow");
  const [rule, setRule] = useState("");
  const [save, setSave] = useState(false);
  return (
    <>
      <p className="t-body-sm muted">
        Rules like <code>shell(git status)</code>, <code>write(docs/**)</code> or <code>web(github.com)</code>. Deny beats ask beats allow.
      </p>
      <div className="list">
        {rules.map((r) => (
          <div key={`${r.effect}:${r.rule}`} className="rule">
            <span className={`chip static effect-${r.effect}`}>{r.effect}</span>
            <code className="ellipsis" title={r.rule}>
              {r.rule}
            </code>
            <span className="t-body-sm muted">{r.source}</span>
            <IconButton icon={mdiDeleteOutline} label="Remove" small onClick={() => act(() => workspaces.removePermissionRule({ workspace: dir, rule: r.rule, save: r.source === "config" }), "Rule removed.")} />
          </div>
        ))}
      </div>
      <form
        className="stack"
        onSubmit={(e) => {
          e.preventDefault();
          if (!rule.trim()) return;
          act(() => workspaces.addPermissionRule({ workspace: dir, effect, rule: rule.trim(), save }), "Rule added.").then(() => setRule(""));
        }}
      >
        <div className="row">
          <select className="select rule-effect" value={effect} onChange={(e) => setEffect(e.target.value)} aria-label="Effect">
            <option value="allow">allow</option>
            <option value="ask">ask</option>
            <option value="deny">deny</option>
          </select>
          <input className="input mono" value={rule} onChange={(e) => setRule(e.target.value)} placeholder="shell(npm test)" aria-label="Rule" />
        </div>
        <div className="row">
          <Switch label="Save to the configuration" checked={save} onChange={setSave} />
          <span className="t-body-sm muted spacer">{save ? "Kept in the configuration" : "For this session"}</span>
          <Button small variant="tonal" icon={mdiPlus} type="submit" disabled={!rule.trim()}>
            Add
          </Button>
        </div>
      </form>
    </>
  );
}
