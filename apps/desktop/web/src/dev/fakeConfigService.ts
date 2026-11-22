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
import { ConfigService, KeySource } from "../gen/blitz/v1/config_pb";
import { configValueProblem, mcpProblem, personalityCheck, policyDefaults, providerProblem, validProfileName, validSecretKey } from "./fakeChecks";
import {
  change,
  configPath,
  fakeGatewayModels,
  fakeMCP,
  fakeMCPOff,
  fakeOwnPersonalities,
  fakePersonalities,
  fakePersonality,
  fakePersonalityIn,
  fakePersonalityOf,
  fakePersonalityState,
  fakePolicy,
  fakeProfile,
  fakeProfileOf,
  fakeProfiles,
  fakeSecrets,
  mcpPath,
  scopeConfig,
  scopePermissions,
  searchSettingsOf,
  workspaceMCP,
} from "./fakeConfigData";
import { fakeConfigEnterpriseHandlers } from "./fakeConfigEnterprise";
import { fakeAlertsState, fakeInvalid, fakeOrg, fakeParam, fakeSignIn, fakeSignedIn, now, sleep, state } from "./fakeHelpers";
import { fakeConnectorCalls, fakeConnectorCategory, fakeConnectorKinds, fakeConnectorMsg, fakeConnectors, fakeInstalledSkills, fakeRegistries, fakeRegistrySkills, fakeTrusted } from "./fakeStudioData";
import { setFakeVoice } from "./fakeVoice";

