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

import { Code, ConnectError, type ServiceImpl } from "@connectrpc/connect";
import { parseMessages } from "../a2ui/model";
import { ActionKind } from "../gen/blitz/v1/turn_pb";
import { AgentScope, type WorkspaceService } from "../gen/blitz/v1/workspace_pb";
import { builtinLimits, checkAnswer, guardrailProblems, ruleProblems } from "./fakeChecks";
import { fakeAgentsOff, fakeMCP } from "./fakeConfigData";
import { backgroundRuns, fakeFlowFile, fakeFlows, fakeReceipts, fakeTasks, runStopped, summary, receiptsFor, watchFakeRun, watchFakeRuns, watchFakeTasks, type FakeFlow } from "./fakeFlows";
import { fakeAlertsState, fakeError, fakeInvalid, fakeOrg, notFound, sleep, state } from "./fakeHelpers";
import {
  agentFolder,
  en,
  fakeAgentFiles,
  fakeBuiltinGuards,
  fakeConnectorCalls,
  fakeConnectorCategory,
  fakeConnectorKinds,
  fakeConnectorMsg,
  fakeConnectors,
  fakeEvalFile,
  fakeEvalRuns,
  fakeEvals,
  fakeGuardFile,
  fakeGuards,
  fakeLiveEvals,
  fakeLiveFor,
  fakeOtherSkills,
  fakeProcessOutput,
  fakeProcesses,
  fakeProjectSkills,
  fakeRuleList,
  fakeRules,
  fakeSkillFile,
  fakeSkillInfo,
  followFakeEval,
  saveAgent,
  startFakeEval,
  watchFakeProcesses,
  type FakeEval,
} from "./fakeStudioData";
import { fakeTask } from "./fakeTurn";

type StudioMethods = Pick<
  ServiceImpl<typeof WorkspaceService>,
  | "listMCPServers"
  | "restartMCPServer"
  | "draftManifest"
  | "listSkills"
  | "getSkill"
  | "listHumanTasks"
  | "watchHumanTasks"
  | "watchProcesses"
  | "getProcessOutput"
  | "killProcess"
  | "listWorkflowRuns"
  | "getWorkflowRun"
  | "watchWorkflowRun"
  | "watchWorkflowRuns"
  | "stopWorkflowRun"
  | "answerHumanTask"
  | "listWorkflows"
  | "saveWorkflow"
  | "deleteWorkflow"
  | "listEvals"
  | "saveEval"
  | "deleteEval"
  | "listEvalRuns"
  | "draftEvalCases"
  | "runEval"
  | "watchEvalRun"
  | "stopEvalRun"
  | "listGuardrails"
  | "saveGuardrail"
  | "deleteGuardrail"
  | "tryGuardrail"
  | "listConnectors"
  | "saveConnector"
  | "deleteConnector"
  | "setConnectorEnabled"
  | "makeConnectorGlobal"
  | "testConnector"
  | "tryConnectorTool"
  | "readConnectorSchema"
  | "draftConnectorQuery"
  | "runConnectorQuery"
  | "listRules"
  | "saveRule"
  | "deleteRule"
  | "reorderRules"
  | "tryRule"
  | "getSkillFile"
  | "saveSkill"
  | "listSkillScenarios"
  | "saveSkillScenario"
  | "deleteSkillScenario"
  | "trySkill"
  | "deleteSkill"
  | "readSkillFile"
  | "writeSkillFile"
  | "deleteSkillFile"
  | "copySkill"
  | "runSkillScript"
  | "listAgents"
  | "setAgent"
  | "listAgentFiles"
  | "setAgentEnabled"
  | "saveAgentFile"
  | "listTools"
  | "deleteAgentFile"
  | "listTasks"
  | "getTask"
  | "watchTasks"
  | "stopTask"
>;

