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

import { mdiDeleteOutline, mdiPlus } from "@mdi/js";
import { Button, Field, IconButton, MultiSelect, Segmented } from "@blitz/webui";
import { A2uiForm } from "../a2ui/A2uiForm";
import { parseForm, problemText } from "../a2ui/model";
import type { SkillInfo, WorkflowRunStep } from "../gen/blitz/v1/workspace_pb";
import { t } from "../i18n";
import { SuggestInput, SuggestTextarea } from "../SecretSuggest";
import { DataStepFields } from "./DataStepFields";
import { DraftButton } from "./Draft";
import { MarkdownField } from "./MarkdownField";
import { agentTypes, gatewayTypes, methods, pauseTypes, type AuthForm, type FieldForm, type PairForm, type StepForm } from "./workflowForm";

/** Settings form for a selected step on the workflow canvas. */
export function WorkflowStepFields({
  workspace,
  workflowName,
  step,
  stepRun,
  agents,
  skills,
  rules,
  mcpServers,
  otherWorkflows,
  onChange,
}: {
  workspace: string;
  workflowName: string;
  step: StepForm;
  stepRun?: WorkflowRunStep;
  agents: string[];
  skills: SkillInfo[];
  rules: string[];
  mcpServers: string[];
  otherWorkflows: string[];
  onChange: (patch: Partial<StepForm>) => void;
}) {
  const scriptsOf = (skill: string) => skills.find((s) => s.name === skill)?.scripts.map((x) => x.name) ?? [];
  return (
    <div className="stack flow-step-form" style={{ gap: 10 }}>
      <span className="t-body-sm muted">{t(`desktop.flows.type_hint.${step.type}`)}</span>
      <Field label={t("desktop.flows.step_title")}>{(id) => <input id={id} className="input" value={step.title} placeholder={t(`desktop.flows.type.${step.type}`)} onChange={(e) => onChange({ title: e.target.value })} />}</Field>
      {agentTypes.includes(step.type) && (
        <>
          <Field label={t("desktop.flows.agent")}>
            {(id) => (
              <select id={id} className="select" value={step.agent} onChange={(e) => onChange({ agent: e.target.value })}>
                <option value="">{t("desktop.flows.active_agent")}</option>
                {[...new Set([...agents, step.agent].filter(Boolean))].map((a) => (
                  <option key={a} value={a}>
                    {a}
                  </option>
                ))}
              </select>
            )}
          </Field>
          <MarkdownField
            key={step.id}
            label={t(`desktop.flows.instructions.${step.type}`)}
            supporting={t("desktop.flows.template_hint")}
            value={step.instructions}
            rows={5}
            placeholder={t(`desktop.flows.instructions_placeholder.${step.type}`)}
            onValue={(v) => onChange({ instructions: v })}
            dir={workspace}
            workspace={workspace}
            tools={<DraftButton workspace={workspace} kind="workflow_step" name={step.title || step.id} description={step.title} current={step.instructions} onUse={(v) => onChange({ instructions: v })} />}
          />
          <Field label={t("desktop.flows.skills")} supporting={t("desktop.flows.skills_hint")}>
            {(id) => <MultiSelect id={id} label={t("desktop.flows.skills")} none={t("desktop.flows.none_chosen")} options={skills.map((s) => ({ value: s.name, detail: s.description }))} chosen={step.skills} onChange={(v) => onChange({ skills: v })} />}
          </Field>
          <Field label={t("desktop.flows.rules")} supporting={t("desktop.flows.rules_hint")}>
            {(id) => <MultiSelect id={id} label={t("desktop.flows.rules")} none={t("desktop.flows.none_chosen")} options={rules.map((r) => ({ value: r }))} chosen={step.rules} onChange={(v) => onChange({ rules: v })} />}
          </Field>
        </>
      )}
      {step.type === "script" && (
        <>
          <Field label={t("desktop.flows.skill")}>
            {(id) => (
              <select id={id} className="select" value={step.skill} onChange={(e) => onChange({ skill: e.target.value, script: "" })}>
                <option value="">{t("desktop.flows.choose")}</option>
                {skills
                  .filter((s) => s.scripts.length > 0)
                  .map((s) => (
                    <option key={s.name} value={s.name}>
                      {s.name}
                    </option>
                  ))}
              </select>
            )}
          </Field>
          <Field label={t("desktop.flows.script")}>
            {(id) => (
              <select id={id} className="select" value={step.script} disabled={!step.skill} onChange={(e) => onChange({ script: e.target.value })}>
                <option value="">{t("desktop.flows.choose")}</option>
                {[...new Set([...scriptsOf(step.skill), step.script].filter(Boolean))].map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </select>
            )}
          </Field>
          <Field label={t("desktop.flows.args")} supporting={t("desktop.flows.args_hint")}>
            {(id) => <input id={id} className="input mono" value={step.args} onChange={(e) => onChange({ args: e.target.value })} />}
          </Field>
        </>
      )}
      {(step.type === "read_file" || step.type === "save_file") && (
        <Field label={t("desktop.flows.path")} supporting={t(`desktop.flows.path_hint.${step.type}`)}>
          {(id) => <input id={id} className="input mono" value={step.path} placeholder={t("desktop.flows.path_placeholder")} onChange={(e) => onChange({ path: e.target.value })} />}
        </Field>
      )}
      {step.type === "api_call" && (
        <>
          <Field label={t("desktop.flows.connector")} supporting={t("desktop.flows.connector_hint")}>
            {(id) => <input id={id} className="input mono" value={step.connector} placeholder={t("desktop.flows.connector_placeholder")} onChange={(e) => onChange({ connector: e.target.value })} />}
          </Field>
          <div className="row flow-method" style={{ gap: 6 }}>
            <select className="select" aria-label={t("desktop.flows.method")} value={step.method} onChange={(e) => onChange({ method: e.target.value })}>
              {methods.map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
            </select>
            <SuggestInput className="input mono" aria-label={t("desktop.flows.url")} placeholder={t("desktop.flows.url_placeholder")} value={step.url} onValue={(v) => onChange({ url: v })} workspace={workspace} />
          </div>
          <span className="t-body-sm muted">{t("desktop.flows.call_hint")}</span>
          <Pairs label={t("desktop.flows.query")} pairs={step.query} workspace={workspace} onChange={(v) => onChange({ query: v })} />
          <Pairs label={t("desktop.flows.headers")} pairs={step.headers} workspace={workspace} onChange={(v) => onChange({ headers: v })} />
          {step.method !== "GET" && step.method !== "HEAD" && (
            <div className="stack" style={{ gap: 4 }}>
              <span className="t-label">{t("desktop.flows.body")}</span>
              <Segmented
                label={t("desktop.flows.body")}
                value={step.bodyKind}
                options={(["none", "text", "json"] as const).map((k) => ({ value: k, label: t(`desktop.flows.body_kinds.${k}`) }))}
                onChange={(v) => onChange({ bodyKind: v as StepForm["bodyKind"] })}
              />
              {step.bodyKind === "none" ? (
                <span className="t-body-sm muted">{t("desktop.flows.body_none")}</span>
              ) : (
                <SuggestTextarea className="input mono" rows={5} aria-label={t("desktop.flows.body")} value={step.body} placeholder={step.bodyKind === "json" ? t("desktop.flows.json_placeholder") : ""} onValue={(v) => onChange({ body: v })} workspace={workspace} />
              )}
            </div>
          )}
          <AuthFields auth={step.auth} workspace={workspace} onChange={(a) => onChange({ auth: a })} />
        </>
      )}
      {step.type === "mcp_call" && (
        <>
          <Field label={t("desktop.flows.mcp_server")}>
            {(id) => (
              <select id={id} className="select" value={step.server} onChange={(e) => onChange({ server: e.target.value })}>
                <option value="">{t("desktop.flows.choose")}</option>
                {[...new Set([...mcpServers, step.server].filter(Boolean))].map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </select>
            )}
          </Field>
          <Field label={t("desktop.flows.mcp_tool")} supporting={t("desktop.flows.mcp_tool_hint")}>
            {(id) => <input id={id} className="input mono" value={step.tool} onChange={(e) => onChange({ tool: e.target.value })} />}
          </Field>
          <Field label={t("desktop.flows.arguments")} supporting={t("desktop.flows.arguments_hint")}>
            {(id) => <SuggestTextarea id={id} className="input mono" rows={5} value={step.argumentsJson} placeholder={t("desktop.flows.json_placeholder")} onValue={(v) => onChange({ argumentsJson: v })} workspace={workspace} />}
          </Field>
        </>
      )}
      {step.type === "data" && <DataStepFields workspace={workspace} step={step} onChange={onChange} />}
      {step.type === "a2a_call" && (
        <>
          <Field label={t("desktop.flows.agent_url")} supporting={t("desktop.flows.agent_url_hint")}>
            {(id) => <SuggestInput id={id} className="input mono" value={step.url} placeholder={t("desktop.flows.agent_url_placeholder")} onValue={(v) => onChange({ url: v })} workspace={workspace} />}
          </Field>
          <Field label={t("desktop.flows.message")} supporting={t("desktop.flows.message_hint")}>
            {(id) => <SuggestTextarea id={id} className="input" rows={4} value={step.instructions} onValue={(v) => onChange({ instructions: v })} workspace={workspace} />}
          </Field>
          <Pairs label={t("desktop.flows.headers")} pairs={step.headers} workspace={workspace} onChange={(v) => onChange({ headers: v })} />
        </>
      )}
      {step.type === "workflow" && (
        <Field label={t("desktop.flows.call_workflow")} supporting={t("desktop.flows.call_workflow_hint")}>
          {(id) => (
            <select id={id} className="select" value={step.workflow} onChange={(e) => onChange({ workflow: e.target.value })}>
              <option value="">{t("desktop.flows.choose")}</option>
              {[...new Set([...otherWorkflows.filter((n) => n !== workflowName), step.workflow].filter(Boolean))].map((n) => (
                <option key={n} value={n}>
                  /{n}
                </option>
              ))}
            </select>
          )}
        </Field>
      )}
      {step.type === "human_task" && <HumanTaskFields step={step} workspace={workspace} onChange={onChange} />}
      {step.type === "timer" && (
        <>
          <Field label={t("desktop.flows.wait")} supporting={t("desktop.flows.wait_hint")}>
            {(id) => <input id={id} className="input mono" value={step.wait} placeholder="2h" onChange={(e) => onChange({ wait: e.target.value, until: "" })} />}
          </Field>
          <Field label={t("desktop.flows.until")} supporting={t("desktop.flows.until_hint")}>
            {(id) => <input id={id} className="input mono" value={step.until} placeholder="2026-10-20T09:00:00Z" onChange={(e) => onChange({ until: e.target.value, wait: "" })} />}
          </Field>
        </>
      )}
      {step.type === "extract" && (
        <div className="stack" style={{ gap: 6 }}>
          <span className="t-label">{t("desktop.flows.fields")}</span>
          <span className="t-body-sm muted">{t("desktop.flows.fields_hint", { id: step.id })}</span>
          {step.fields.map((fl, i) => (
            <div key={i} className="row flow-field" style={{ gap: 6 }}>
              <input className="input mono" aria-label={t("desktop.flows.field_name")} placeholder={t("desktop.flows.field_name")} value={fl.name} onChange={(e) => onChange({ fields: step.fields.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)) })} />
              <select className="select" aria-label={t("desktop.flows.field_type")} value={fl.type} onChange={(e) => onChange({ fields: step.fields.map((x, j) => (j === i ? { ...x, type: e.target.value as FieldForm["type"] } : x)) })}>
                {(["string", "number", "boolean", "list"] as const).map((ty) => (
                  <option key={ty} value={ty}>
                    {t(`desktop.flows.field_types.${ty}`)}
                  </option>
                ))}
              </select>
              <IconButton icon={mdiDeleteOutline} label={t("desktop.flows.field_remove")} small onClick={() => onChange({ fields: step.fields.filter((_, j) => j !== i) })} />
            </div>
          ))}
          <div className="row">
            <Button small icon={mdiPlus} onClick={() => onChange({ fields: [...step.fields, { name: "", type: "string", description: "" }] })}>
              {t("desktop.flows.field_add")}
            </Button>
          </div>
        </div>
      )}
      {!gatewayTypes.includes(step.type) && !pauseTypes.includes(step.type) && (
        <div className="stack" style={{ gap: 6 }}>
          <span className="t-label">{t("desktop.flows.repeat")}</span>
          <span className="t-body-sm muted">{t("desktop.flows.repeat_hint")}</span>
          <div className="row flow-field" style={{ gap: 6 }}>
            <input className="input mono" aria-label={t("desktop.flows.for_each")} placeholder={t("desktop.flows.for_each_placeholder")} value={step.forEach} onChange={(e) => onChange({ forEach: e.target.value })} />
            <input className="input flow-concurrency" type="number" min={1} max={32} aria-label={t("desktop.flows.concurrency")} title={t("desktop.flows.concurrency")} placeholder="4" disabled={!step.forEach.trim()} value={step.concurrency} onChange={(e) => onChange({ concurrency: e.target.value })} />
          </div>
        </div>
      )}
      {!gatewayTypes.includes(step.type) && !pauseTypes.includes(step.type) && (
        <div className="stack" style={{ gap: 6 }}>
          <span className="t-label">{t("desktop.flows.if_it_fails")}</span>
          <span className="t-body-sm muted">{t("desktop.flows.if_it_fails_hint")}</span>
          <div className="flow-retry">
            <Field label={t("desktop.flows.retry_attempts")}>{(id) => <input id={id} className="input" type="number" min={1} max={10} value={step.retryAttempts} placeholder="1" onChange={(e) => onChange({ retryAttempts: e.target.value })} />}</Field>
            <Field label={t("desktop.flows.retry_delay")}>{(id) => <input id={id} className="input mono" value={step.retryDelay} placeholder="1s" onChange={(e) => onChange({ retryDelay: e.target.value })} />}</Field>
            <Field label={t("desktop.flows.timeout")}>{(id) => <input id={id} className="input mono" value={step.timeout} placeholder="5m" onChange={(e) => onChange({ timeout: e.target.value })} />}</Field>
          </div>
        </div>
      )}
      {stepRun && (stepRun.output || stepRun.error) && (
        <div className="stack" style={{ gap: 2 }}>
          <span className="t-label">{t(stepRun.error ? "desktop.flows.step_error" : "desktop.flows.step_output")}</span>
          <pre className="eval-text">{stepRun.error || stepRun.output}</pre>
        </div>
      )}
    </div>
  );
}

/** Names and values (headers, query parameters), each completing {secrets:…}. */
function Pairs({ label, pairs, workspace, onChange }: { label: string; pairs: PairForm[]; workspace: string; onChange: (v: PairForm[]) => void }) {
  return (
    <div className="stack" style={{ gap: 4 }}>
      <span className="t-label">{label}</span>
      {pairs.map((p, i) => (
        <div key={i} className="row flow-field" style={{ gap: 6 }}>
          <input className="input mono" aria-label={t("desktop.flows.pair_name")} placeholder={t("desktop.flows.pair_name")} value={p.name} onChange={(e) => onChange(pairs.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)))} />
          <SuggestInput className="input mono" aria-label={t("desktop.flows.pair_value")} placeholder={t("desktop.flows.pair_value")} value={p.value} onValue={(v) => onChange(pairs.map((x, j) => (j === i ? { ...x, value: v } : x)))} workspace={workspace} />
          <IconButton icon={mdiDeleteOutline} label={t("desktop.flows.pair_remove")} small onClick={() => onChange(pairs.filter((_, j) => j !== i))} />
        </div>
      ))}
      <div className="row">
        <Button small icon={mdiPlus} onClick={() => onChange([...pairs, { name: "", value: "" }])}>
          {t("desktop.flows.pair_add")}
        </Button>
      </div>
    </div>
  );
}

