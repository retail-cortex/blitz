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

import { createRouterTransport } from "@connectrpc/connect";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setTransport } from "./api";
import { ConfigService } from "./gen/blitz/v1/config_pb";
import { FileService } from "./gen/blitz/v1/file_pb";
import { SessionService } from "./gen/blitz/v1/session_pb";
import { WorkspaceService } from "./gen/blitz/v1/workspace_pb";
import { stopAllLoops } from "./loops";
import { builtin, builtinCommands, builtinsInOrder, commandGroups, exportName, goalPrompt, type CommandContext } from "./slashCommands";

vi.mock("./clipboard", () => ({ copyText: vi.fn(async () => {}) }));
vi.mock("./events", () => ({ showLicense: vi.fn(), openDocs: vi.fn() }));

// What the fake service was asked, as "method arg…".
let calls: string[] = [];
const rec = (s: string) => calls.push(s);

beforeEach(() => {
  calls = [];
  setTransport(
    createRouterTransport(({ service }) => {
      service(SessionService, {
        getUsage: () => ({ usage: { calls: 2, input: 10n, cached: 1n, output: 5n, priced: true, costUsd: 0.01, lastPrompt: 9n }, autoCompact: true, threshold: 100 }),
        exportSession: () => ({ markdown: "# chat" }),
        forkSession: (r) => (rec(`fork ${r.turn}`), { session: { id: "s2", messageCount: 4 } }),
        listRewindPoints: () => ({ points: [{ index: 0, text: "first" }, { index: 3, text: "second\nmore" }] }),
        getGoal: () => ({ goal: { condition: "tests pass", continues: 1, max: 5 } }),
        setGoal: (r) => (rec(`setGoal ${r.condition}`), { goal: { condition: r.condition, max: 5 } }),
        clearGoal: () => (rec("clearGoal"), {}),
        compact: (r) => (rec(`compact ${r.focus}`), { eventsCompacted: 3, summaryChars: 40 }),
        saveSnapshot: (r) => (rec(`snapshot ${r.name} ${r.force}`), { snapshot: { snapshot: r.name } }),
        searchSession: () => ({ found: 0 }),
      });
      service(WorkspaceService, {
        getModel: () => ({ provider: "gemini", name: "gemini-4-argon" }),
        getGitStatus: () => ({ repo: true, branch: "main", changed: 2 }),
        getSandbox: () => ({ summary: ["Shell: sandboxed", "Network: off"] }),
        listMCPServers: () => ({ servers: [{ name: "github", target: "npx gh", autoApprove: true }] }),
        getServiceInfo: () => ({ version: "0.5.3" }),
        listAgents: () => ({ agents: [{ name: "blitz", displayName: "Blitz", description: "main", active: true, pinnedModel: "" }, { name: "qa", displayName: "QA", description: "tests", pinnedModel: "anthropic/claude-haiku-4-5" }] }),
        setAgent: (r) => (rec(`setAgent ${r.name}`), { agent: { displayName: "QA" } }),
        setModel: (r) => (rec(`setModel ${r.ref}`), {}),
        setSetting: (r) => (rec(`set ${r.key}=${r.value}`), { key: r.key }),
        setPermissionMode: (r) => (rec(`mode ${r.mode}`), { mode: r.mode }),
        getModelSettings: (r) => (r.ref ? { model: { model: r.ref, settings: { temperature: 0.2 } } } : { all: { "gpt-5": { seed: 7 } } }),
        updateModelSettings: (r) => (rec(`msettings ${r.ref} ${r.reset} ${r.changes.map((c) => `${c.key}=${c.value}`).join(",")}`), { model: { model: r.ref, provider: "openai" }, unsupported: ["seed"] }),
        pinModel: (r) => (rec(`pin ${r.agent} ${r.ref}`), { agent: r.agent, model: r.ref, saved: { path: "/x/.env.toml" } }),
        unpinModel: (r) => (rec(`unpin ${r.agent}`), { agent: r.agent, model: "gemini-4-argon" }),
        listStyles: () => ({ styles: [{ name: "default", description: "plain", active: true }] }),
        listTasks: () => ({ tasks: [{ id: "t1", agent: "qa", state: "running", prompt: "check" }] }),
        getTask: (r) => ({ task: { id: r.id, agent: "qa", state: "done", result: "all good" }, events: [] }),
        stopTask: (r) => (rec(`stopTask ${r.id} ${r.sessionIds.join(",")}`), { task: { state: "stopped" } }),
        reloadMemory: () => ({ paths: ["/w/AGENTS.md"], files: ["AGENTS.md"] }),
        addMemory: (r) => (rec(`addMemory ${r.text}`), { path: "/w/BLITZ.md" }),
        listNotes: () => ({ notes: [{ name: "style", kind: "feedback", text: "short answers" }] }),
        forgetNote: (r) => (rec(`forget ${r.name}`), {}),
        getProjectSettings: () => ({ settings: { files: [".blitz/settings.toml"], state: "trusted" } }),
        listApprovals: () => ({ approvals: [{ key: "k1", kind: "shell", subject: "go test", always: false }] }),
        revokeApprovals: (r) => (rec(`revoke ${r.keys.join(",")} ${r.all}`), { revoked: r.all ? 3 : 1 }),
        listHooks: () => ({ hooks: [{ event: "PreToolUse", type: "command", match: "shell", runs: "./check.sh", source: "your settings", failures: [] }] }),
        listPermissionRules: () => ({ rules: [{ effect: "allow", rule: "shell(go test *)", source: "project" }] }),
        addPermissionRule: (r) => (rec(`allow? ${r.effect} ${r.rule} ${r.save}`), { rule: r.rule }),
        removePermissionRule: (r) => (rec(`remove ${r.rule}`), { rule: r.rule, removed: 1 }),
        listEnvs: () => ({ envs: [{ key: "abc", deps: ["numpy"], skills: ["data"], size: 2048n, ready: true }] }),
        pruneEnvs: () => (rec("prune"), { removed: 1, freed: 1048576n, failed: [] }),
        removeEnv: (r) => (rec(`removeEnv ${r.key}`), {}),
        listSkills: (r) => ({ skills: [{ name: "pdf", description: "reads pdf files" }].filter((s) => !r.query || s.name.includes(r.query)) }),
        getSkill: (r) => ({ skill: r.name === "pdf" ? { name: "pdf", description: "reads pdf files", tools: [{ name: "read_file" }] } : undefined }),
        listTools: () => ({ agent: "blitz", tools: [{ name: "read_file", description: "Reads a file", planAllowed: true }] }),
        setLocale: (r) => (rec(`locale ${r.input}`), { tag: "es", nativeName: "Español" }),
        listCheckpoints: () => ({ checkpoints: [] }),
        undo: () => ({ restored: [] }),
      });
      service(FileService, { writeFile: (r) => (rec(`write ${r.path} ${r.text}`), { version: "1" }) });
      service(ConfigService, {
        setConfigValue: (r) => (rec(`config ${r.key}=${r.value}`), { change: { path: "/u/.env.toml" } }),
        listSkillRegistries: () => ({ registries: [{ name: "castor", url: "https://c.example/index.json" }] }),
        searchRegistrySkills: () => ({ skills: [{ registry: "castor", name: "lit-review", version: "1.2.0", description: "reviews literature" }], problems: ["registry other: HTTP 500"] }),
        installRegistrySkill: (r) => (rec(`install ${r.ref} ${r.workspace}`), { skill: { name: "lit-review", version: "1.2.0", registry: "castor" } }),
      });
    }),
  );
});
afterEach(() => stopAllLoops());

