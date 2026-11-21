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

import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError, type ServiceImpl } from "@connectrpc/connect";
import { ErrorInfoSchema } from "../gen/blitz/v1/turn_pb";
import { WorkspaceService } from "../gen/blitz/v1/workspace_pb";
import { personalityCheck, sessionSetting, setModelSetting, type ModelSettings } from "./fakeChecks";
import { fakeAccepted, fakeModeNote, fakeModelInfo, fakeModelSettings, fakePersonality, fakePersonalityOf, searchSettingsOf } from "./fakeConfigData";
import { fakeHealthHandlers } from "./fakeHealth";
import { fakeInvalid, fakeParam, now, sleep, state, states } from "./fakeHelpers";
import { fakePicturesAndSpecsHandlers } from "./fakePicturesSpecs";
import { fakeProject, fakeSuggestions } from "./fakeTurn";
import { fakeSearchCorpus } from "./fakeWorkspaceFiles";
import { fakeWorkspaceGitHandlers } from "./fakeWorkspaceGit";
import { fakeWorkspaceStudioHandlers } from "./fakeWorkspaceStudio";

// The days the fake service has a log for (DeleteLogDay removes them).
let fakeLogDays = ["2026-09-28", "2026-09-27"];

/** Returns the fake ConnectRPC handler implementation for WorkspaceService. */
export function fakeWorkspaceService(): ServiceImpl<typeof WorkspaceService> {
  const impl: Partial<ServiceImpl<typeof WorkspaceService>> = {
    ...fakeWorkspaceGitHandlers(),
    ...fakeWorkspaceStudioHandlers(),
    ...fakeHealthHandlers(),
    ...fakePicturesAndSpecsHandlers(),
    getSandbox: () => ({ summary: ["Shell commands: sandboxed (sandbox-exec)", "Network: allowed for listed hosts", "Writable: the workspace and the temp folder"] }),
    pinModel: ({ agent, ref }) => ({ agent, model: ref }),
    unpinModel: ({ agent, workspace }) => ({ agent, model: state(workspace).settings.model }),
    reloadMemory: () => ({ paths: ["/home/me/blitz/AGENTS.md", "/home/me/blitz/.agents/AGENTS.md"], files: ["AGENTS.md"] }),
    addMemory: () => ({ path: "/home/me/blitz/BLITZ.md" }),
    listNotes: () => ({ notes: [{ name: "answer-style", kind: "feedback", text: "Keep answers short; show the command first." }] }),
    forgetNote: () => ({}),
    listHooks: () => ({ hooks: [{ event: "PreToolUse", type: "command", match: "run_shell_command", runs: "./scripts/guard.sh", source: "your settings", failures: [] }] }),
    listEnvs: () => ({ envs: [{ key: "a1b2c3", deps: ["pandas", "matplotlib"], skills: ["data-report"], size: 182_000_000n, ready: true }] }),
    pruneEnvs: () => ({ removed: 0, freed: 0n, failed: [] }),
    removeEnv: () => ({}),
    // A ! command: a few lines of output, then how it ended ("fail" in it exits 1; "sleep" runs until stopped).
    runShell: async function* ({ command }, ctx) {
      const start = Date.now();
      const lines = command.includes("sleep") ? [] : [`$ ${command}`, "total 24", "drwxr-xr-x  5 you  staff  160 Oct 26 09:12 cmd", "drwxr-xr-x  7 you  staff  224 Oct 26 09:12 internal", "-rw-r--r--  1 you  staff  512 Oct 26 09:12 go.mod"];
      for (const l of lines) {
        await sleep(150);
        yield { output: l + "\n" };
      }
      if (command.includes("sleep")) await new Promise((_, reject) => ctx.signal.addEventListener("abort", () => reject(new ConnectError("canceled", Code.Canceled))));
      yield { done: true, exitCode: command.includes("fail") ? 1 : 0, elapsedMs: BigInt(Date.now() - start) };
    },
    getSettings: ({ workspace }) => {
      const s = state(workspace).settings;
      return { agent: s.agent, model: s.model, provider: s.provider, effort: s.effort, permissionMode: s.mode, modeNote: fakeModeNote(s.provider), agency: s.agency, locale: s.locale, style: s.style, imagesEnabled: true, speechModel: s.speechModel ?? "", imageModel: s.imageModel ?? "", acceptedMedia: fakeAccepted(s.provider) };
    },
    // ?nomodel: the model can't be reached (the chat's warning card).
    getModel: ({ workspace }) => ({
      name: state(workspace).settings.model,
      provider: state(workspace).settings.provider,
      unavailable: fakeParam("nomodel") || fakeParam("newuser") ? "the Gemini API key is missing (set GEMINI_API_KEY)" : "",
    }),
    setModel: ({ workspace, ref }) => {
      const [provider, name] = ref.includes("/") ? ref.split("/", 2) : [state(workspace).settings.provider, ref];
      const st = state(workspace).settings;
      Object.assign(st, { provider, model: name });
      if (fakeModeNote(provider) && st.mode === "auto") st.mode = "default"; // APR-14c
      return {};
    },
    setSetting: ({ workspace, key, value }) => {
      const s = state(workspace).settings;
      const r = sessionSetting(key, value);
      if ("error" in r) throw fakeInvalid(r.error, r.reason, r.reason === "UNKNOWN_SETTING" ? { key } : {});
      if (r.key === "effort") s.effort = r.value;
      if (r.key === "agency") s.agency = r.value;
      if (r.key === "style") s.style = r.value;
      if (r.key === "personality") fakePersonalityOf.set(workspace, fakePersonality(r.value).name);
      return { key: r.key };
    },
    setPermissionMode: ({ workspace, mode }) => {
      if (mode === "bypass") {
        const err = new ConnectError("bypass needs the OS sandbox", Code.FailedPrecondition);
        err.details.push({ desc: ErrorInfoSchema, value: { reason: "BYPASS_NEEDS_SANDBOX" } });
        throw err;
      }
      const st = state(workspace).settings;
      st.mode = mode === "auto" && fakeModeNote(st.provider) ? "default" : mode; // APR-14c
      return { mode: st.mode };
    },
    // Each model's settings, as set (none at first: Default), and the global ones under them.
    getModelSettings: ({ ref }) => (ref ? { model: fakeModelInfo(ref) } : { all: Object.fromEntries(fakeModelSettings) }),
    updateModelSettings: ({ ref, reset, changes }) => {
      const name = ref.split("/").pop() ?? ref;
      let next: ModelSettings = reset ? {} : { ...(fakeModelSettings.get(name) ?? {}) };
      for (const c of changes) {
        const r = setModelSetting(next, c.key, c.value);
        if (typeof r === "string") throw fakeInvalid(r);
        next = r;
      }
      if (Object.keys(next).length) fakeModelSettings.set(name, next);
      else fakeModelSettings.delete(name);
      // Gemini takes no seed (the provider's: reported, still saved).
      return { model: fakeModelInfo(ref), unsupported: changes.filter((c) => c.key === "seed" && c.value).map((c) => c.key) };
    },
    // A sample answer that shows the personality (PER-30).
    tryPersonality: ({ text }) => {
      const why = text.trim() ? personalityCheck({ name: "try", description: "", voice: "", text }) : "";
      if (why) throw fakeInvalid(why, "INVALID_SETTING");
      const answer = /mentor|explain/i.test(text)
        ? "Let's find it together: the error names the import that's missing. Check the module is in go.mod, then run the build again. Does that make sense?"
        : /cheer|upbeat/i.test(text)
          ? "No worries, that's an easy one! 🎉 Add the missing module to go.mod and run the build again."
          : /direct/i.test(text)
            ? "Add the missing module to go.mod. Then rebuild. If it still fails, the import path is wrong."
            : "The error names the missing import. Add its module to go.mod (go get), then run the build again.";
      return { answer, question: "The build failed on a missing import. What now?" };
    },
    listStyles: ({ workspace }) => ({
      styles: [
        { name: "concise", description: "Short answers; details only when asked", source: "built-in" },
        { name: "default", description: "No style: the agent's own way of answering", source: "built-in" },
        { name: "explanatory", description: "Explains choices and the codebase as it works, to learn from", source: "built-in" },
      ].map((st) => ({ ...st, active: st.name === state(workspace).settings.style })),
    }),
    listLocales: () => ({ locales: [{ tag: "en-US", name: "English" }, { tag: "es", name: "Español" }, { tag: "fr-CA", name: "Français (Canada)" }] }),
    setLocale: ({ input }) => ({ tag: input }),
    listPermissionRules: () => ({
      rules: [
        { effect: "allow", rule: "shell(go test *)", source: "config" },
        { effect: "ask", rule: "shell(git push *)", source: "config" },
        { effect: "deny", rule: "read(.env*)", source: "session" },
      ],
    }),
    addPermissionRule: ({ rule }) => ({ rule }),
    removePermissionRule: ({ rule }) => ({ rule }),
    listApprovals: () => ({ approvals: [{ key: "k1", kind: "write", subject: "internal/cart", always: false }] }),
    revokeApprovals: () => ({ revoked: 1 }),
    searchWorkspace: ({ query, sources, folder }) => {
      const words = query.toLowerCase().split(/\s+/).filter(Boolean);
      let want = sources.length ? sources : ["files", "documents"];
      if (folder) want = want.filter((s) => s === "files" || s === "documents"); // chats and notes aren't in a folder (SRCH-18)
      const under = (ref: string) => !folder || ref === folder || ref.startsWith(folder + "/");
      return { sources: want, hits: fakeSearchCorpus.filter((h) => want.includes(h.source) && under(h.ref) && words.every((w) => `${h.ref} ${h.title} ${h.snippet}`.toLowerCase().includes(w))) };
    },
    getSearchStatus: ({ workspace }) => {
      const settings = searchSettingsOf(workspace);
      const on = settings.enabled;
      return {
        status: {
          enabled: on,
          items: (on ? { files: 42, documents: 3, chats: 7, notes: 2 } : {}) as Record<string, number>,
          scanning: false,
          lastScan: on ? now() : undefined,
          unreadable: on ? 1 : 0,
          error: "",
          embeddingModel: on ? settings.embeddingModel : "",
          embedded: on && settings.embeddingModel ? 318 : 0,
          embedError: "",
          enriched: on && settings.enrich ? 37 : 0,
          enrichError: "",
          problem: "",
          settings,
        },
      };
    },
    reindex: () => ({}),
    getProjectSettings: ({ workspace }) => ({ settings: fakeProject(state(workspace)) }),
    trustProject: ({ workspace, trusted }) => {
      state(workspace).projectTrust = trusted ? "trusted" : "declined";
      return { reopened: true };
    },
    closeWorkspace: () => ({}),
    loadImage: ({ path }) => ({ image: { id: `img-${path}`, name: path, mime: "image/png", width: 640, height: 480, size: 12345n } }),
    // A file a model made (spec_056 CA-71): a 1×1 picture.
    readStoredFile: () => ({ data: Uint8Array.from(atob("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP8z8DwHwAFBQIAX8jx0gAAAABJRU5ErkJggg=="), (c) => c.charCodeAt(0)), mimeType: "image/png" }),
    getServiceInfo: () => ({ version: fakeParam("oldservice") ? "0.6.1" : "dev", executable: "", settingsChanged: fakeParam("settingschanged") }),
    listLogDays: () => ({ days: [...fakeLogDays], dir: "~/.blitz/logs" }),
    deleteLogDay: ({ day }) => {
      fakeLogDays = fakeLogDays.filter((d) => d !== day);
      return {};
    },
    readLog: ({ day, minLevel, text }) => {
      const d = day || "2026-09-28";
      const rank: Record<string, number> = { DEBUG: 0, INFO: 1, WARN: 2, ERROR: 3 };
      const min = rank[(minLevel || "debug").toUpperCase()];
      const all = [
        { time: timestampFromDate(new Date(`${d}T09:14:02.120`)), level: "INFO", message: "start", attrs: [{ key: "version", value: "dev" }, { key: "provider", value: "gemini" }] },
        { time: timestampFromDate(new Date(`${d}T09:14:09.803`)), level: "INFO", message: "workspace opened", attrs: [{ key: "workspace", value: "/home/me/blitz" }] },
        { time: timestampFromDate(new Date(`${d}T09:20:41.017`)), level: "WARN", message: "model unavailable", attrs: [{ key: "workspace", value: "/home/me/blitz" }, { key: "error", value: "quota exceeded: Quota exceeded for aiplatform.googleapis.com/online_prediction_requests_per_base_model" }] },
        { time: timestampFromDate(new Date(`${d}T09:21:03.500`)), level: "ERROR", message: "turn failed", attrs: [{ key: "error", value: "the model isn't available" }, { key: "attempt", value: "2" }] },
      ];
      const words = text.toLowerCase().split(/\s+/).filter(Boolean);
      const entries = all
        .filter((e) => rank[e.level] >= min && words.every((w) => `${e.level} ${e.message} ${e.attrs.map((a) => `${a.key}=${a.value}`).join(" ")}`.toLowerCase().includes(w)))
        .reverse();
      return { day: d, path: `~/.blitz/logs/blitz-${d}.jsonl`, entries, matched: entries.length };
    },
    // ?servicedown: the service doesn't answer (the start screen, DSK-50, DSK-51d).
    listWorkspaces: () => {
      if (fakeParam("servicedown")) throw new ConnectError("the service isn't running", Code.Unavailable);
      return { workspaces: [...states.keys()] };
    },
    getSuggestions: async ({ workspace }) => {
      await sleep(150);
      return fakeSuggestions(workspace);
    },
    snoozeReflection: () => ({}),
    listCommands: () => ({
      commands: [
        { name: "setup", description: "Set up the agent harness: .agents/AGENTS.md, skills and agents", source: "bundled" },
        { name: "spec", description: "Write or revise the project's specification, as numbered specs in .agents/specs/", argumentHint: "[what to build, or what changed]", source: "bundled" },
        { name: "skills-update", description: "Revisit this workspace's skills against its specification; also /skills update", argumentHint: "[what to focus on]", source: "bundled" },
        { name: "agents-update", description: "Revisit this workspace's agents against its specification; also /agents update", argumentHint: "[what to focus on]", source: "bundled" },
        { name: "rules-update", description: "Revisit this workspace's rules and guardrails against its specification and runs; also /rules update", argumentHint: "[what to focus on]", source: "bundled" },
        { name: "reflect", description: "Learn from this workspace's runs: what went wrong, the patterns, and what to change in its harness", argumentHint: "[since: a date or a number of days]", source: "bundled" },
        { name: "review", description: "Review the uncommitted changes for bugs", argumentHint: "[branch | files]", source: "bundled" },
        { name: "db:migrate", description: "Write a database migration", argumentHint: "<name>", source: "project" },
      ],
    }),
    searchWeb: ({ terms }) => ({ provider: "Google", links: [{ title: `${terms} — docs`, url: "https://example.com/docs" }], prompt: "Read these pages…" }),
    addImage: async ({ name, data }) => {
      await sleep(400);
      return { image: { id: `img-${data.length}`, name, mimeType: "image/png", width: 1280, height: 720, size: BigInt(data.length), resized: false } };
    },
  };
  return impl as ServiceImpl<typeof WorkspaceService>;
}
