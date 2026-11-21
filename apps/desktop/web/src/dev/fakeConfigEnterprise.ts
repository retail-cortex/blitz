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
import type { ConfigService } from "../gen/blitz/v1/config_pb";
import {
  fakeNotices,
  fakeOrgItems,
  fakeOrgWorkspaces,
  fakeServiceJob,
  fakeServicePushed,
  fakeServiceRuns,
  fakeServiceSet,
  fakeSubmissions,
} from "./fakeConfigData";
import { fakeOrg, fakeOrgState, fakeParam, now, sleep } from "./fakeHelpers";

type EnterpriseMethods = Pick<
  ServiceImpl<typeof ConfigService>,
  | "getEnterprise"
  | "getConnectorConsent"
  | "revokeConnectorConsent"
  | "refreshEnterprise"
  | "connectEnterprise"
  | "enterpriseSignIn"
  | "getBranding"
  | "getTerms"
  | "acceptTerms"
  | "listOrgWorkspaces"
  | "createOrgWorkspace"
  | "shareWithOrganization"
  | "openOrgWorkspace"
  | "watchServiceRuns"
  | "getOrgNotices"
  | "setOrgNotices"
  | "setOrgItem"
  | "syncOwnItems"
  | "shareOwnItem"
  | "listSubmissions"
  | "listOrgItems"
  | "getOrgUsage"
  | "listServiceApprovals"
  | "answerServiceRun"
  | "getServiceJob"
  | "pushServiceJob"
  | "listServiceJobs"
  | "runServiceJob"
  | "setServiceJobEnabled"
  | "listServiceRunFiles"
  | "takeServiceRun"
  | "saveServiceRunFile"
  | "getOrgSync"
  | "saveOrgSync"
  | "enterpriseSignOut"
>;