// A conversation that records what a command said, ran and did.
function context(line: string, over: Partial<CommandContext> = {}) {
  const said: string[] = [];
  const ran: string[] = [];
  const did: string[] = [];
  const [, name, args = ""] = /^\/(\S+)\s*([\s\S]*)$/.exec(line)!;
  const c: CommandContext = {
    dir: "/w",
    args: args.trim(),
    line,
    sessionId: "s1",
    sessionTitle: "Fix the Cart!",
    projectWaiting: 0,
    settings: { agent: "blitz", permissionMode: "default", effort: "", locale: "en-US" } as CommandContext["settings"],
    say: (text, tone = "info") => said.push(`${tone}: ${text}`),
    run: (text, opts) => ran.push(`${text}${opts ? " " + JSON.stringify(opts) : ""}`),
    send: (text) => did.push(`send ${text}`),
    snack: (text) => did.push(`snack ${text}`),
    usage: () => said.push("error: usage"),
    busy: () => false,
    settingsChanged: () => did.push("settingsChanged"),
    openView: (v) => did.push(`view ${v}`),
    openSettings: (section, scope) => did.push(`settings ${section} ${scope}`),
    reload: async () => void did.push("reload"),
    refreshTotal: () => did.push("refreshTotal"),
    refreshList: () => did.push("refreshList"),
    newSession: async () => void did.push("newSession"),
    rename: async (title) => void did.push(`rename ${title}`),
    rewind: async (i, mode, force) => void did.push(`rewind ${i} ${mode} ${force}`),
    addImagePath: (p) => did.push(`attach ${p}`),
    clearAttachments: () => 2,
    answers: () => ["first answer", "last answer"],
    theme: () => "system",
    setTheme: (theme) => did.push(`theme ${theme}`),
    help: () => "HELP",
    ...over,
  };
  return { c, said, ran, did, name };
}