/** Studio, workflows, evals, rules, guardrails, connectors, skills, agents, tasks, and process RPC handlers for WorkspaceService. */
export function fakeWorkspaceStudioHandlers(): StudioMethods {
  return {
    listMCPServers: () => ({
      servers: [
        ...fakeMCP.map((m) => ({ name: m.name, target: m.url || [m.command, ...(m.args ?? [])].join(" "), autoApprove: !!m.autoApprove, disabled: !!m.disabled, state: m.disabled ? "disabled" : m.name === "github" ? "connected" : m.name === "jira" ? "failed" : "idle", tools: m.name === "github" ? 26 : 0, error: m.name === "jira" && !m.disabled ? "401 Unauthorized: sign in with blitz mcp login jira" : "" })),
        { name: "db", target: "npx @acme/db-mcp", state: "connected", tools: 4 },
      ],
    }),
    restartMCPServer: () => ({}),
    draftManifest: ({ name, current }) => ({
      text: `# ${name || "Reviewer"}\n\nYou review changes for correctness first, then clarity.\n\n## How you work\n- Read the diff and the files it touches.\n- List real bugs with the line and why.\n- Suggest the smallest fix.\n${current ? "\n(Improved from the instructions there.)" : ""}`,
      changes: ["Said what the agent is for and how it works", "Added a short checklist"],
    }),
    listSkills: ({ workspace, query }) => ({ skills: [...fakeOtherSkills, ...fakeProjectSkills.map((x) => fakeSkillInfo(workspace, x))].filter((x) => !query || x.name.includes(query)) }),
    getSkill: ({ name }) => ({ skill: { name, description: "Read and summarize PDF files", tools: [{ name: "read_file" }] } }),
    listHumanTasks: ({ workspace }) => ({ alerts: fakeAlertsState.value, tasks: fakeTasks.filter((x) => !workspace || x.workspace === workspace) }),
    watchHumanTasks: ({ workspace }, ctx) => watchFakeTasks(workspace, ctx.signal),
    watchProcesses: ({ workspace }, ctx) => watchFakeProcesses(workspace, ctx.signal),
    getProcessOutput: ({ workspace, id }) => ({ output: fakeProcessOutput, process: fakeProcesses.find((x) => x.workspace === workspace && x.process?.id === id)?.process }),
    killProcess: ({ workspace, id }) => {
      const i = fakeProcesses.findIndex((x) => x.workspace === workspace && x.process?.id === id);
      if (i < 0) throw fakeError(`no process ${id}`, "UNKNOWN_PROCESS");
      const [gone] = fakeProcesses.splice(i, 1);
      return { process: { ...gone.process, running: false, exitCode: -1 } };
    },
    listWorkflowRuns: ({ workspace, workflow, limit }) => ({ runs: receiptsFor(workspace).filter((r) => (!workspace || r.workspace === workspace) && (!workflow || r.workflow === workflow)).slice(0, limit || 50).map(summary) }),
    getWorkflowRun: ({ id }) => {
      const r = fakeReceipts.find((x) => x.id === id);
      if (!r) throw new ConnectError(`no workflow run ${id}`, Code.NotFound);
      return { run: r };
    },
    watchWorkflowRun: ({ id }, ctx) => watchFakeRun(id, ctx.signal),
    watchWorkflowRuns: ({ workspace }, ctx) => watchFakeRuns(workspace, ctx.signal),
    stopWorkflowRun: ({ id }) => {
      const r = fakeReceipts.find((x) => x.id === id && x.status === "running");
      if (!r) throw new ConnectError("the workflow run isn't running", Code.NotFound);
      runStopped.add(r.backgroundId);
      const bg = backgroundRuns().find((x) => x.id === r.backgroundId);
      if (bg) state(bg.workspace).pending.get(`${bg.id}-save`)?.("stop");
      return {};
    },
    answerHumanTask: ({ taskId, actionJson, dataJson }) => {
      const i = fakeTasks.findIndex((x) => x.id === taskId);
      if (i < 0) throw new ConnectError("no such task", Code.NotFound);
      const form = parseMessages(fakeTasks[i].a2uiJson).form;
      if (!form) throw fakeError("a timer isn't answered: its run goes on at its time", "NOT_ANSWERABLE");
      const r = checkAnswer(form, taskId, actionJson, dataJson);
      if ("problems" in r) throw fakeInvalid(`the answer isn't accepted: ${r.problems.join("; ")}`, "ANSWER_INVALID", { problems: r.problems.join("\n") });
      fakeTasks.splice(i, 1);
      return {};
    },
    listWorkflows: ({ workspace }) => ({ dir: `${workspace}/.agents/workflows`, workflows: fakeFlows.map((f) => fakeFlowFile(workspace, f)) }),
    saveWorkflow: ({ workspace, previousName, workflow }) => {
      const w = workflow!;
      if (!/^[a-z0-9]+([-_][a-z0-9]+)*$/.test(w.name)) return { problems: [`name "${w.name}": lowercase letters, digits and single hyphens, up to 64`] };
      const next: FakeFlow = {
        name: w.name,
        description: w.description,
        mode: w.mode,
        nodes: w.nodes.map((n) => ({ ...n, skills: [...n.skills], rules: [...n.rules], args: [...n.args], fields: n.fields.map((f) => ({ ...f })) })),
        edges: w.edges.map((e) => ({ from: e.from, to: e.to, when: e.when, if: e.if, default: e.default, error: e.error, label: e.label })),
      };
      const i = fakeFlows.findIndex((x) => x.name === (previousName || w.name));
      if (i >= 0) fakeFlows[i] = next;
      else fakeFlows.push(next);
      return { workflow: fakeFlowFile(workspace, next) };
    },
    deleteWorkflow: ({ name }) => {
      const i = fakeFlows.findIndex((x) => x.name === name);
      if (i < 0) throw new ConnectError(`no workflow ${name}`, Code.NotFound);
      fakeFlows.splice(i, 1);
      return {};
    },
    listEvals: ({ workspace }) => ({ dir: `${workspace}/.agents/evals`, evals: fakeEvals.map((e) => fakeEvalFile(workspace, e)) }),
    saveEval: ({ workspace, previousName, eval: spec }) => {
      const e = spec!;
      if (!/^[a-z0-9]+([-_][a-z0-9]+)*$/.test(e.name)) return { problems: [`name "${e.name}": lowercase letters, digits and single hyphens, up to 64`] };
      const blank = e.cases.find((c) => !c.prompt.trim());
      if (blank) return { problems: [`case ${blank.id}: it needs a prompt`] };
      const next: FakeEval = { name: e.name, description: e.description, agents: e.agents, skills: e.skills, near: e.near || "judge", allow: e.allow, cases: e.cases.map((c) => ({ ...c, threshold: c.type === "near" ? c.threshold || 0.8 : 0 })) };
      const i = fakeEvals.findIndex((x) => x.name === (previousName || e.name));
      if (i >= 0) fakeEvals[i] = next;
      else fakeEvals.push(next);
      return { eval: fakeEvalFile(workspace, next) };
    },
    deleteEval: ({ name }) => {
      const i = fakeEvals.findIndex((x) => x.name === name);
      if (i < 0) throw new ConnectError(`no eval ${name}`, Code.NotFound);
      fakeEvals.splice(i, 1);
      return {};
    },
    listEvalRuns: ({ name }) => ({ runs: [...fakeLiveFor(name), ...(fakeEvalRuns[name] ?? []).slice().reverse()] }),
    draftEvalCases: async ({ agent, existing }) => {
      await new Promise((r) => setTimeout(r, 900));
      const taken = new Set(existing.map((c) => c.id));
      const id = (s: string) => (taken.has(s) ? `${s}-2` : s);
      return {
        cases: [
          { id: id("plan-steps"), name: "Plans in steps", prompt: "Plan adding a wishlist to the shop.", output: "A numbered plan: data model, API, page, tests, rollout.", type: "near", threshold: 0.75, scope: "fail" },
          { id: id("asks-first"), name: "Asks when unclear", prompt: "Make it faster.", output: "Asks what should be faster before planning.", type: "near", threshold: 0.8, scope: "fail" },
          { id: id("no-code"), name: "Doesn't write code", prompt: "Write the wishlist API.", output: "Plans it, and leaves the code to a coding agent.", type: "near", threshold: 0.7, scope: "warn" },
          { id: id("format"), name: "", prompt: `Reply with only the word READY if you're ${agent}.`, output: "READY", type: "exact", threshold: 0, scope: "info" },
        ],
      };
    },
    runEval: async function* ({ name, background }, ctx) {
      const sk = name.startsWith("skill:") ? fakeProjectSkills.find((k) => k.name === name.slice(6)) : undefined;
      const e = sk ? ({ name, description: "", agents: [], skills: [], near: "judge", allow: [], cases: (sk.scenarios ?? []).map((sc) => ({ id: sc.name, name: sc.description, prompt: sc.prompt, output: sc.outcome, type: "near", threshold: sc.threshold, scope: "fail" })) } as FakeEval) : fakeEvals.find((x) => x.name === name);
      if (!e) throw new ConnectError(`no eval ${name}`, Code.NotFound);
      const live = startFakeEval(e);
      if (!background) ctx.signal.addEventListener("abort", () => (live.stopped = true), { once: true });
      yield* followFakeEval(live, ctx.signal);
    },
    watchEvalRun: async function* ({ id }, ctx) {
      const live = fakeLiveEvals.get(id);
      if (live) return yield* followFakeEval(live, ctx.signal);
      const run = Object.values(fakeEvalRuns).flat().find((r) => r.id === id);
      if (!run) throw new ConnectError(`no eval run ${id}`, Code.NotFound);
      yield { event: { case: "finished" as const, value: run } };
    },
    stopEvalRun: ({ id }) => {
      const live = fakeLiveEvals.get(id);
      if (!live) throw new ConnectError(`no eval run ${id}`, Code.NotFound);
      live.stopped = true;
      return {};
    },
    listGuardrails: ({ workspace }) => ({
      dir: `${workspace}/.agents/guardrails`,
      builtins: fakeBuiltinGuards.map((b) => {
        const g = fakeGuards.find((x) => x.builtin === b.id);
        return { ...b, file: g ? fakeGuardFile(workspace, g) : { name: b.id, builtin: b.id, stage: b.stage, action: b.defaultAction, agents: [], tools: [], description: "", pii: false, judge: false, text: "", off: false, path: "", problem: "", editable: true, limits: b.id === "limits" ? builtinLimits() : undefined } };
      }),
      guardrails: fakeGuards.filter((g) => !g.builtin).map((g) => fakeGuardFile(workspace, g)),
    }),
    saveGuardrail: ({ workspace, previousName, guardrail }) => {
      const g = guardrail!;
      const name = (g.builtin || g.name).trim();
      const problems = guardrailProblems({ ...g, name });
      if (problems.length) return { problems };
      const previous = g.builtin ? (fakeGuards.some((x) => x.name === g.builtin) ? g.builtin : "") : previousName;
      if (previous !== name && fakeGuards.some((x) => x.name === name)) return { problems: [`name ${JSON.stringify(name)}: there's a guardrail by that name`] };
      const b = fakeBuiltinGuards.find((x) => x.id === g.builtin);
      const next = { name, builtin: g.builtin, stage: b ? b.stage : g.stage, action: g.action || b?.defaultAction || "block", agents: g.agents, tools: g.tools, description: g.description, pii: g.pii, judge: g.judge, limits: g.limits, text: g.text, off: g.off };
      const i = fakeGuards.findIndex((x) => x.name === (previous || name));
      if (i >= 0) fakeGuards[i] = next;
      else fakeGuards.push(next);
      return { guardrail: fakeGuardFile(workspace, next) };
    },
    deleteGuardrail: ({ name }) => {
      const i = fakeGuards.findIndex((x) => x.name === name);
      if (i < 0) throw new ConnectError(`no guardrail ${name}`, Code.NotFound);
      fakeGuards.splice(i, 1);
      return {};
    },
    tryGuardrail: async ({ guardrail, sample }) => {
      await new Promise((r) => setTimeout(r, 600));
      if (guardrail?.builtin === "secrets") return /@|AKIA/.test(sample) ? { pass: false, reason: `It would be shown as: ${sample.replace(/\S+@\S+/g, "[EMAIL REDACTED]")}` } : { pass: true, reason: "No secrets or personal data found." };
      if (guardrail?.builtin === "prompt_injection") return /ignore|you are now/i.test(sample) ? { pass: false, reason: "it contains text aimed at the agent" } : { pass: true, reason: "Nothing in it looks aimed at the agent." };
      return /delete|-D|drop/i.test(sample) ? { pass: false, reason: "It deletes a branch." } : { pass: true, reason: "Nothing here is deleted." };
    },
    listConnectors: ({ workspace }) => ({
      categories: [
        {
          ...fakeConnectorCategory,
          workspaceDir: `${workspace}/.agents/connectors/databases`,
          state: "connected",
          toolCount: fakeConnectors.filter((d) => d.enabled !== false).reduce((n, d) => n + d.tools.length + d.queries.length, 0),
        },
      ],
      kinds: [...fakeConnectorKinds, { id: "mysql", category: "databases", name: en("MySQL"), backend: "toolbox", origin: "workspace", path: `${workspace}/.agents/connectors/kinds/mysql.yaml`, problem: 'source "mysql": not in Blitz\'s Toolbox' }],
      connections: [
        ...fakeConnectors.map((d) => fakeConnectorMsg(workspace, d)),
        ...(fakeOrg.signedIn
          ? [{ ...fakeConnectorMsg(workspace, { ...fakeConnectors[0], name: "warehouse", description: "Acme's sales warehouse" }), scope: "org-locked", trusted: true, path: "~/.blitz/enterprise/blitz.acme.com/catalog/locked/connectors/databases/warehouse.yaml" }]
          : []),
      ],
      workspaceDir: `${workspace}/.agents/connectors`,
      globalDir: "~/.blitz/connectors",
    }),
    saveConnector: ({ workspace, connection, previousName, secretValues }) => {
      if (connection!.scope === "global") throw new ConnectError("a global connection is changed in Settings, not in a workspace", Code.FailedPrecondition);
      return fakeConnectorCalls.save(workspace, { ...connection!, scope: "workspace" }, previousName, secretValues);
    },
    deleteConnector: ({ category, scope, name }) => {
      if (scope === "global") throw new ConnectError("a global connection is changed in Settings, not in a workspace", Code.FailedPrecondition);
      const i = fakeConnectors.findIndex((d) => d.name === name && d.scope === scope && d.category === category);
      if (i < 0) throw new ConnectError(`no connection ${name}`, Code.NotFound);
      fakeConnectors.splice(i, 1);
      return {};
    },
    setConnectorEnabled: ({ category, scope, name, enabled }) => {
      const d = fakeConnectors.find((d) => d.name === name && d.scope === scope && d.category === category);
      if (!d) throw new ConnectError(`no connection ${name}`, Code.NotFound);
      d.enabled = enabled;
      return {};
    },
    makeConnectorGlobal: ({ category, name }) => {
      const d = fakeConnectors.find((d) => d.name === name && d.scope === "workspace" && d.category === category);
      if (!d) throw new ConnectError(`no connection ${name}`, Code.NotFound);
      if (!d.trusted) throw new ConnectError("save and trust it first", Code.FailedPrecondition);
      if (fakeConnectors.some((x) => x.name === name && x.scope === "global" && x.category === category)) return { problems: [`there's a global connection named "${name}" already`] };
      d.scope = "global";
      return { connection: fakeConnectorMsg("", d) };
    },
    testConnector: ({ connection }) => fakeConnectorCalls.test(connection!),
    tryConnectorTool: ({ connection, tool }) => fakeConnectorCalls.tryTool(connection!, tool),
    readConnectorSchema: ({ connection }) => fakeConnectorCalls.readSchema(connection!),
    draftConnectorQuery: ({ request }) => fakeConnectorCalls.draft(request),
    runConnectorQuery: ({ statement, readOnly, maxRows }) => {
      if (readOnly && /^\s*(insert|update|delete|drop|alter|create)\b/i.test(statement)) return { problem: "the connection is read-only: this statement writes" };
      const rows = [
        [1042, "Ada Lovelace", "128.50", "2026-11-06"],
        [1043, "Grace Hopper", "64.00", "2026-11-06"],
        [1044, null, "12.25", "2026-11-07"],
      ];
      const shown = rows.slice(0, maxRows || rows.length);
      return {
        table: {
          columns: [
            { name: "id", description: "The order's number", type: "integer", dbType: "int4" },
            { name: "customer", description: "Who placed it", type: "string", length: 80n, dbType: "varchar", nullable: true },
            { name: "total", description: "", type: "number", dbType: "numeric" },
            { name: "placed", description: "", type: "date", dbType: "date" },
          ],
          rowsJson: JSON.stringify(shown),
          total: shown.length,
          truncated: shown.length < rows.length,
          markdown: "",
        },
        problem: "",
      };
    },
    listRules: ({ workspace }) => ({ dir: `${workspace}/.agents/rules`, rules: fakeRuleList(workspace) }),
    saveRule: ({ workspace, previousName, rule }) => {
      const r = rule!;
      const problems = ruleProblems(r);
      if (problems.length) return { problems };
      const i = fakeRules.findIndex((x) => x.name === previousName);
      const next = { name: r.name, when: r.when, action: r.action, order: r.order, agents: r.agents, description: r.description, paths: r.paths, text: r.text, off: r.off };
      if (i >= 0) fakeRules[i] = next;
      else fakeRules.push(next);
      return { rule: fakeRuleList(workspace).find((x) => x.name === r.name) };
    },
    deleteRule: ({ name }) => {
      const i = fakeRules.findIndex((x) => x.name === name);
      if (i < 0) throw new ConnectError(`no rule ${name}`, Code.NotFound);
      fakeRules.splice(i, 1);
      return {};
    },
    reorderRules: ({ names }) => {
      names.forEach((n, i) => {
        const r = fakeRules.find((x) => x.name === n);
        if (r) r.order = (i + 1) * 10;
      });
      return {};
    },
    tryRule: async ({ sample }) => (await new Promise((r) => setTimeout(r, 700)), /test/i.test(sample) ? { pass: true, reason: "It names the tests it ran." } : { pass: false, reason: "It doesn't say how the change was tested." }),
    getSkillFile: ({ workspace, name }) => {
      const x = fakeProjectSkills.find((k) => k.name === name);
      if (x) return { file: fakeSkillFile(workspace, x) };
      const o = fakeOtherSkills.find((k) => k.name === name);
      if (!o) throw new ConnectError(`no skill ${name}`, Code.NotFound);
      return { file: { skill: o, path: `${o.dir}/SKILL.md`, spec: { name: o.name, description: o.description, instructions: "Read the file, then summarize it." } } };
    },
    saveSkill: ({ workspace, previousName, skill }) => {
      const sp = skill!;
      if (!/^[a-z0-9]+(-[a-z0-9]+)*$/.test(sp.name)) return { problems: [`name "${sp.name}": lowercase letters, digits and single hyphens, up to 64`] };
      if (!sp.description.trim()) return { problems: ["description: say what the skill is for and when to use it"] };
      if (sp.name !== previousName && fakeProjectSkills.some((k) => k.name === sp.name)) return { problems: [`name "${sp.name}": the workspace has a skill by that name`] };
      let x = fakeProjectSkills.find((k) => k.name === previousName);
      if (!x) fakeProjectSkills.push((x = { name: sp.name, description: "", tools: "", agents: [], members: [], instructions: "", files: {} }));
      Object.assign(x, { name: sp.name, description: sp.description, tools: sp.tools, agents: sp.agents, members: sp.members, instructions: sp.instructions, triggers: sp.triggers });
      return { file: fakeSkillFile(workspace, x) };
    },
    listSkillScenarios: ({ workspace, skill }) => ({ scenarios: (fakeProjectSkills.find((k) => k.name === skill)?.scenarios ?? []).map((sc) => ({ ...sc, path: `${workspace}/.agents/skills/${skill}/scenarios/${sc.name}.md`, problem: "" })) }),
    saveSkillScenario: ({ workspace, skill, previousName, scenario }) => {
      const x = fakeProjectSkills.find((k) => k.name === skill);
      if (!x) throw new ConnectError(`no skill ${skill}`, Code.NotFound);
      const sc = scenario!;
      const probs = [!/^[a-z0-9]+(-[a-z0-9]+)*$/.test(sc.name) && `name "${sc.name}": lowercase letters, digits and single hyphens, up to 64`, !sc.prompt.trim() && "prompt: what to ask the agent", !sc.outcome.trim() && "outcome: the answer expected"].filter(Boolean) as string[];
      if (probs.length) return { problems: probs };
      const list = (x.scenarios ??= []);
      const next = { name: sc.name, description: sc.description, prompt: sc.prompt, executes: sc.executes, expectedSkills: sc.expectedSkills, threshold: sc.threshold, outcome: sc.outcome };
      const i = list.findIndex((k) => k.name === (previousName || sc.name));
      if (i >= 0) list[i] = next;
      else list.push(next);
      list.sort((a, b) => a.name.localeCompare(b.name));
      return { scenario: { ...next, path: `${workspace}/.agents/skills/${skill}/scenarios/${sc.name}.md`, problem: "" } };
    },
    deleteSkillScenario: ({ skill, name }) => {
      const x = fakeProjectSkills.find((k) => k.name === skill);
      const i = x?.scenarios?.findIndex((k) => k.name === name) ?? -1;
      if (!x || i < 0) throw new ConnectError(`no scenario ${name}`, Code.NotFound);
      x.scenarios!.splice(i, 1);
      return {};
    },
    trySkill: async ({ skill, agent, prompt, force }) => {
      await new Promise((r) => setTimeout(r, 900));
      const x = fakeProjectSkills.find((k) => k.name === skill);
      const words = prompt.toLowerCase().split(/\W+/);
      const near = (x?.triggers ?? []).some((p) => p.toLowerCase().split(/\W+/).filter((w) => w.length > 3).filter((w) => words.some((v) => v.startsWith(w.slice(0, 5)))).length >= 2);
      const triggered = force || near;
      return { agent: agent || x?.agents[0] || "blitz", output: triggered ? "## What's new in v2.4\n- Wishlists you can share with a link.\n- Checkout is a step shorter." : "I can help with that: what would you like to know?", skillsUsed: triggered ? [skill] : [], triggered, forced: force, refusals: [], costUsd: 0.0024, duration: { seconds: 3n, nanos: 0 } };
    },
    deleteSkill: ({ name }) => {
      const i = fakeProjectSkills.findIndex((k) => k.name === name);
      if (i < 0) throw new ConnectError(`no skill ${name}`, Code.NotFound);
      fakeProjectSkills.splice(i, 1);
      return {};
    },
    readSkillFile: ({ name, path }) => ({ content: new TextEncoder().encode(fakeProjectSkills.find((k) => k.name === name)?.files[path] ?? "") }),
    writeSkillFile: ({ workspace, name, path, content }) => {
      const x = fakeProjectSkills.find((k) => k.name === name)!;
      if (!/^((references|scripts)\/[^.]|(requirements\.txt|pyproject\.toml|package\.json)$)/.test(path) || path.includes("..")) throw new ConnectError(`"${path}": a skill's files go in references/ or scripts/`, Code.InvalidArgument);
      x.files[path] = new TextDecoder().decode(content);
      return { file: fakeSkillFile(workspace, x) };
    },
    deleteSkillFile: ({ workspace, name, path }) => {
      const x = fakeProjectSkills.find((k) => k.name === name)!;
      delete x.files[path];
      return { file: fakeSkillFile(workspace, x) };
    },
    copySkill: ({ workspace, from, name }) => {
      if (fakeProjectSkills.some((k) => k.name === name)) return { problems: [`name "${name}": the workspace has a skill by that name`] };
      const o = [...fakeOtherSkills, ...fakeProjectSkills].find((k) => k.name === from)!;
      const x = { name, description: o.description, tools: "", agents: [], members: [], instructions: "Read the file, then summarize it.", files: {} };
      fakeProjectSkills.push(x);
      return { file: fakeSkillFile(workspace, x) };
    },
    runSkillScript: async ({ script, args }) => (await new Promise((r) => setTimeout(r, 900)), { exitCode: 0, stdout: `${script}: ${args.length ? args.join(" ") : "no arguments"}\n3 entries since v0.7.0\n`, stderr: "", sandbox: "seatbelt" }),
    listAgents: ({ workspace }) => ({
      agents: [
        { name: "blitz", displayName: "Blitz", description: "General agent: does the work and hands code changes to the coder", active: state(workspace).settings.agent === "blitz" },
        { name: "qa", displayName: "QA", description: "Testing specialist", active: state(workspace).settings.agent === "qa", pinnedModel: "anthropic/claude-sonnet-5" },
        { name: "planning-agent", displayName: "Planner", description: "Plans before changing", active: false },
      ],
    }),
    setAgent: ({ workspace, name }) => {
      state(workspace).settings.agent = name;
      return {};
    },
    listAgentFiles: ({ workspace, scope }) => {
      const dir = agentFolder(workspace, scope);
      const off = workspace && scope === AgentScope.USER ? (fakeAgentsOff.get(workspace) ?? new Set<string>()) : new Set<string>();
      const files = [...fakeAgentFiles.get(dir)!.values()].map((f) => ({ ...f, offHere: off.has(f.agent?.name ?? "") }));
      return { dir, files: files.sort((a, b) => (a.path ?? "").localeCompare(b.path ?? "")) };
    },
    setAgentEnabled: ({ workspace, name, enabled }) => {
      const off = fakeAgentsOff.get(workspace) ?? new Set<string>();
      if (enabled) off.delete(name);
      else off.add(name);
      fakeAgentsOff.set(workspace, off);
      return {};
    },
    saveAgentFile: ({ workspace, scope, previousName, agent }) => saveAgent(workspace, scope, previousName, agent!),
    listTools: ({ workspace, all }) => {
      const mine = ["list_files", "glob", "read_file", "view_image", "grep", "create_file", "edit", "replace_in_file", "delete_snippet", "run_shell_command", "manage_background_process", "ask_user_question", "invoke_agent", "list_agents", "todo_write"];
      const more = ["web_fetch", "web_search", "notebook_edit", "browser_navigate", "browser_screenshot", "lsp_definition", "lsp_references"];
      return {
        agent: all ? "" : state(workspace).settings.agent,
        tools: (all ? [...mine, ...more] : mine).map((name) => ({ name, description: name.replace(/_/g, " "), planAllowed: !/create|edit|replace|delete|shell|notebook/.test(name) })),
      };
    },
    deleteAgentFile: ({ workspace, scope, name, path: byPath }) => {
      const files = fakeAgentFiles.get(agentFolder(workspace, scope))!;
      const path = byPath ? (files.has(byPath) ? byPath : undefined) : [...files].find(([, f]) => f.agent?.name === name)?.[0];
      if (!path) throw new ConnectError(`no agent ${name || byPath}`, Code.NotFound);
      files.delete(path);
      return {};
    },
    listTasks: ({ workspace, sessionIds }) => ({
      tasks: [...state(workspace).tasks.keys()].map((id) => fakeTask(state(workspace), id)).filter((t) => sessionIds.includes(t.sessionId)),
    }),
    getTask: ({ workspace, id }) => {
      if (!state(workspace).tasks.has(id)) notFound(id);
      const t = fakeTask(state(workspace), id);
      const events = ['→ read_file {"path":"internal/cart/cart.go"}', "← read_file", '→ grep {"pattern":"math.Round"}', "← grep"];
      return { task: t, events: t.state === "running" ? events.slice(0, 2) : events };
    },
    watchTasks: async function* ({ workspace, sessionIds }, ctx) {
      yield { event: { kind: { case: "ready", value: true } } };
      const asked = new Set<string>();
      const ended = new Set<string>();
      while (!ctx.signal.aborted) {
        await sleep(500);
        const s = state(workspace);
        for (const id of s.tasks.keys()) {
          const t = fakeTask(s, id);
          if (!sessionIds.includes(t.sessionId)) continue;
          if (t.state === "waiting" && !asked.has(id)) {
            asked.add(id);
            yield {
              event: {
                kind: {
                  case: "approvalRequest",
                  value: { requestId: `${id}-req`, tool: "run_shell_command", kind: ActionKind.COMMAND, detail: "go test ./internal/cart/...", scopeLabel: "this exact command in this workspace", agent: t.agent, taskId: id },
                },
              },
            };
          }
          if (asked.has(id) && t.state !== "waiting" && !ended.has(`${id}-req`)) {
            ended.add(`${id}-req`);
            yield { event: { kind: { case: "resolved", value: `${id}-req` } } };
          }
          if (["done", "stopped"].includes(t.state) && !ended.has(id)) {
            ended.add(id);
            yield { event: { kind: { case: "task", value: t } } };
          }
        }
      }
    },
    stopTask: ({ workspace, id }) => {
      const t = state(workspace).tasks.get(id);
      if (!t) notFound(id);
      t.stopped ??= Date.now();
      return { task: fakeTask(state(workspace), id) };
    },
  };
}