/** An API call's auth: its type, then that type's values (secrets by name). */
function AuthFields({ auth, workspace, onChange }: { auth: AuthForm; workspace: string; onChange: (a: AuthForm) => void }) {
  const set = (patch: Partial<AuthForm>) => onChange({ ...auth, ...patch });
  const secret = (key: keyof AuthForm, label: string) => (
    <Field label={label}>{(id) => <SuggestInput id={id} className="input mono" value={String(auth[key])} placeholder={t("desktop.flows.secret_placeholder")} onValue={(v) => set({ [key]: v } as Partial<AuthForm>)} workspace={workspace} />}</Field>
  );
  return (
    <div className="stack" style={{ gap: 6 }}>
      <Field label={t("desktop.flows.auth")} supporting={t("desktop.flows.auth_hint")}>
        {(id) => (
          <select id={id} className="select" value={auth.type} onChange={(e) => set({ type: e.target.value as AuthForm["type"] })}>
            {(["", "bearer", "basic", "api_key", "oauth2"] as const).map((k) => (
              <option key={k} value={k}>
                {t(`desktop.flows.auth_types.${k || "none"}`)}
              </option>
            ))}
          </select>
        )}
      </Field>
      {auth.type === "bearer" && secret("token", t("desktop.flows.auth_token"))}
      {auth.type === "basic" && (
        <>
          {secret("username", t("desktop.flows.auth_username"))}
          {secret("password", t("desktop.flows.auth_password"))}
        </>
      )}
      {auth.type === "api_key" && (
        <>
          <div className="row" style={{ gap: 6 }}>
            <Field label={t("desktop.flows.auth_key_name")}>{(id) => <input id={id} className="input mono" value={auth.name} placeholder={t("desktop.flows.auth_key_name_placeholder")} onChange={(e) => set({ name: e.target.value })} />}</Field>
            <Field label={t("desktop.flows.auth_key_in")}>
              {(id) => (
                <select id={id} className="select" value={auth.in} onChange={(e) => set({ in: e.target.value as AuthForm["in"] })}>
                  <option value="header">{t("desktop.flows.auth_in.header")}</option>
                  <option value="query">{t("desktop.flows.auth_in.query")}</option>
                </select>
              )}
            </Field>
          </div>
          {secret("value", t("desktop.flows.auth_key_value"))}
        </>
      )}
      {auth.type === "oauth2" && (
        <>
          <Field label={t("desktop.flows.auth_token_url")}>{(id) => <SuggestInput id={id} className="input mono" value={auth.tokenUrl} placeholder={t("desktop.flows.auth_token_url_placeholder")} onValue={(v) => set({ tokenUrl: v })} workspace={workspace} />}</Field>
          {secret("clientId", t("desktop.flows.auth_client_id"))}
          {secret("clientSecret", t("desktop.flows.auth_client_secret"))}
          <Field label={t("desktop.flows.auth_scopes")} supporting={t("desktop.flows.auth_scopes_hint")}>{(id) => <input id={id} className="input mono" value={auth.scopes} onChange={(e) => set({ scopes: e.target.value })} />}</Field>
          <Field label={t("desktop.flows.auth_audience")}>{(id) => <input id={id} className="input mono" value={auth.audience} onChange={(e) => set({ audience: e.target.value })} />}</Field>
        </>
      )}
    </div>
  );
}