async function exec(line: string, over: Partial<CommandContext> = {}) {
  const x = context(line, over);
  const b = builtin(x.name);
  expect(b, `no built-in /${x.name}`).toBeDefined();
  await b!.run(x.c);
  return { said: x.said.join("\n"), ran: x.ran, did: x.did, calls };
}

describe("the registry", () => {
  it("has each name once, every one in a group, in /help's order", () => {
    const names = builtinCommands.map((b) => b.name);
    expect(new Set(names).size).toBe(names.length);
    const order = builtinsInOrder();
    for (let i = 1; i < order.length; i++) {
      const [a, b] = [order[i - 1], order[i]];
      const ga = commandGroups.indexOf(a.group), gb = commandGroups.indexOf(b.group);
      expect(ga < gb || (ga === gb && a.name < b.name), `${a.name} before ${b.name}`).toBe(true);
    }
  });
  it("names exports as the terminal does", () => {
    expect(exportName("Fix the Cart!", "s1")).toBe("fix-the-cart.md");
    expect(exportName("", "s1")).toBe("session-s1.md");
    expect(exportName("a".repeat(60), "x")).toBe(`${"a".repeat(50)}.md`);
  });
});

describe("commands", () => {
  it.each([
    // [line, what it says (a part), the service calls, what the window did, the turns it ran]
    ["/status", "gemini/gemini-4-argon", [], [], []],
    ["/copy", "", [], ["snack Copied."], []],
    ["/copy 3", "error: usage", [], [], []],
    ["/export", "Wrote the session to fix-the-cart.md", ["write fix-the-cart.md # chat"], [], []],
    ["/export /w/notes/chat.md", "notes/chat.md", ["write notes/chat.md # chat"], [], []],
    ["/export /tmp/x.md", "error:", [], [], []],
    ["/fork 2", "s2", ["fork 2"], ["reload", "refreshList"], []],
    ["/fork x", "error:", [], [], []],
    ["/rewind", "second", [], [], []],
    ["/rewind 1 code --force", "", [], ["rewind 3 code true"], []],
    ["/rewind 9", "error:", [], [], []],
    ["/agents", "QA", [], [], []],
    ["/agents update auth", "", [], [], ['/agents update auth {"command":true}']],
    ["/model_settings", "seed=7", [], [], []],
    ["/model_settings gpt-5", "temperature=0.2", [], [], []],
    ["/model_settings gpt-5 temperature=0.3", "seed", ["msettings gpt-5 false temperature=0.3"], ["settingsChanged"], []],
    ["/model_settings gpt-5 reset", "global", ["msettings gpt-5 true "], ["settingsChanged"], []],
    ["/model_settings gpt-5 nope", "error:", [], [], []],
    ["/pin_model", "anthropic/claude-haiku-4-5", [], [], []],
    ["/pin_model qa openai/gpt-5", "/x/.env.toml", ["pin qa openai/gpt-5"], ["settingsChanged"], []],
    ["/pin_model qa", "error:", [], [], []],
    ["/unpin qa", "qa", ["unpin qa"], ["settingsChanged"], []],
    ["/style", "default", [], [], []],
    ["/style terse", "terse", ["set style=terse"], ["settingsChanged"], []],
    ["/goal", "tests pass", [], [], []],
    ["/goal clear", "Goal cleared", ["clearGoal"], [], []],
    ["/goal the tests pass", "Goal set", ["setGoal the tests pass"], [], [`/goal the tests pass ${JSON.stringify({ prompt: goalPrompt("the tests pass"), shown: "/goal the tests pass" })}`]],
    ["/loop", "No loops", [], [], []],
    ["/loop 30s hi", "error:", [], [], []],
    ["/loop stop 4", "error:", [], [], []],
    ["/tasks", "t1", [], [], []],
    ["/tasks show t1", "all good", [], [], []],
    ["/tasks stop t1", "stopped", ["stopTask t1 s1"], [], []],
    ["/tasks peek", "error:", [], [], []],
    ["/attach shot.png", "shot.png", [], ["attach shot.png"], []],
    ["/attach clear", "2", [], [], []],
    ["/attach", "error: usage", [], [], []],
    ["/init", "", [], [], ['/setup {"command":true}']],
    ["/memory", "AGENTS.md", [], [], []],
    ["/memory add use tabs", "BLITZ.md", ["addMemory use tabs"], [], []],
    ["/memory notes", "short answers", [], [], []],
    ["/memory forget style", "style", ["forget style"], [], []],
    ["/memory forget", "error:", [], [], []],
    ["/trust", "trusted", [], [], []],
    ["/approvals", "go test", [], [], []],
    ["/approvals revoke 1", "Revoked 1 rule", ["revoke k1 false"], [], []],
    ["/approvals clear", "Revoked 3 rules", ["revoke  true"], [], []],
    ["/approvals revoke 7", "error:", [], [], []],
    ["/hooks", "./check.sh", [], [], []],
    ["/permissions", "shell(go test *)", [], [], []],
    ["/permissions deny shell(git push *) --save", "deny", ["allow? deny shell(git push *) true"], ["settingsChanged"], []],
    ["/permissions remove shell(go test *)", "removed", ["remove shell(go test *)"], ["settingsChanged"], []],
    ["/permissions nope x", "error: usage", [], [], []],
    ["/sandbox", "Network: off", [], [], []],
    ["/envs", "numpy", [], [], []],
    ["/envs prune", "1.0 MB", ["prune"], [], []],
    ["/envs remove abc", "abc", ["removeEnv abc"], [], []],
    ["/envs what", "error:", [], [], []],
    ["/mcp", "github", [], [], []],
    ["/skills", "pdf", [], [], []],
    ["/skills search pd", "pdf", [], [], []],
    ["/skills show pdf", "read_file", [], [], []],
    ["/skills show nope", "error:", [], [], []],
    ["/skills update", "", [], [], ['/skills update {"command":true}']],
    ["/skills search lit", "castor/lit-review", [], [], []],
    ["/skills install castor/lit-review --project", "Installed lit-review 1.2.0 from castor", ["install castor/lit-review /w"], [], []],
    ["/skills install", "error:", [], [], []],
    ["/skills registry", "", [], ["settings skills undefined"], []],
    ["/tools", "read_file", [], [], []],
    ["/config", "", [], ["settings file /w"], []],
    ["/config temperature=0.1", "this session", ["set temperature=0.1"], ["settingsChanged"], []],
    ["/config ui.theme=dark --save", "/u/.env.toml", ["config ui.theme=dark"], ["settingsChanged"], []],
    ["/config nonsense", "error: usage", [], [], []],
    ["/locale", "en-US", [], [], []],
    ["/locale es", "Español", ["locale es"], ["settingsChanged"], []],
    ["/theme", "system", [], [], []],
    ["/theme dark", "dark", [], ["theme dark"], []],
    ["/theme pink", "error: usage", [], [], []],
    ["/help", "HELP", [], [], []],
    ["/cost", "$0.0100", [], [], []],
    ["/context", "9", [], [], []],
    ["/compact api", "", ["compact api"], ["refreshTotal"], []],
    ["/session save keep", "keep", ["snapshot keep false"], ["refreshList"], []],
    ["/session", "error: usage", [], [], []],
    ["/rename New title", "New title", [], ["rename New title"], []],
    ["/new", "", [], ["newSession"], []],
    ["/diff", "", [], ["view changes"], []],
    ["/plan add a cache", "", [], [], ['add a cache {"plan":true}']],
    ["/btw why?", "", [], [], ['why? {"aside":true,"shown":"/btw why?"}']],
    ["/agent qa", "QA", ["setAgent qa"], ["settingsChanged"], []],
    ["/model openai/gpt-5", "openai/gpt-5", ["setModel openai/gpt-5"], ["settingsChanged"], []],
    ["/mode plan", "", ["mode plan"], ["settingsChanged"], []],
    ["/effort high", "high", ["set effort=high"], ["settingsChanged"], []],
    ["/search session cart", "cart", [], [], []],
    ["/search", "error: usage", [], [], []],
    ["/checkpoints", "", [], [], []],
    ["/undo", "", [], [], []],
    ["/license full", "", [], [], []],
    ["/docs", "", [], [], []],
    ["/docs spec then setup", "", [], [], []],
    ["/docs zzzz-nothing", "Nothing in the docs", [], [], []],
    ["/license what", "error: usage", [], [], []],
  ])("%s", async (line, says, wantCalls, wantDid, wantRan) => {
    const r = await exec(line);
    if (says) expect(r.said).toContain(says);
    else expect(r.said).not.toContain("error:");
    expect(r.calls).toEqual(wantCalls);
    expect(r.did).toEqual(wantDid);
    expect(r.ran).toEqual(wantRan);
  });

  it("/trust opens the review when settings wait for it", async () => {
    const review = vi.fn();
    await exec("/trust", { projectWaiting: 2, reviewProject: review });
    expect(review).toHaveBeenCalled();
  });

  it("/loop starts, lists and stops a loop that skips while a turn runs", async () => {
    vi.useFakeTimers();
    try {
      let busy = false;
      const sent: string[] = [];
      const over = { send: (t: string) => sent.push(t), busy: () => busy };
      expect((await exec("/loop 1m check the build", over)).said).toContain("Loop 1 started");
      vi.advanceTimersByTime(60_000);
      busy = true;
      vi.advanceTimersByTime(60_000);
      busy = false;
      vi.advanceTimersByTime(60_000);
      expect(sent).toEqual(["check the build", "check the build"]);
      expect((await exec("/loop", over)).said).toContain("every 1m: check the build");
      expect((await exec("/loop stop 1", over)).said).toContain("Loop 1 stopped");
      vi.advanceTimersByTime(120_000);
      expect(sent).toHaveLength(2);
    } finally {
      vi.useRealTimers();
    }
  });
});