/** Returns the fake ConnectRPC handler implementation for ConfigService. */
export function fakeConfigService(): ServiceImpl<typeof ConfigService> {
  const impl: Partial<ServiceImpl<typeof ConfigService>> = {
    ...fakeConfigEnterpriseHandlers(),
    trustSkillScripts: ({ dir, hash }) => (fakeTrusted.set(dir.split("/").pop()!, hash), {}),
    getSkillReview: () => ({}),
    listSkillRegistries: () => ({ registries: fakeRegistries, installed: fakeInstalledSkills }),
    listPersonalities: ({ workspace }) => ({
      profile: { ...fakeProfile },
      personalities: fakePersonalities().map((p) => ({ ...p, text: "", personDefault: p.name === fakePersonalityState.defaultName, workspaceChoice: !!workspace && p.name === fakePersonalityIn(workspace) })),
    }),
    getPersonality: ({ name }) => ({ personality: { ...fakePersonality(name) } }),
    savePersonality: ({ personality: p }) => {
      if (!p) throw fakeInvalid("no personality");
      const why = personalityCheck(p);
      if (why) throw fakeInvalid(why, "INVALID_SETTING");
      fakeOwnPersonalities.set(p.name, { name: p.name, description: p.description.trim(), voice: p.voice, source: "own", text: p.text.trim() });
      return { change: { path: `~/.blitz/personalities/${p.name}.md`, problems: [] } };
    },
    deletePersonality: ({ name }) => {
      const p = fakePersonality(name);
      if (p.source !== "own") throw fakeInvalid("a built-in personality or the organization's can't be changed: duplicate it to make your own", "READ_ONLY");
      fakeOwnPersonalities.delete(p.name);
      return { change: { path: `~/.blitz/personalities/${p.name}.md`, problems: [] } };
    },
    listBrowserProfiles: ({ workspace }) => ({ profiles: fakeProfiles.map((p) => ({ ...p })), workspaceProfile: fakeProfileOf.get(workspace) ?? "", show: "ask", open: "ask" }),
    createBrowserProfile: ({ name }) => {
      if (!validProfileName(name)) throw fakeInvalid(`profile name ${JSON.stringify(name)}: lowercase letters, digits, - and _, up to 32`);
      if (fakeProfiles.some((p) => p.name === name)) throw new ConnectError(`a browser profile named "${name}" exists`, Code.InvalidArgument);
      const p = { name, created: now(), allowDomains: [] as string[] };
      fakeProfiles.push(p);
      return { profile: p };
    },
    deleteBrowserProfile: ({ name }) => {
      fakeProfiles.splice(0, fakeProfiles.length, ...fakeProfiles.filter((p) => p.name !== name));
      for (const [ws, p] of fakeProfileOf) if (p === name) fakeProfileOf.delete(ws);
      return {};
    },
    clearBrowserProfile: () => ({}),
    setBrowserProfileDomains: ({ name, allowDomains }) => {
      const p = fakeProfiles.find((x) => x.name === name);
      if (!p) throw new ConnectError(`no browser profile "${name}"`, Code.NotFound);
      p.allowDomains = allowDomains;
      return { profile: p };
    },
    setWorkspaceBrowserProfile: ({ workspace, name }) => {
      if (name) fakeProfileOf.set(workspace, name);
      else fakeProfileOf.delete(workspace);
      return {};
    },
    openBrowserProfile: () => ({ browser: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" }),
    listSecrets: ({ workspace }) => ({ secrets: (fakeSecrets.get(workspace) ?? []).map((key) => ({ key, updated: { seconds: BigInt(1760700000), nanos: 0 } })), store: "macOS Keychain" }),
    setSecret: ({ workspace, key, value }) => {
      if (!validSecretKey(key)) throw fakeInvalid(`${JSON.stringify(key)} can't name a secret: use letters, digits, _, . and -`);
      if (!value) throw fakeInvalid("a secret needs a value");
      const l = fakeSecrets.get(workspace) ?? [];
      if (!l.includes(key)) fakeSecrets.set(workspace, [...l, key].sort());
      return {};
    },
    deleteSecret: ({ workspace, key }) => {
      fakeSecrets.set(workspace, (fakeSecrets.get(workspace) ?? []).filter((k) => k !== key));
      return {};
    },
    listBuiltinSkills: () => ({
      skills: [
        { name: "code-review", description: "Review a change for bugs, style and tests", version: "1.0.0" },
        { name: "git-workflow", description: "Branches, commits and pull requests the team's way", version: "1.0.0" },
        { name: "testing-tdd", description: "Write the failing test first, then the code", version: "1.0.0" },
      ],
    }),
    listMCPServerConfigs: ({ workspace }) =>
      workspace
        ? {
            servers: [
              ...fakeMCP.map((m) => ({ ...m, scope: "global", offHere: !!fakeMCPOff.get(workspace)?.has(m.name) })),
              ...workspaceMCP(workspace).map((m) => ({ ...m, scope: "workspace" })),
              { name: "db", command: "npx @acme/db-mcp", scope: "project" },
            ],
            path: mcpPath(workspace),
          }
        : { servers: fakeMCP.map((m) => ({ ...m, scope: "global" })), path: mcpPath("") },
    listGlobalConnectors: () => ({
      categories: [{ ...fakeConnectorCategory, workspaceDir: "", state: "", toolCount: 0 }],
      kinds: fakeConnectorKinds,
      connections: fakeConnectors.filter((d) => d.scope === "global").map((d) => fakeConnectorMsg("", { ...d, enabled: true })),
      globalDir: "~/.blitz/connectors",
    }),
    saveGlobalConnector: ({ connection, previousName, secretValues }) => fakeConnectorCalls.save("", { ...connection!, scope: "global" }, previousName, secretValues),
    deleteGlobalConnector: ({ category, name }) => {
      const i = fakeConnectors.findIndex((d) => d.name === name && d.scope === "global" && d.category === category);
      if (i < 0) throw new ConnectError(`no connection ${name}`, Code.NotFound);
      fakeConnectors.splice(i, 1);
      return {};
    },
    testGlobalConnector: ({ connection }) => fakeConnectorCalls.test(connection!),
    tryGlobalConnectorTool: ({ connection, tool }) => fakeConnectorCalls.tryTool(connection!, tool),
    readGlobalConnectorSchema: ({ connection }) => fakeConnectorCalls.readSchema(connection!),
    draftGlobalConnectorQuery: ({ request }) => fakeConnectorCalls.draft(request),
    saveMCPServer: ({ workspace, name, server }) => {
      const list = workspace ? workspaceMCP(workspace) : fakeMCP;
      const i = list.findIndex((m) => m.name === name);
      if (!name && list.some((m) => m.name === server?.name)) throw new Error(`an MCP server named "${server?.name}" is already configured`);
      if (name && i < 0) throw new ConnectError(`no MCP server by that name: ${name}`, Code.NotFound);
      const why = mcpProblem({ name: server?.name ?? "", command: server?.command ?? "", url: server?.url ?? "" });
      if (why) throw fakeInvalid(why);
      if (i >= 0) list[i] = server!;
      else list.push(server!);
      return { change: { path: mcpPath(workspace) } };
    },
    removeMCPServer: ({ workspace, name }) => {
      const list = workspace ? workspaceMCP(workspace) : fakeMCP;
      const i = list.findIndex((m) => m.name === name);
      if (i < 0) throw new ConnectError(`no MCP server by that name: ${name}`, Code.NotFound);
      list.splice(i, 1);
      return { change: { path: mcpPath(workspace) } };
    },
    setMCPServerEnabled: ({ workspace, name, enabled }) => {
      const own = workspace ? workspaceMCP(workspace).find((x) => x.name === name) : undefined;
      if (workspace && !own) {
        const off = fakeMCPOff.get(workspace) ?? new Set<string>();
        if (enabled) off.delete(name);
        else off.add(name);
        fakeMCPOff.set(workspace, off);
      } else {
        const m = own ?? fakeMCP.find((x) => x.name === name);
        if (m) m.disabled = !enabled;
      }
      return { change: { path: mcpPath(workspace) } };
    },
    makeMCPServerGlobal: ({ workspace, name }) => {
      const list = workspaceMCP(workspace);
      const i = list.findIndex((m) => m.name === name);
      if (i < 0) throw new ConnectError(`no MCP server by that name: ${name}`, Code.NotFound);
      if (fakeMCP.some((m) => m.name === name)) throw new ConnectError(`an MCP server named "${name}" is already configured`, Code.AlreadyExists);
      fakeMCP.push(list.splice(i, 1)[0]);
      return { change: { path: mcpPath("") } };
    },
    addSkillRegistry: ({ url, name, token }) => {
      if (!/^https:\/\//.test(url) && !url.startsWith("/")) throw new ConnectError(`${url}: a registry needs https (http only for localhost)`, Code.FailedPrecondition);
      const reg = { name: name || "castor", url, hasToken: !!token };
      fakeRegistries.splice(0, fakeRegistries.length, ...fakeRegistries.filter((r) => r.name !== reg.name), reg);
      return { registry: reg, skills: fakeRegistrySkills.length };
    },
    removeSkillRegistry: ({ name }) => {
      fakeRegistries.splice(0, fakeRegistries.length, ...fakeRegistries.filter((r) => r.name !== name));
      return {};
    },
    searchRegistrySkills: ({ query }) => ({
      skills: fakeRegistries.length ? fakeRegistrySkills.filter((k) => query.toLowerCase().split(/\s+/).every((w) => (k.name + " " + k.description).toLowerCase().includes(w))) : [],
      problems: [],
    }),
    installRegistrySkill: ({ ref, workspace }) => {
      const [registry, name] = ref.includes("/") ? ref.split("/") : ["castor", ref];
      const k = fakeRegistrySkills.find((x) => x.name === name);
      if (!k) throw new ConnectError(`no registry offers that skill: ${ref}`, Code.NotFound);
      const skill = { name, registry, version: k.version, dir: workspace ? `${workspace}/.agents/skills/${name}` : `/home/me/.blitz/skills/${name}`, installed: now() };
      fakeInstalledSkills.splice(0, fakeInstalledSkills.length, ...fakeInstalledSkills.filter((i) => i.dir !== skill.dir), skill);
      return { skill };
    },
    updateRegistrySkills: () => ({ updated: [], problems: [] }),
    uninstallRegistrySkill: ({ name }) => {
      const i = fakeInstalledSkills.findIndex((x) => x.name === name);
      const [skill] = fakeInstalledSkills.splice(i, 1);
      return { skill };
    },
    getInterfaceLanguage: () => ({ locale: "", catalogs: [] }),
    getSignIn: ({ provider }) => ({
      statuses: (provider ? [provider] : ["google", "anthropic", "aws", "azure"]).map((p) => fakeSignIn(p)),
    }),
    signIn: async function* ({ provider }) {
      yield { line: "Your browser has been opened to visit:" };
      await sleep(500);
      yield { line: "https://accounts.example.com/o/oauth2/auth?client_id=blitz", url: "https://accounts.example.com/o/oauth2/auth?client_id=blitz" };
      if (provider === "aws") yield { line: "Then enter the code: WXYZ-1234", code: "WXYZ-1234" };
      await sleep(1500);
      fakeSignedIn.add(provider);
      yield { done: true, status: fakeSignIn(provider) };
    },
    signOut: ({ provider }) => {
      fakeSignedIn.delete(provider);
      return { status: fakeSignIn(provider) };
    },
    listModels: ({ providers, chatOnly }) =>
      providers.length === 1 && providers[0] === "api"
        ? { defaultProvider: "api", providers: [{ provider: "api", ids: fakeGatewayModels, models: fakeGatewayModels.map((id) => ({ id })) }] }
        : chatOnly && !fakeOrg.url
        ? {
            defaultProvider: "gemini",
            providers: [
              {
                provider: "gemini",
                models: [
                  { id: "gemini-4-carbon", hasPrice: true, inputPerMtok: 4, outputPerMtok: 20, capabilities: { tools: true, images: true, thinking: true, context: 2_000_000 } },
                  { id: "gemini-4-argon", hasPrice: true, inputPerMtok: 2, outputPerMtok: 10, capabilities: { tools: true, images: true, thinking: true, context: 1_000_000 } },
                  { id: "gemini-3.8-pro", hasPrice: true, inputPerMtok: 1.25, outputPerMtok: 10, capabilities: { tools: true, images: true, thinking: true, context: 1_000_000 } },
                  { id: "gemini-3.8-flash", hasPrice: true, inputPerMtok: 0.75, outputPerMtok: 3.75, capabilities: { tools: true, images: true, thinking: true, context: 1_000_000 } },
                  { id: "gemini-3.5-flash-lite", hasPrice: true, inputPerMtok: 0.1, outputPerMtok: 0.4, capabilities: { tools: true, images: true, thinking: false, context: 1_000_000 } },
                ],
              },
              {
                provider: "ollama",
                models: [
                  { id: "qwen3:14b", capabilities: { tools: true, images: false, thinking: true, context: 40_000 } },
                  { id: "qwen2.5-coder:7b", capabilities: { tools: true, images: false, thinking: false, context: 32_768 } },
                  { id: "gemma3:12b", capabilities: { tools: false, images: true, thinking: false, context: 128_000 } },
                ],
              },
            ],
            notSetUp: ["anthropic", "openai"],
          }
        : fakeOrg.url && (providers.length === 0 || providers.includes("enterprise"))
        ? {
            defaultProvider: "enterprise",
            providers: [
              fakeOrg.signedIn
                ? { provider: "enterprise", ids: ["fast", "smart"], models: [{ id: "fast", displayName: "Gemini Flash", hasPrice: true, inputPerMtok: 0.75, outputPerMtok: 3.75, capabilities: { tools: true, images: true, thinking: false, context: 1_000_000 } }, { id: "smart", displayName: "Claude Opus", isDefault: true, hasPrice: true, inputPerMtok: 5, outputPerMtok: 25, capabilities: { tools: true, images: true, thinking: true, context: 200_000 } }] }
                : { provider: "enterprise", error: "sign in to your organization to use its models" },
            ],
          }
        : providers.length === 1 && providers[0] === "ollama"
        ? { defaultProvider: "gemini", providers: [fakeParam("ollama") ? { provider: "ollama", ids: ["llama4:8b", "qwen3:14b"] } : { provider: "ollama", error: "connection refused" }] }
        : {
            defaultProvider: "gemini",
            providers: [
              { provider: "gemini", ids: ["gemini-3.5-flash-lite", "gemini-3.8-flash", "gemini-3.8-pro", "gemini-4-argon"] },
              { provider: "anthropic", error: "no API key" },
            ],
          },
    checkApiKey: async ({ provider, key }) => {
      await sleep(400);
      if (!["gemini", "anthropic", "openai"].includes(provider)) throw new ConnectError(`provider "${provider}" takes no API key to check`, Code.InvalidArgument);
      return key.trim().startsWith("bad") ? { works: false, error: "401 Unauthorized: invalid API key" } : { works: true, models: [`${provider}-a`, `${provider}-b`, `${provider}-c`] };
    },
    describeConfig: ({ workspace }) => {
      const c = scopeConfig(workspace);
      return {
        path: configPath(workspace),
        provider: c.provider,
        defaultModel: c.model,
        api: c.api,
        secretStore: "macOS Keychain",
        providers: Object.entries(c.keys).map(([name, keySource]) => {
          const a = c.auth[name];
          return { name, keySource, keyMissing: false, baseUrl: "", model: "", auth: a?.method ?? "", projectId: a?.projectId ?? "", location: a?.location ?? "", profile: a?.profile ?? "" };
        }),
      };
    },
    setApiKey: ({ workspace, provider }) => {
      scopeConfig(workspace).keys[provider] = KeySource.KEYCHAIN;
      return change(workspace);
    },
    secureApiKey: ({ workspace, provider }) => {
      scopeConfig(workspace).keys[provider] = KeySource.KEYCHAIN;
      return change(workspace);
    },
    removeApiKey: ({ workspace, provider }) => {
      scopeConfig(workspace).keys[provider] = workspace ? KeySource.INHERITED : KeySource.NONE;
      return change(workspace);
    },
    setProvider: ({ workspace, provider, defaultModel, key, auth, api }) => {
      const why = providerProblem({ provider, key: key.trim(), auth: auth && { method: auth.method, profile: auth.profile }, api: api && { baseUrl: api.baseUrl, headers: api.headers.map((h) => ({ name: h.name, value: h.value })), tokenScript: api.tokenScript, tokenScriptLanguage: api.tokenScriptLanguage } });
      if (why) throw fakeInvalid(why);
      const method = auth?.method === "api_key" ? "" : (auth?.method ?? "");
      const c = scopeConfig(workspace);
      c.provider = provider;
      c.model = defaultModel;
      if (key) c.keys[provider] = KeySource.KEYCHAIN;
      if (auth) c.auth[provider] = { method, projectId: auth.projectId, location: auth.location, profile: auth.profile };
      if (api) c.api = { ...api, headers: api.headers.map((h) => ({ ...h, value: h.value.includes("{{TOKEN_SCRIPT}}") ? h.value : "", hasValue: true })) };
      return change(workspace);
    },
    setConfigValue: ({ workspace, key, value }) => {
      const why = configValueProblem(key, value);
      if (why) throw fakeInvalid(why);
      if (setFakeVoice(workspace, key, value)) return change(workspace);
      const c = scopeConfig(workspace);
      if (key === "llm.provider") c.provider = value;
      if (key === "blitz.default_model") c.model = value;
      if (key === "ui.task_alerts") fakeAlertsState.value = value;
      if (key.startsWith("profile.")) {
        if (workspace) throw fakeInvalid(`${key} is the person's own setting: a workspace can't change it`);
        if (value.length > 60 || /[\n\r]/.test(value)) throw fakeInvalid(`${key} is one line of at most 60 characters`);
        fakeProfile[key.slice(8) as keyof typeof fakeProfile] = value;
      }
      if (key === "ui.personality") {
        if (value) fakePersonality(value);
        if (workspace) fakePersonalityOf.set(workspace, value || "default");
        else fakePersonalityState.defaultName = value || "default";
      }
      if (key === "audio.model" && workspace) state(workspace).settings.speechModel = value;
      if (key === "images.model" && workspace) state(workspace).settings.imageModel = value;
      if (key.startsWith("search.") && workspace) {
        const s = searchSettingsOf(workspace);
        if (key === "search.enabled") s.enabled = value !== "false";
        if (key === "search.sources") s.sources = value ? value.split(",") : ["files", "documents"];
        if (key === "search.embedding_model") s.embeddingModel = value;
        if (key === "search.enrich") s.enrich = value === "true";
        if (key === "search.enrich_model") s.enrichModel = value;
        if (key === "search.enrich_daily_limit") s.enrichDailyLimit = Number(value) || 200;
        if (key === "search.include_ignored") s.includeIgnored = value === "true";
      }
      if (key === "workers.policy.allow")
        fakePolicy.allow = value
          ? value
              .split(",")
              .map((k) => k.trim())
              .filter((k) => k && k !== "none")
          : [...policyDefaults.allow];
      const limit = { max_concurrent: "maxConcurrent", default_max_turns: "defaultMaxTurns", max_turns: "maxTurns", default_max_cost_usd: "defaultMaxCostUsd", max_cost_usd: "maxCostUsd" }[key.replace("workers.policy.", "")] as "maxConcurrent" | undefined;
      if (key.startsWith("workers.policy.") && limit) fakePolicy[limit] = value ? Number(value) : policyDefaults[limit];
      if (key === "workers.policy.default_timeout") fakePolicy.defaultTimeout = value || policyDefaults.defaultTimeout;
      if (key === "workers.policy.max_timeout") fakePolicy.maxTimeout = value || policyDefaults.maxTimeout;
      return change(workspace);
    },
    getConfigFile: ({ workspace }) => ({ path: configPath(workspace), text: scopeConfig(workspace).text }),
    describePermissions: ({ workspace }) => {
      const p = scopePermissions(workspace);
      const global = scopePermissions("");
      const on = p.defaults === "" ? global.defaults !== "off" : p.defaults === "on";
      return {
        path: configPath(workspace),
        rules: p.rules,
        inherited: workspace ? global.rules : [],
        readOnlyDefaults: p.defaults,
        readOnlyDefaultsOn: on,
        readOnlyCommands: ["ls", "pwd", "cat", "head", "tail", "wc", "stat", "du", "df", "which", "grep", "rg", "diff", "git status", "git log", "git show", "git diff", "git blame", "git rev-parse"],
        readOnlyGuards: ["git … --output", "git … --ext-diff", "rg … --pre"],
      };
    },
    checkPermission: ({ rule, sample }) => {
      const m = /^(shell|bash|write|delete|read|web|search|mcp|skill|agent)\((.+)\)$/i.exec(rule.trim());
      if (!m) return { error: `invalid permission rule "${rule}": use kind(pattern) or a tool name` };
      const kind = m[1].toLowerCase() === "bash" ? "shell" : m[1].toLowerCase();
      const pattern = m[2].trim();
      const form = kind !== "shell" ? (["read", "write", "delete"].includes(kind) ? "path" : "name") : pattern.startsWith("re:") ? "regex" : /[*?]/.test(pattern) ? "glob" : "prefix";
      const tested = !!sample.trim();
      const matches = tested && (form === "prefix" ? sample.trim() === pattern || sample.trim().startsWith(pattern + " ") : sample.includes(pattern.replace(/\*/g, "")));
      const redirect = / >>? *([^ ]+)/.exec(sample)?.[1] ?? "";
      return { rule: `${kind}(${pattern})`, kind, pattern, form, tested, matches, redirect };
    },
    addPermission: ({ workspace, effect, rule }) => {
      if (!["allow", "ask", "deny"].includes(effect)) throw fakeInvalid(`unknown permission effect ${JSON.stringify(effect)} (allow, ask or deny)`);
      const p = scopePermissions(workspace);
      const canonical = rule.trim().replace(/^bash\(/i, "shell(");
      if (!p.rules.some((r) => r.rule === canonical && r.effect === effect)) p.rules.push({ effect, rule: canonical });
      return { ...change(workspace), rule: canonical };
    },
    removePermission: ({ workspace, rule, effect }) => {
      if (effect && !["allow", "ask", "deny"].includes(effect)) throw fakeInvalid(`unknown permission effect ${JSON.stringify(effect)} (allow, ask or deny)`);
      const p = scopePermissions(workspace);
      const before = p.rules.length;
      const want = [rule.trim(), rule.trim().replace(/^bash\(/i, "shell(")];
      p.rules = p.rules.filter((r) => !(want.includes(r.rule) && (!effect || r.effect === effect)));
      return { ...change(workspace), removed: before - p.rules.length };
    },
    setReadOnlyDefaults: ({ workspace, value }) => {
      scopePermissions(workspace).defaults = value;
      return change(workspace);
    },
    checkConfigFile: ({ text }) => {
      const problems: { line: number; column: number; error: boolean; message: string }[] = [];
      text.split("\n").forEach((l, i) => {
        if (/^\s*\[[^\]]*$/.test(l)) problems.push({ line: i + 1, column: 1, error: true, message: "expected '.' or ']' to end table name" });
        else if (/^\s*(temprature|modle)\s*=/.test(l)) problems.push({ line: i + 1, column: 1, error: false, message: `unknown setting ${l.split("=")[0].trim()}` });
      });
      return { problems };
    },
    getSettingsReference: () => ({
      settings: [
        { key: "blitz", type: "table", default: "", doc: "BlitzConfig controls the core behaviour settings." },
        { key: "blitz.temperature", type: "number", default: "0.2", doc: "Temperature is the sampling temperature, unless [model_settings] sets one for the model." },
        { key: "llm", type: "table", default: "", doc: "LLMConfig holds provider configurations for LLM backends." },
        { key: "llm.provider", type: "string", default: '"gemini"', doc: "Provider is the model provider: gemini, openai, anthropic or ollama." },
        { key: "llm.max_retries", type: "integer", default: "3", doc: "MaxRetries is how many times a failed model call is tried again." },
        { key: "llm.stream", type: "boolean", default: "true", doc: "Stream shows the answer as it comes." },
        { key: "llm.gemini", type: "table", default: "", doc: "GeminiConfig configures the Gemini provider." },
        { key: "llm.gemini.model", type: "string", default: '"gemini-4-argon"', doc: "Model is the Gemini model to use." },
        { key: "model_settings.<model>.top_p", type: "number", default: "", doc: "TopP is nucleus sampling: only the most likely tokens up to this probability." },
      ],
    }),
    saveConfigFile: ({ workspace, text }) => {
      if (text.includes("[[")) throw new ConnectError("line 1: expected a table", Code.InvalidArgument);
      scopeConfig(workspace).text = text;
      return { ...change(workspace), warnings: text.includes("sk-") ? ["[llm.anthropic] api_key is written as plain text: set it in Providers & keys to keep it in the keychain"] : [] };
    },
  };
  return impl as ServiceImpl<typeof ConfigService>;
}
