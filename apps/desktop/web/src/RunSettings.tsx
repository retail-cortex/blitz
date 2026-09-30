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

import { useCallback, useEffect, useState, type ReactNode } from "react";
import {
  mdiAlertCircleOutline,
  mdiChevronDown,
  mdiChevronRight,
  mdiClose,
  mdiDeleteOutline,
  mdiTuneVariant,
} from "@mdi/js";
import { sessions, workspaces } from "./api";
import { message, reason } from "./errors";
import { language, t } from "./i18n";
import type { Usage } from "./gen/blitz/v1/turn_pb";
import type { AgentInfo, Approval, GetSettingsResponse, LocaleInfo, ModelSettingsInfo, PermissionRule, Style } from "./gen/blitz/v1/workspace_pb";
import { agencies, efforts, modes } from "./options";
import { PermissionSettings } from "./PermissionSettings";
import { ProviderSettings } from "./ProviderSettings";
import { useApp } from "./state";
import { Button, Field, Icon, IconButton, useSnackbar } from "./ui/controls";

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
  const [styles, setStyles] = useState<Style[]>([]);
  const [rules, setRules] = useState<PermissionRule[]>([]);
  const [approvals, setApprovals] = useState<Approval[]>([]);
  const [usage, setUsage] = useState<Usage>();
  const [modelRef, setModelRef] = useState("");

  const model = settings ? (settings.provider ? `${settings.provider}/${settings.model}` : settings.model) : "";
  const load = useCallback(async () => {
    try {
      const [a, all, l, r, ap, u, st] = await Promise.all([
        workspaces.listAgents({ workspace: dir }),
        workspaces.getModelSettings({ workspace: dir }),
        workspaces.listLocales({ workspace: dir }),
        workspaces.listPermissionRules({ workspace: dir }),
        workspaces.listApprovals({ workspace: dir }),
        sessions.getUsage({ workspace: dir }).catch(() => undefined),
        workspaces.listStyles({ workspace: dir }).catch(() => undefined),
      ]);
      setStyles(st?.styles ?? []);
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
      snack(reason(e) === "BYPASS_NEEDS_SANDBOX" ? t("desktop.bypass_needs_sandbox") : message(e), { error: true });
    }
  };

  const setModelSetting = (key: string, value: string) =>
    act(async () => {
      const r = await workspaces.updateModelSettings({ workspace: dir, ref: model, changes: [{ key, value }] });
      setModelInfo(r.model);
      if (r.unsupported.length) snack(t("desktop.rs.unsupported", { model, keys: r.unsupported.join(", ") }));
    });

  const s = modelInfo?.settings;
  return (
    <aside className="run-settings" aria-label={t("desktop.run_settings")}>
      <div className="panel-head">
        <Icon path={mdiTuneVariant} />
        <span className="t-title">{t("desktop.run_settings")}</span>
        <span className="spacer" />
        <IconButton icon={mdiClose} label={t("desktop.rs.close")} small onClick={() => update((p) => ({ ...p, run_settings: false }))} />
      </div>
      {error && (
        <div className="card error row small-card">
          <Icon path={mdiAlertCircleOutline} size="sm" /> {error}
        </div>
      )}
      <div className="panel-scroll">
        <Section title={t("desktop.rs.agent_model")}>
          <Field label={t("desktop.rs.agent")}>
            {(id) => (
              <select id={id} className="select" value={settings?.agent ?? ""} onChange={(e) => act(() => workspaces.setAgent({ workspace: dir, name: e.target.value }), t("desktop.rs.agent_switched"))}>
                {agents.map((a) => (
                  <option key={a.name} value={a.name}>
                    {a.displayName}
                    {a.pinnedModel ? ` (${a.pinnedModel})` : ""}
                  </option>
                ))}
              </select>
            )}
          </Field>
          <Field label={t("desktop.rs.model")} supporting={t("desktop.rs.model_help")}>
            {(id) => (
              <>
                <input
                  id={id}
                  className="input mono"
                  list={`${id}-models`}
                  value={modelRef}
                  onChange={(e) => setModelRef(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()}
                  onBlur={() => modelRef.trim() && modelRef.trim() !== model && act(() => workspaces.setModel({ workspace: dir, ref: modelRef.trim() }), t("desktop.rs.model_set", { model: modelRef.trim() }))}
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

        <Section title={t("desktop.rs.keys")} open={false}>
          <ProviderSettings workspace={dir} compact />
        </Section>

        <Section title={t("desktop.rs.thinking")}>
          <Field label={t("desktop.rs.effort")} supporting={t("desktop.rs.effort_help")}>
            {(id) => (
              <select id={id} className="select" value={settings?.effort ?? ""} onChange={(e) => act(() => workspaces.setSetting({ workspace: dir, key: "effort", value: e.target.value || "auto" }))}>
                {efforts().map((e) => (
                  <option key={e.value} value={e.value}>
                    {e.label} — {e.detail}
                  </option>
                ))}
              </select>
            )}
          </Field>
          <NumberSetting label={t("desktop.rs.budget")} help={t("desktop.rs.budget_help")} value={s?.thinkingBudget} min={0} step={1024} onCommit={(v) => setModelSetting("thinking_budget", v)} />
        </Section>

        <Section title={settings?.model ? t("desktop.rs.generation_for", { model: settings.model }) : t("desktop.rs.generation")}>
          <SliderSetting label={t("desktop.rs.temperature")} value={s?.temperature} fallback={modelInfo?.globalTemperature} min={0} max={2} step={0.05} onCommit={(v) => setModelSetting("temperature", v)} />
          <SliderSetting label={t("desktop.rs.top_p")} value={s?.topP} fallback={1} min={0.01} max={1} step={0.01} onCommit={(v) => setModelSetting("top_p", v)} />
          <NumberSetting label={t("desktop.rs.max_tokens")} value={s?.maxTokens} placeholder={modelInfo?.globalMaxTokens ? String(modelInfo.globalMaxTokens) : ""} min={1} step={256} onCommit={(v) => setModelSetting("max_tokens", v)} />
          <p className="t-body-sm muted">{t("desktop.rs.saved_note")}</p>
          <Button small onClick={() => act(async () => setModelInfo((await workspaces.updateModelSettings({ workspace: dir, ref: model, reset: true })).model), t("desktop.rs.reset_done"))}>
            {t("desktop.rs.reset")}
          </Button>
        </Section>

        <Section title={t("desktop.rs.behaviour")}>
          <Field label={t("desktop.rs.mode")} supporting={modes().find((m) => m.value === settings?.permissionMode)?.detail}>
            {(id) => (
              <select id={id} className="select" value={settings?.permissionMode ?? "default"} onChange={(e) => act(() => workspaces.setPermissionMode({ workspace: dir, mode: e.target.value }))}>
                {modes().map((m) => (
                  <option key={m.value} value={m.value}>
                    {m.label}
                  </option>
                ))}
              </select>
            )}
          </Field>
          <Field label={t("desktop.rs.agency")} supporting={agencies().find((a) => a.value === settings?.agency)?.detail}>
            {(id) => (
              <select id={id} className="select" value={settings?.agency ?? "high"} onChange={(e) => act(() => workspaces.setSetting({ workspace: dir, key: "agency", value: e.target.value }))}>
                {agencies().map((a) => (
                  <option key={a.value} value={a.value}>
                    {a.label}
                  </option>
                ))}
              </select>
            )}
          </Field>
          <Field label={t("desktop.rs.style")} supporting={styles.find((s) => s.name === (settings?.style || "default"))?.description}>
            {(id) => (
              <select id={id} className="select" value={settings?.style || "default"} onChange={(e) => act(() => workspaces.setSetting({ workspace: dir, key: "style", value: e.target.value }))}>
                {styles.map((s) => (
                  <option key={s.name} value={s.name}>
                    {s.name.charAt(0).toUpperCase() + s.name.slice(1)}
                  </option>
                ))}
              </select>
            )}
          </Field>
          <Field label={t("desktop.rs.replies_in")}>
            {(id) => (
              <select id={id} className="select" value={settings?.locale ?? ""} onChange={(e) => act(() => workspaces.setLocale({ workspace: dir, input: e.target.value }), t("desktop.rs.language_changed"))}>
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

        <Section title={t("desktop.rs.rules", { count: rules.filter((r) => r.source !== "built-in").length })} open={false}>
          <PermissionSettings workspace={dir} compact onChanged={load} />
        </Section>

        <Section title={t("desktop.rs.approvals", { count: approvals.length })} open={false}>
          {approvals.length === 0 && <p className="t-body-sm muted">{t("desktop.rs.approvals_none")}</p>}
          <div className="list">
            {approvals.map((a) => (
              <div key={a.key} className="rule">
                <span className="chip static">{a.kind}</span>
                <code className="ellipsis" title={a.subject}>
                  {a.subject}
                </code>
                <span className="t-body-sm muted">{a.always ? t("desktop.rs.always") : t("desktop.rs.session")}</span>
                <IconButton icon={mdiDeleteOutline} label={t("desktop.rs.revoke")} small onClick={() => act(() => workspaces.revokeApprovals({ workspace: dir, keys: [a.key] }), t("desktop.rs.revoked"))} />
              </div>
            ))}
          </div>
          {approvals.length > 1 && (
            <Button small danger onClick={() => act(() => workspaces.revokeApprovals({ workspace: dir, all: true }), t("desktop.rs.revoked_all"))}>
              {t("desktop.rs.revoke_all")}
            </Button>
          )}
        </Section>

        <Section title={t("desktop.rs.context")} open={false}>
          {usage && usage.calls > 0 ? (
            <dl className="facts">
              <dt>{t("desktop.rs.context_now")}</dt>
              <dd>{t("desktop.rs.tokens", { count: Number(usage.lastPrompt).toLocaleString(language()) })}</dd>
              <dt>{t("desktop.rs.sent_received")}</dt>
              <dd>
                {Number(usage.input).toLocaleString(language())} / {Number(usage.output).toLocaleString(language())}
              </dd>
              {usage.priced && (
                <>
                  <dt>{t("desktop.rs.cost")}</dt>
                  <dd>${usage.costUsd.toFixed(4)}</dd>
                </>
              )}
            </dl>
          ) : (
            <p className="t-body-sm muted">{t("desktop.rs.nothing_sent")}</p>
          )}
          <Button
            small
            variant="tonal"
            onClick={() =>
              act(async () => {
                const r = await sessions.compact({ workspace: dir });
                snack(t("desktop.compacted", { events: r.eventsCompacted, chars: r.summaryChars }));
              })
            }
          >
            {t("desktop.rs.compact")}
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
        <span className="muted">{value === undefined ? t("desktop.rs.default") : ""}</span>
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