/** Enterprise, organization catalog, org workspaces, and service job handlers for ConfigService. */
export function fakeConfigEnterpriseHandlers(): EnterpriseMethods {
  return {
    getEnterprise: () => ({ state: fakeOrgState(true) }),
    getConnectorConsent: () => (fakeOrg.signedIn ? { connectors: ["warehouse"], consented: false, url: "https://accounts.google.com/o/oauth2/auth?fake" } : { connectors: [] }),
    revokeConnectorConsent: () => ({}),
    refreshEnterprise: async () => {
      await sleep(500);
      return { state: fakeOrgState(true), changed: false };
    },
    connectEnterprise: ({ url }) => {
      if (url && !url.startsWith("https://")) throw new ConnectError("the organization's service is reached over https", Code.InvalidArgument);
      fakeOrg.url = url.replace(/\/$/, "");
      fakeOrg.signedIn = false;
      return { state: fakeOrgState(!!url) };
    },
    enterpriseSignIn: async function* () {
      if (!fakeOrg.url) throw new ConnectError("connect to your organization's service first", Code.FailedPrecondition);
      yield { url: "https://accounts.example.com/o/oauth2/auth?client_id=blitz-desktop" };
      await sleep(1500);
      fakeOrg.signedIn = true;
      yield { done: true, state: fakeOrgState(false) };
    },
    getBranding: () =>
      fakeOrg.url && fakeParam("brand")
        ? {
            branding: {
              name: "Acme Assist",
              shortName: "Acme",
              accent: "#7b3fe4",
              accentDark: "#c4a5ff",
              support: "https://acme.example/help",
              about: "Acme's assistant for its engineers, with Acme's models and rules.",
              images: { "logo.svg": "data:image/svg+xml;base64," + btoa('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10" fill="#7b3fe4"/><path d="M8 16l4-9 4 9" stroke="#fff" stroke-width="2" fill="none"/></svg>') },
              documents: [
                { document: "terms", version: "2026-11", text: "## Acceptable use\n\nUse Acme Assist for Acme's work only. Don't paste customers' personal data." },
                { document: "privacy", version: "1", url: "https://acme.example/privacy" },
              ],
            },
          }
        : {},
    getTerms: () =>
      fakeOrg.termsAccepted || !fakeParam("terms")
        ? { version: fakeParam("terms") ? "2026-11" : "", accepted: true }
        : { version: "2026-11", accepted: false, text: "## Acceptable use\n\nUse Acme Assist for Acme's work only. Don't paste customers' personal data.\n\n- Keep secrets out of prompts.\n- Check what the agent changes before you ship it." },
    acceptTerms: () => {
      fakeOrg.termsAccepted = true;
      return { state: fakeOrgState(false) };
    },
    listOrgWorkspaces: () => {
      if (!fakeOrg.url) throw new ConnectError("connect to your organization's service first", Code.FailedPrecondition);
      return { organization: "Acme Assist", workspaces: fakeOrgWorkspaces };
    },
    createOrgWorkspace: async ({ name, personal }) => {
      await sleep(600);
      const handle = (personal ? "~me/" : "") + name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
      if (fakeOrgWorkspaces.some((w) => w.handle === handle)) throw new ConnectError("a workspace has that handle already", Code.FailedPrecondition);
      const w = { handle, name, role: "owner", sync: true, dir: `/home/me/Blitz/Acme Assist/${handle}`, cloned: false };
      fakeOrgWorkspaces.push(w);
      return { workspace: w };
    },
    shareWithOrganization: async ({ workspace, name, personal }) => {
      await sleep(1200);
      if (workspace.includes("leaky") || name.toLowerCase().includes("leaky"))
        throw new ConnectError("nothing was shared: the folder or its history holds what look like secrets: config.py:1 (AWS access key AKIA••••••••••••). Remove them from the files and the history, or mark a false alarm with blitz:allow-secret on its line", Code.FailedPrecondition);
      const handle = (personal ? "~me/" : "") + name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
      const w = { handle, name, role: "owner", sync: true, dir: workspace, cloned: true };
      fakeOrgWorkspaces.push(w);
      return { workspace: w, notes: ["Your changes not yet committed were committed first, as “Shared with Acme Assist”."] };
    },
    openOrgWorkspace: async ({ handle }) => {
      await sleep(800);
      return { dir: `/home/me/Blitz/Acme Assist/${handle}`, cloned: true };
    },
    watchServiceRuns: async function* (_req, ctx) {
      if (!fakeOrg.url || !fakeOrg.signedIn) throw new ConnectError("connect to your organization's service first", Code.FailedPrecondition);
      let last = "";
      for (;;) {
        const going = fakeServiceRuns.filter((r) => r.status === "running");
        const sig = going.map((r) => r.id).join("|");
        if (sig !== last) {
          const change = last && { runId: last.split("|")[0], jobId: fakeServiceJob.id, workspace: "plans", dir: "/home/me/Blitz/Acme Assist/plans", status: "succeeded" };
          last = sig;
          yield {
            listed: true,
            change: change || undefined,
            runs: going.map((run) => ({ run, jobId: fakeServiceJob.id, jobName: fakeServiceJob.name, workspace: "plans", dir: "/home/me/Blitz/Acme Assist/plans" })),
          };
        }
        for (const r of going) if (Date.now() - Number(r.created?.seconds ?? 0n) * 1000 > 8000) r.status = "succeeded";
        await sleep(500);
        if (ctx?.signal.aborted) return;
      }
    },
    getOrgNotices: () => ({ notices: fakeNotices }),
    setOrgNotices: async ({ notices }) => {
      await sleep(300);
      if (notices?.webhook && !notices.webhook.startsWith("https://")) throw new ConnectError("a webhook is an https:// address", Code.InvalidArgument);
      Object.assign(fakeNotices, notices);
      return { notices: fakeNotices };
    },
    setOrgItem: ({ kind, name, on }) => {
      const it = fakeOrgItems.find((i) => i.kind === kind && i.name === name);
      if (!it || it.state !== "available") throw new Error("Your organization requires it: it can't be switched off.");
      it.on = on;
      return { item: it };
    },
    syncOwnItems: () => ({ sent: ["skill/tone"], received: [], removed: [], conflicts: [], skipped: {} }),
    shareOwnItem: ({ kind, name, note }) => {
      const s = { id: BigInt(fakeSubmissions.length + 1), kind, name, note, state: "judging", verdicts: [], reason: "", publishedVersion: "" };
      fakeSubmissions.unshift(s);
      return { submission: s };
    },
    listSubmissions: () => ({
      submissions: fakeOrg.signedIn ? fakeSubmissions : [],
      own: [
        { kind: "skill", name: "tone", skipped: "" },
        { kind: "agent", name: "reviewer", skipped: "" },
        { kind: "skill", name: "Big Notes", skipped: "its name isn't lowercase letters, digits and single hyphens or underscores" },
      ],
      contribute: ["skill"],
    }),
    listOrgItems: ({ kind }) => ({
      version: 7,
      items: fakeOrg.signedIn ? fakeOrgItems.filter((i) => !kind || i.kind === kind) : [],
    }),
    getOrgUsage: () => ({
      today: [
        { model: "claude-sonnet-5-5", requests: 42n, inputTokens: 380_000n, outputTokens: 41_000n, costUsd: 1.84 },
        { model: "gemini-4-flash", requests: 120n, inputTokens: 900_000n, outputTokens: 60_000n, costUsd: 0.31 },
      ],
      month: [{ model: "claude-sonnet-5-5", requests: 812n, inputTokens: 7_400_000n, outputTokens: 790_000n, costUsd: 36.2 }],
      limits: [
        { scopeKind: "person", period: "day", unit: "usd", model: "", amountUsd: 5, used: { costUsd: fakeParam("budget") ? 4.7 : 2.15 }, resets: timestampFromDate(new Date(Date.now() + 9 * 3600e3)) },
        { scopeKind: "group", period: "month", unit: "tokens", model: "claude-sonnet-5-5", tokens: 20_000_000n, used: { inputTokens: 7_400_000n, outputTokens: 790_000n }, resets: timestampFromDate(new Date(Date.now() + 12 * 24 * 3600e3)) },
      ],
      rateLimits: [{ model: "claude-sonnet-5-5", personRpm: 20, tpm: 400_000 }],
      promptLogs: true,
    }),
    listServiceApprovals: () => {
      if (!fakeOrg.url || !fakeOrg.signedIn) throw new ConnectError("connect to your organization's service first", Code.FailedPrecondition);
      return {
        runs: fakeServiceRuns
          .filter((r) => r.status === "paused")
          .map((run) => ({ run, jobId: fakeServiceJob.id, jobName: fakeServiceJob.name, workspace: "plans", dir: "/home/me/Blitz/Acme Assist/plans" })),
      };
    },
    answerServiceRun: ({ runId }) => {
      const run = fakeServiceRuns.find((r) => r.id === runId && r.status === "paused");
      if (!run) throw new ConnectError("that run isn't waiting for an answer", Code.FailedPrecondition);
      run.status = "running";
      run.pending = undefined;
      return { run };
    },
    getServiceJob: ({ name }) => {
      if (fakeParam("unpushed") && !fakeServicePushed.has(name)) return { found: false, runs: [], reason: "not_pushed" };
      if (fakeParam("notservice")) return { found: false, runs: [], reason: "not_service", detail: 'its WORKER.md doesn\'t read: schedule "whenever" isn\'t a schedule' };
      if (name === "sales-digest" || fakeServicePushed.has(name)) return { found: true, job: { ...fakeServiceJob, name }, runs: name === "sales-digest" && !fakeServicePushed.has(name) ? fakeServiceRuns : [] };
      return { found: false, runs: [], reason: "not_read" };
    },
    pushServiceJob: ({ name }) => {
      fakeServicePushed.add(name);
      return { job: { found: true, job: { ...fakeServiceJob, name }, runs: [] } };
    },
    listServiceJobs: () => ({
      jobs: fakeOrg.signedIn
        ? [{ id: "oj1", urn: "urn:blitz:jobs:oj1", name: "weekly-digest", description: "The week's orders, summed up", schedule: "Mondays at 8:00", enabled: true, workspace: "organization", dir: "", organization: true, nextRun: timestampFromDate(new Date(Date.now() + 36 * 3600e3)) }]
        : [],
    }),
    runServiceJob: () => {
      const run = { id: `run-${Date.now()}`, trigger: "now", status: "running", costUsd: 0, created: now(), artifacts: `${fakeServiceSet}:${Date.now()}` };
      fakeServiceRuns.unshift(run as (typeof fakeServiceRuns)[number]);
      return { run };
    },
    setServiceJobEnabled: ({ enabled }) => {
      fakeServiceJob.enabled = enabled;
      return {};
    },
    listServiceRunFiles: ({ artifacts }) => ({
      files: artifacts.endsWith("9f01")
        ? [
            { urn: `${artifacts}/events.jsonl`, path: "events.jsonl", size: 48213n },
            { urn: `${artifacts}/out/sales-by-region.csv`, path: "out/sales-by-region.csv", size: 1820n },
            { urn: `${artifacts}/out/sales-by-region.png`, path: "out/sales-by-region.png", size: 64410n },
          ]
        : [],
    }),
    takeServiceRun: ({ runId }) => {
      const run = fakeServiceRuns.find((r) => r.id === runId);
      if (run) Object.assign(run, { merged: true, mergeNote: "" });
      return { run };
    },
    saveServiceRunFile: ({ urn }) => ({ path: `/home/me/Downloads/${urn.split("/").pop()}` }),
    getOrgSync: ({ workspace }) =>
      workspace.includes("/Blitz/Acme Assist/")
        ? fakeParam("conflict")
          ? { state: "conflicts", pairs: [{ shared: "plan.md", mine: "plan (conflict, Ana).md" }] }
          : fakeParam("refused")
            ? { state: "refused", pairs: [], error: "exit status 1: error: remote unpack failed: reference delta not found", failed: true }
            : { state: "saved", pairs: [] }
        : { state: "", pairs: [] },
    saveOrgSync: () => (fakeParam("refused") ? { state: "refused", pairs: [], error: "exit status 1: error: remote unpack failed: reference delta not found", failed: true } : { state: "saved", pairs: [] }),
    enterpriseSignOut: () => {
      fakeOrg.signedIn = false;
      return { state: fakeOrgState(false) };
    },
  };
}
