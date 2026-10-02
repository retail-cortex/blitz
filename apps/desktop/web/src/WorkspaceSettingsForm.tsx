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
import { createPortal } from "react-dom";
import { useOnConfigChanged } from "./workspaceSettings";
import {
  mdiAlertCircleOutline,
  mdiBrain,
  mdiChevronDown,
  mdiChevronRight,
  mdiDeleteOutline,
  mdiFileCogOutline,
  mdiFolderOutline,
  mdiKeyOutline,
  mdiRobotOutline,
  mdiShieldAlertOutline,
  mdiShieldCheckOutline,
  mdiShieldKeyOutline,
  mdiSineWave,
  mdiTextBoxOutline,
  mdiTune,
} from "@mdi/js";
import { config, sessions, workspaces } from "./api";
import { message, reason } from "./errors";
import { language, t, tn } from "./i18n";
import type { Usage } from "./gen/blitz/v1/turn_pb";
import type { AgentInfo, Approval, GetSettingsResponse, LocaleInfo, ModelSettingsInfo, PermissionRule, ProjectSettings, Style } from "./gen/blitz/v1/workspace_pb";
import { checkModelRef } from "./models";
import { ModelInput, useModelCatalog } from "./ModelInput";
import { agencies, efforts, modes } from "./options";
import { PermissionSettings } from "./PermissionSettings";
import { ProviderSettings } from "./ProviderSettings";
import { useApp } from "./state";
import { lookOf, WorkspaceFields, type WorkspaceLook } from "./WorkspaceFields";
import { editWorkspace, type WorkspacePrefs } from "./prefs";
import { projectState, waiting } from "./project";
import { ProjectDialog } from "./ProjectDialog";
import { Button, Field, Icon, IconButton, useSnackbar } from "./ui/controls";

// Which sections the user unfolded, kept in the window (localStorage):
// they start folded.
const openKey = "blitz.desktop.rs-open";
function openSections(): Set<string> {
  try {
    return new Set(JSON.parse(localStorage.getItem(openKey) ?? "[]"));
  } catch {
    return new Set();
  }
}
function keepOpen(id: string, open: boolean) {
  const all = openSections();
  if (open) all.add(id);
  else all.delete(id);
  try {
    localStorage.setItem(openKey, JSON.stringify([...all]));
  } catch {
    // not kept: they start folded next time
  }
}

/**
 * One of the panel's sections, a card: its icon and title, where what it
 * sets is kept (scope), and folded, a summary of what's set. pairs lays
 * its fields out two to a row when the panel is wide enough. It starts
 * folded, unless the user left it unfolded.
 */
