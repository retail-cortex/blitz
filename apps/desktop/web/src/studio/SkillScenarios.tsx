/*
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

import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { mdiCheckCircleOutline, mdiCloseCircleOutline, mdiContentSaveOutline, mdiDeleteOutline, mdiPencilOutline, mdiPlay, mdiPlus, mdiStop, mdiTestTube } from "@mdi/js";
import { workspaces } from "../api";
import { message } from "../errors";
import { t, tn } from "../i18n";
import type { EvalResult, EvalRun, SkillFile, SkillScenario, TrySkillResponse } from "../gen/blitz/v1/workspace_pb";
import { Button, Dialog, Field, Icon, IconButton, Switch, useSnackbar } from "../ui/controls";
import { money, resultsCost } from "./evalForm";
import { Results, statusClass, statusIcon } from "./EvalsPane";
import { scenarioName } from "./skillForm";

/** A scenario as the Keep it dialog edits it. */
interface ScenarioForm {
  name: string;
  description: string;
  prompt: string;
  executes: boolean;
  expected: string[];
  threshold: number;
  outcome: string;
}

/**
 * A skill's Scenarios tab (spec_skills_013 SK-111, SK-112): **Try it**
 * runs a prompt once (the skill offered, or given), showing whether the
 * agent used it and what it said; **Keep it** saves that as a scenario
 * (scenarios/<name>.md, Castor's format); the skill's scenarios are
 * listed with **Run all**, which runs them in the background as the eval
 * "skill:<name>" and follows the run (again when the tab opens).
 */