/** A human task's settings: the message above its form, and the form (A2UI JSON) beside a preview of it. */
function HumanTaskFields({ step, workspace, onChange }: { step: StepForm; workspace: string; onChange: (patch: Partial<StepForm>) => void }) {
  const parsed = parseForm(step.formJson);
  return (
    <>
      <Field label={t("desktop.flows.task_message")} supporting={t("desktop.flows.task_message_hint")}>
        {(id) => <SuggestTextarea id={id} className="input" rows={3} value={step.instructions} onValue={(v) => onChange({ instructions: v })} workspace={workspace} />}
      </Field>
      <div className="row field-tools">
        <span className="t-label spacer">{t("desktop.flows.form")}</span>
        <DraftButton workspace={workspace} kind="form" name={step.title || step.id} description={step.instructions} current={step.formJson} onUse={(v) => onChange({ formJson: v })} />
      </div>
      <textarea className="input mono flow-form-json" rows={10} aria-label={t("desktop.flows.form")} value={step.formJson} spellCheck={false} onChange={(e) => onChange({ formJson: e.target.value })} />
      <span className={`t-body-sm ${parsed.problem ? "error-text" : "muted"}`}>{parsed.problem ? t("desktop.flows.form_problem", { problem: problemText(parsed.problem) }) : t("desktop.flows.form_hint")}</span>
      {parsed.form && (
        <div className="card outlined flow-form-preview">
          <span className="t-label">{t("desktop.flows.form_preview")}</span>
          <A2uiForm form={parsed.form} />
        </div>
      )}
    </>
  );
}