function Section({
  id,
  icon,
  title,
  scope,
  summary,
  pairs,
  children,
}: {
  id: string;
  icon: string;
  title: string;
  scope?: string;
  summary?: string;
  pairs?: boolean;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(() => openSections().has(id));
  const toggle = () =>
    setOpen((o) => {
      keepOpen(id, !o);
      return !o;
    });
  return (
    <section className={`panel-section ${open ? "open" : ""}`}>
      <button className="panel-section-head" onClick={toggle} aria-expanded={open}>
        <Icon path={icon} size="sm" className="panel-section-icon" />
        <span className="panel-section-title">
          <span className="t-title-sm">{title}</span>
          {!open && summary && <span className="t-body-sm muted ellipsis">{summary}</span>}
        </span>
        {scope && <span className="scope-chip t-label-sm">{scope}</span>}
        <Icon path={open ? mdiChevronDown : mdiChevronRight} size="sm" />
      </button>
      {open && <div className={`panel-section-body ${pairs ? "pairs" : ""}`}>{children}</div>}
    </section>
  );
}

/**
 * A workspace's settings as one form of folding cards (Settings ›
 * Workspaces, for an open workspace: from its pencil in the Workspaces
 * menu, or the status bar's model):
 * how it's shown, agent and model, behaviour; under Advanced, thinking,
 * generation, API keys, permission rules, standing approvals, project
 * settings and its settings file; and the session's context. Simple
 * mode (advanced settings off) keeps how it's shown, its agent and model,
 * and its permission mode and reply language.
 */
export function WorkspaceSettingsForm({
  dir,
  settings,
  error,
  onChanged,
  onSettingsFile,
}: {
  dir: string;
  settings?: GetSettingsResponse;
  error: string;
  onChanged: () => void;
  onSettingsFile: () => void;
}) {
  const { prefs, update } = useApp();
  const advanced = prefs.advanced;
  const ws = prefs.workspaces.find((w) => w.dir === dir);
  const [reviewing, setReviewing] = useState(false);
  const [project, setProjectSettings] = useState<ProjectSettings>();
  const [filePath, setFilePath] = useState("");
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
  const catalog = useModelCatalog(dir);

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
      // Asides: the panel works without them.
      workspaces.getProjectSettings({ workspace: dir }).then((r) => setProjectSettings(r.settings), () => {});
      config.getConfigFile({ workspace: dir }).then((f) => setFilePath(f.path), () => {});
    } catch (e) {
      snack(message(e), { error: true });
    }
  }, [dir, snack]);
  useEffect(() => {
    load();
  }, [load]);
  // Rules and approvals changed elsewhere (the settings, a terminal, a file).
  useOnConfigChanged(dir, load);
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
    <>
      {/* Over the window, not the panel (a blurred pane holds what's fixed in it). */}
      {reviewing && createPortal(<ProjectDialog dir={dir} onClose={() => setReviewing(false)} onDecided={() => (onChanged(), void load())} />, document.body)}
      {error && (
        <div className="card error row small-card">
          <Icon path={mdiAlertCircleOutline} size="sm" /> {error}
        </div>
      )}
      <div className="panel-scroll">
        {ws && (
          <Section id="workspace" icon={mdiFolderOutline} title={t("desktop.rs.workspace")} summary={ws.description || ws.dir}>
            <LookFields ws={ws} onSave={(look) => update((p) => editWorkspace(p, dir, look))} />
          </Section>
        )}

        <Section id="agent" icon={mdiRobotOutline} title={t("desktop.rs.agent_model")} summary={settings ? `${settings.agent} · ${settings.model}` : undefined}>
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
          <Field label={t("desktop.rs.model")}>
            {(id) => (
              <ModelInput
                id={id}
                value={modelRef}
                onChange={setModelRef}
                catalog={catalog}
                provider={settings?.provider}
                extras={[model, ...models]}
                hint={t("desktop.rs.model_help")}
                onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()}
                onCommit={(ref) => ref && ref !== model && !checkModelRef(ref, catalog, settings?.provider).error && act(() => workspaces.setModel({ workspace: dir, ref }), t("desktop.rs.model_set", { model: ref }))}
              />
            )}
          </Field>
        </Section>

        <Section
          id="behaviour"
          icon={mdiTune}
          title={t("desktop.rs.behaviour")}
          summary={[modes().find((m) => m.value === (settings?.permissionMode ?? "default"))?.label, agencies().find((a) => a.value === settings?.agency)?.label].filter(Boolean).join(" · ")}
          pairs
        >
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
          {advanced && (
            <>
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
            </>
          )}
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

        {/* Simple mode stops here: what follows has sensible defaults. */}
        {advanced && (
          <>
            <div className="panel-divider" role="separator">
              <span className="t-label">{t("desktop.rs.advanced")}</span>
            </div>

            <Section id="thinking" icon={mdiBrain} title={t("desktop.rs.thinking")} summary={efforts().find((e) => e.value === (settings?.effort ?? ""))?.label} pairs>
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

            <Section
              id="generation"
              icon={mdiSineWave}
              title={settings?.model ? t("desktop.rs.generation_for", { model: settings.model }) : t("desktop.rs.generation")}
              scope={t("desktop.rs.scope.model")}
              summary={[s?.temperature !== undefined && `${t("desktop.rs.temperature")} ${s.temperature}`, s?.topP !== undefined && `${t("desktop.rs.top_p")} ${s.topP}`].filter(Boolean).join(" · ") || t("desktop.rs.defaults")}
            >
              <SliderSetting label={t("desktop.rs.temperature")} value={s?.temperature} fallback={modelInfo?.globalTemperature} min={0} max={2} step={0.05} onCommit={(v) => setModelSetting("temperature", v)} />
              <SliderSetting label={t("desktop.rs.top_p")} value={s?.topP} fallback={1} min={0.01} max={1} step={0.01} onCommit={(v) => setModelSetting("top_p", v)} />
              <NumberSetting label={t("desktop.rs.max_tokens")} value={s?.maxTokens} placeholder={modelInfo?.globalMaxTokens ? String(modelInfo.globalMaxTokens) : ""} min={1} step={256} onCommit={(v) => setModelSetting("max_tokens", v)} />
              <p className="t-body-sm muted">{t("desktop.rs.saved_note")}</p>
              <Button small onClick={() => act(async () => setModelInfo((await workspaces.updateModelSettings({ workspace: dir, ref: model, reset: true })).model), t("desktop.rs.reset_done"))}>
                {t("desktop.rs.reset")}
              </Button>
            </Section>

            <Section id="keys" icon={mdiKeyOutline} title={t("desktop.rs.keys")} scope={t("desktop.rs.scope.workspace")} summary={settings?.provider}>
              <ProviderSettings workspace={dir} compact />
            </Section>

            <Section id="rules" icon={mdiShieldCheckOutline} title={t("desktop.rs.rules", { count: rules.filter((r) => r.source !== "built-in").length })} scope={t("desktop.rs.scope.workspace")}>
              <PermissionSettings workspace={dir} compact onChanged={load} />
            </Section>

            <Section id="approvals" icon={mdiShieldKeyOutline} title={t("desktop.rs.approvals", { count: approvals.length })}>
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

            <Section
              id="project"
              icon={mdiShieldAlertOutline}
              title={t("desktop.project.title")}
              scope={t("desktop.rs.scope.project")}
              summary={project ? (waiting(project) ? tn("desktop.rs.project_waiting", waiting(project)) : projectState(project)) : undefined}
            >
              {project && (
                <>
                  <p className="t-body-sm">{waiting(project) ? tn("desktop.project.bar", waiting(project)) : projectState(project)}</p>
                  {project.files.length > 0 ? (
                    <code className="t-body-sm muted">{project.files.join(", ")}</code>
                  ) : (
                    <p className="t-body-sm muted">{t("desktop.rs.project_none")}</p>
                  )}
                </>
              )}
              {project && project.files.length > 0 && (
                <Button small variant={waiting(project) ? "tonal" : undefined} icon={mdiShieldAlertOutline} onClick={() => setReviewing(true)}>
                  {t("desktop.project.review")}
                </Button>
              )}
            </Section>

            <Section id="file" icon={mdiFileCogOutline} title={t("desktop.settings.file")} scope={t("desktop.rs.scope.workspace")} summary={t("desktop.rs.file_summary")}>
              <p className="t-body-sm muted">{t("desktop.rs.file_help")}</p>
              {filePath && (
                <code className="t-body-sm ellipsis" title={filePath}>
                  {filePath}
                </code>
              )}
              <Button small icon={mdiFileCogOutline} onClick={onSettingsFile}>
                {t("desktop.rs.file_edit")}
              </Button>
            </Section>

            <Section
              id="context"
              icon={mdiTextBoxOutline}
              title={t("desktop.rs.context")}
              scope={t("desktop.rs.scope.session")}
              summary={usage && usage.calls > 0 ? t("desktop.rs.tokens", { count: Number(usage.lastPrompt).toLocaleString(language()) }) : undefined}
         
            >
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
          </>
        )}
      </div>
    </>
  );
}

// How the workspace is shown, saved as it's changed: a name or description
// when its field is left, a colour as it's picked.
export function LookFields({ ws, onSave }: { ws: WorkspacePrefs; onSave: (look: WorkspaceLook) => void }) {
  const [look, setLook] = useState<WorkspaceLook>(() => lookOf(ws));
  useEffect(() => setLook(lookOf(ws)), [ws]);
  const saved = lookOf(ws);
  const save = (v: WorkspaceLook) => {
    if (v.name !== saved.name || v.description !== saved.description || v.color !== saved.color) onSave(v);
  };
  return (
    <WorkspaceFields
      ws={ws}
      value={look}
      compact
      onChange={(v) => {
        setLook(v);
        if (v.color !== look.color) save(v);
      }}
      onBlur={() => save(look)}
    />
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