export function SkillScenarios({ workspace, file, agents, changed, onChanged }: { workspace: string; file: SkillFile; agents: string[]; changed: boolean; onChanged: () => void }) {
  const snack = useSnackbar();
  const skill = file.skill?.name ?? "";
  const editable = file.editable;
  const evalName = `skill:${skill}`;
  const [list, setList] = useState<SkillScenario[]>([]);
  const [agent, setAgent] = useState("");
  const [prompt, setPrompt] = useState("");
  const [force, setForce] = useState(false);
  const [trying, setTrying] = useState(false);
  const [tried, setTried] = useState<TrySkillResponse | null>(null);
  const [editing, setEditing] = useState<{ form: ScenarioForm; previous: string } | null>(null);
  const [problems, setProblems] = useState<string[]>([]);
  const [deleting, setDeleting] = useState("");
  const [live, setLive] = useState<{ run: EvalRun | null; results: EvalResult[]; total: number } | null>(null);
  const following = useRef<AbortController | null>(null);
  const running = !!live && !live.run?.status.match(/passed|failed|stopped/);

  const load = useCallback(async () => {
    try {
      setList((await workspaces.listSkillScenarios({ workspace, skill })).scenarios);
    } catch (e) {
      snack(message(e), { error: true });
    }
  }, [workspace, skill, snack]);

  // Follows a run of the scenarios into live; leaving stops only the following.
  const follow = useCallback(
    async (total: number, events: (signal: AbortSignal) => AsyncIterable<{ event: { case?: string; value?: unknown } }>) => {
      following.current?.abort();
      const ctl = new AbortController();
      following.current = ctl;
      setLive({ run: null, results: [], total });
      try {
        for await (const res of events(ctl.signal)) {
          const ev = res.event;
          if (ev.case === "started") {
            const run = ev.value as EvalRun;
            setLive((l) => l && { ...l, run: { ...run, results: [] }, results: run.results });
          } else if (ev.case === "result") setLive((l) => l && { ...l, results: [...l.results, ev.value as EvalResult] });
          else if (ev.case === "finished") {
            const run = ev.value as EvalRun;
            setLive((l) => l && { ...l, run, results: run.results });
            if (run.status !== "stopped") snack(t(run.status === "passed" ? "desktop.scenarios.passed" : "desktop.scenarios.failed", { name: skill }), { error: run.status !== "passed" });
          }
        }
      } catch (e) {
        if (ctl.signal.aborted) return; // left: the run goes on
        setLive(null);
        snack(message(e), { error: true });
      } finally {
        if (following.current === ctl) following.current = null;
      }
    },
    [skill, snack],
  );

  useEffect(() => {
    void load();
    // A run going on (started here before, or elsewhere) is followed again.
    workspaces.listEvalRuns({ workspace, name: evalName, limit: 1 }).then(
      (r) => {
        const going = r.runs.find((x) => x.status === "running");
        if (going) void follow(file.skill?.scenarios ?? 0, (signal) => workspaces.watchEvalRun({ workspace, id: going.id }, { signal }));
      },
      () => {},
    );
    return () => following.current?.abort();
  }, [workspace, evalName, load, follow, file.skill?.scenarios]);

  const tryIt = async () => {
    setTrying(true);
    setTried(null);
    try {
      setTried(await workspaces.trySkill({ workspace, skill, agent, prompt, force }));
    } catch (e) {
      snack(message(e), { error: true });
    } finally {
      setTrying(false);
    }
  };
  // Keep it: the try as a scenario, the agent's answer as the outcome.
  const keep = (r: TrySkillResponse) => {
    setProblems([]);
    setEditing({
      previous: "",
      form: { name: scenarioName(prompt, list.map((s) => s.name)), description: "", prompt, executes: r.triggered, expected: r.triggered ? r.skillsUsed : [], threshold: 0.8, outcome: r.output },
    });
  };
  const startNew = () => {
    setProblems([]);
    setEditing({ previous: "", form: { name: scenarioName(prompt || "scenario", list.map((s) => s.name)), description: "", prompt, executes: true, expected: [], threshold: 0.8, outcome: "" } });
  };
  const edit = (s: SkillScenario) => {
    setProblems([]);
    setEditing({ previous: s.name, form: { name: s.name, description: s.description, prompt: s.prompt, executes: s.executes, expected: [...s.expectedSkills], threshold: s.threshold, outcome: s.outcome } });
  };
  const save = async () => {
    if (!editing) return;
    const f = editing.form;
    try {
      const r = await workspaces.saveSkillScenario({ workspace, skill, previousName: editing.previous, scenario: { name: f.name.trim(), description: f.description, prompt: f.prompt, executes: f.executes, expectedSkills: f.executes ? f.expected : [], threshold: f.threshold, outcome: f.outcome } });
      if (r.problems.length) return setProblems(r.problems);
      snack(t("desktop.scenarios.saved", { name: r.scenario?.name ?? f.name }));
      setEditing(null);
      await load();
      onChanged();
    } catch (e) {
      setProblems([message(e)]);
    }
  };
  const remove = async (name: string) => {
    setDeleting("");
    try {
      await workspaces.deleteSkillScenario({ workspace, skill, name });
      await load();
      onChanged();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const runAll = () => void follow(list.length, (signal) => workspaces.runEval({ workspace, name: evalName, background: true }, { signal }));
  const stop = async () => {
    const id = live?.run?.id;
    if (!id) return;
    try {
      await workspaces.stopEvalRun({ workspace, id });
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const setField = <K extends keyof ScenarioForm>(k: K, v: ScenarioForm[K]) => setEditing((e) => e && { ...e, form: { ...e.form, [k]: v } });

  return (
    <div className="stack skill-scenarios" style={{ gap: 16 }}>
      <section className="card outlined stack" style={{ gap: 10 }}>
        <span className="t-title-sm">{t("desktop.scenarios.try")}</span>
        <span className="t-body-sm muted">{t("desktop.scenarios.try_hint")}</span>
        {changed && <span className="t-body-sm warn-text">{t("desktop.scenarios.as_saved")}</span>}
        <div className="pair">
          <Field label={t("desktop.scenarios.agent")}>
            {(id) => (
              <select id={id} className="select" value={agent} onChange={(e) => setAgent(e.target.value)}>
                <option value="">{file.skill?.agents[0] ? t("desktop.scenarios.agent_skill", { name: file.skill.agents[0] }) : t("desktop.scenarios.agent_active")}</option>
                {agents.map((a) => (
                  <option key={a} value={a}>
                    {a}
                  </option>
                ))}
              </select>
            )}
          </Field>
          <label className="row agent-switch">
            <Switch label={t("desktop.scenarios.force")} checked={force} onChange={setForce} />
            <span className="stack" style={{ gap: 0 }}>
              <span className="t-label">{t("desktop.scenarios.force")}</span>
              <span className="t-body-sm muted">{t(force ? "desktop.scenarios.force_on" : "desktop.scenarios.force_off")}</span>
            </span>
          </label>
        </div>
        <Field label={t("desktop.scenarios.prompt")}>{(id) => <textarea id={id} className="input" rows={3} value={prompt} placeholder={t("desktop.scenarios.prompt_placeholder")} onChange={(e) => setPrompt(e.target.value)} />}</Field>
        <div className="row">
          <span className="spacer" />
          <Button variant="tonal" icon={mdiPlay} iconSpin={trying} disabled={trying || !prompt.trim()} onClick={() => void tryIt()}>
            {trying ? t("desktop.scenarios.trying") : t("desktop.scenarios.try_button")}
          </Button>
        </div>
        {tried && (
          <div className="stack scenario-tried" style={{ gap: 8 }}>
            <div className="row" style={{ gap: 8 }}>
              <Icon path={tried.triggered ? mdiCheckCircleOutline : mdiCloseCircleOutline} className={tried.triggered ? "" : "warn-text"} />
              <span className="t-title-sm spacer">{tried.forced ? t("desktop.scenarios.given", { name: skill }) : t(tried.triggered ? "desktop.scenarios.triggered" : "desktop.scenarios.not_triggered", { name: skill })}</span>
              <span className="t-body-sm muted">{[tried.agent, money(tried.costUsd)].join(" · ")}</span>
            </div>
            {tried.skillsUsed.length > 0 && <span className="t-body-sm muted">{t("desktop.scenarios.used", { skills: tried.skillsUsed.join(", ") })}</span>}
            <pre className="eval-text">{tried.output || "—"}</pre>
            {tried.refusals.length > 0 && <span className="t-body-sm muted">{t("desktop.scenarios.refused", { what: tried.refusals.join("; ") })}</span>}
            {editable && (
              <div className="row">
                <span className="t-body-sm muted spacer">{t("desktop.scenarios.keep_hint")}</span>
                <Button variant="filled" icon={mdiContentSaveOutline} onClick={() => keep(tried)}>
                  {t("desktop.scenarios.keep")}
                </Button>
              </div>
            )}
          </div>
        )}
      </section>

      <section className="stack" style={{ gap: 6 }}>
        <div className="row" style={{ gap: 8 }}>
          <span className="t-title-sm spacer">{tn("desktop.scenarios.count", list.length)}</span>
          {editable && <IconButton icon={mdiPlus} label={t("desktop.scenarios.new")} small onClick={startNew} />}
          {running ? (
            <Button icon={mdiStop} disabled={!live?.run?.id} onClick={() => void stop()}>
              {t("desktop.evals.stop")}
            </Button>
          ) : (
            <Button variant="tonal" icon={mdiPlay} disabled={!list.some((s) => !s.problem)} onClick={runAll}>
              {t("desktop.scenarios.run_all")}
            </Button>
          )}
        </div>
        <span className="t-body-sm muted">{t("desktop.scenarios.hint")}</span>
        {list.length === 0 && <p className="t-body-sm muted">{t("desktop.scenarios.none")}</p>}
        <div className="list">
          {list.map((s) => {
            const last = live?.results.find((r) => r.case === s.name);
            return (
              <div key={s.name} className="list-item">
                <Icon path={last ? statusIcon[last.status] : mdiTestTube} className={last ? statusClass(last.status) : "muted"} />
                <span className="lines spacer">
                  <span className="ellipsis">
                    <span className="mono">{s.name}</span>
                    {s.description && ` · ${s.description}`}
                  </span>
                  <small className={`ellipsis ${s.problem ? "error-text" : ""}`}>{s.problem || [t(s.executes ? "desktop.scenarios.should_use" : "desktop.scenarios.should_not_use"), `≥ ${s.threshold}`, s.prompt.split("\n")[0]].join(" · ")}</small>
                </span>
                {editable && <IconButton icon={mdiPencilOutline} label={t("desktop.scenarios.edit", { name: s.name })} small onClick={() => edit(s)} />}
                {editable && <IconButton icon={mdiDeleteOutline} label={t("desktop.scenarios.delete", { name: s.name })} small onClick={() => setDeleting(s.name)} />}
              </div>
            );
          })}
        </div>
        {live && (
          <section className="stack" style={{ gap: 6 }}>
            <div className="row" style={{ gap: 8 }}>
              <Icon path={statusIcon[live.run?.status ?? "running"]} spin={running} className={statusClass(live.run?.status ?? "running")} />
              <span className="t-title-sm spacer">{running ? t("desktop.evals.running", { done: live.results.length, total: live.total }) : t(`desktop.evals.run_status.${live.run?.status ?? "stopped"}`)}</span>
              {live.results.length > 0 && <span className="t-body-sm muted">{t(running ? "desktop.evals.cost_so_far" : "desktop.evals.cost", { cost: money(resultsCost(live.results)) })}</span>}
            </div>
            {running && <progress className="eval-progress" max={live.total} value={live.results.length} aria-label={t("desktop.evals.progress")} />}
            <Results results={live.results} />
          </section>
        )}
      </section>

      {editing &&
        createPortal(
          <Dialog
            title={editing.previous ? t("desktop.scenarios.edit", { name: editing.previous }) : t("desktop.scenarios.keep_title")}
            icon={mdiTestTube}
            wide
            onClose={() => setEditing(null)}
            footer={
              <>
                <Button onClick={() => setEditing(null)}>{t("desktop.cancel")}</Button>
                <Button variant="filled" icon={mdiContentSaveOutline} onClick={() => void save()}>
                  {t("desktop.scenarios.save")}
                </Button>
              </>
            }
          >
            <div className="stack" style={{ gap: 12 }}>
              <div className="pair">
                <Field label={t("desktop.scenarios.name")} supporting={t("desktop.skills.name_hint")}>
                  {(id) => <input id={id} className="input mono" value={editing.form.name} onChange={(e) => setField("name", e.target.value)} />}
                </Field>
                <Field label={t("desktop.scenarios.description")}>{(id) => <input id={id} className="input" value={editing.form.description} onChange={(e) => setField("description", e.target.value)} />}</Field>
              </div>
              <Field label={t("desktop.scenarios.prompt")}>{(id) => <textarea id={id} className="input" rows={2} value={editing.form.prompt} onChange={(e) => setField("prompt", e.target.value)} />}</Field>
              <label className="row agent-switch">
                <Switch label={t("desktop.scenarios.executes")} checked={editing.form.executes} onChange={(v) => setField("executes", v)} />
                <span className="stack" style={{ gap: 0 }}>
                  <span className="t-label">{t("desktop.scenarios.executes")}</span>
                  <span className="t-body-sm muted">{t(editing.form.executes ? "desktop.scenarios.should_use_hint" : "desktop.scenarios.should_not_use_hint", { name: skill })}</span>
                </span>
              </label>
              <div className="pair">
                <Field label={t("desktop.scenarios.threshold")} supporting={t("desktop.scenarios.threshold_hint")}>
                  {(id) => <input id={id} className="input" type="number" min={0} max={1} step={0.05} value={editing.form.threshold} onChange={(e) => setField("threshold", Number(e.target.value))} />}
                </Field>
                {editing.form.executes && (
                  <Field label={t("desktop.scenarios.expected")} supporting={t("desktop.scenarios.expected_hint", { name: skill })}>
                    {(id) => <input id={id} className="input mono" value={editing.form.expected.join(", ")} onChange={(e) => setField("expected", e.target.value.split(/[\s,]+/).filter(Boolean))} />}
                  </Field>
                )}
              </div>
              <Field label={t("desktop.scenarios.outcome")} supporting={t("desktop.scenarios.outcome_hint")}>
                {(id) => <textarea id={id} className="input" rows={8} value={editing.form.outcome} onChange={(e) => setField("outcome", e.target.value)} />}
              </Field>
              {problems.length > 0 && (
                <div className="card error stack" role="alert" style={{ gap: 4 }}>
                  {problems.map((p) => (
                    <span key={p} className="t-body-sm">
                      {p}
                    </span>
                  ))}
                </div>
              )}
            </div>
          </Dialog>,
          document.body,
        )}
      {deleting &&
        createPortal(
          <Dialog
            title={t("desktop.scenarios.delete", { name: deleting })}
            icon={mdiDeleteOutline}
            onClose={() => setDeleting("")}
            footer={
              <>
                <Button onClick={() => setDeleting("")}>{t("desktop.cancel")}</Button>
                <Button variant="filled" danger onClick={() => void remove(deleting)}>
                  {t("desktop.skills.delete")}
                </Button>
              </>
            }
          >
            <p>{t("desktop.scenarios.delete_body", { name: deleting })}</p>
          </Dialog>,
          document.body,
        )}
    </div>
  );
}
