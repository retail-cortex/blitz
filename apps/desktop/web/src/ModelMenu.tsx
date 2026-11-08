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

import { useState } from "react";
import { mdiAlertCircleOutline, mdiBrain, mdiCogOutline, mdiPinOutline, mdiRefresh, mdiStarOutline } from "@mdi/js";
import { Icon, Menu, useSnackbar, type MenuEntry } from "@blitz/webui";
import { config, sessions, workspaces } from "./api";
import { message } from "./errors";
import { configChanged, openSettings } from "./events";
import type { ListModelsResponse, ListedModel } from "./gen/blitz/v1/config_pb";
import type { AgentInfo } from "./gen/blitz/v1/workspace_pb";
import { t } from "./i18n";
import { modelNotice } from "./slashCommands";
import { publishStatus } from "./status";

/** How providers are named in the menu. */
const providerNames: Record<string, string> = { gemini: "Gemini", anthropic: "Anthropic", openai: "OpenAI", ollama: "Ollama", bedrock: "Bedrock", azure: "Azure" };

/** A provider's name as people know it. */
export function providerName(p: string): string {
  if (p === "enterprise") return t("desktop.models.org");
  if (p === "api") return t("desktop.keys.provider.api");
  return providerNames[p] ?? p;
}

/** With this many models or more, the menu has a filter (spec_061 MOD-12). */
const searchAt = 12;

/** A model's line: its price in and out per million tokens, its context, and "no tools" when it can't call them. */
export function modelDetail(m: ListedModel): string {
  const parts: string[] = [];
  if (m.displayName && m.displayName !== m.id) parts.push(m.id);
  if (m.hasPrice) parts.push(t("desktop.models.price", { input: price(m.inputPerMtok), output: price(m.outputPerMtok) }));
  const c = m.capabilities;
  if (c?.context) parts.push(t("desktop.models.context", { tokens: contextText(c.context) }));
  if (c && c.context && !c.tools) parts.push(t("desktop.models.no_tools"));
  return parts.join(" · ");
}

const price = (usd: number) => `$${Number(usd.toFixed(2))}`;
const contextText = (n: number) => (n >= 1_000_000 ? `${Number((n / 1_000_000).toFixed(1))}M` : `${Math.round(n / 1000)}K`);

/** A listed model as a reference naming its provider. */
export const refOf = (provider: string, m: ListedModel) => `${provider}/${m.id}`;

/**
 * The model menu's entries (spec_061 MOD-12): each provider's models under
 * its heading, the current one checked; a provider that couldn't list them
 * says why; the active agent's pin; then Use for new chats, Refresh and the
 * providers not set up.
 */
export function modelEntries(
  list: ListModelsResponse | null,
  current: string,
  pin: { agent: string; model: string } | null,
  act: { choose: (ref: string) => void; makeDefault: () => void; refresh: () => void; setUp: (provider: string) => void },
): MenuEntry[] {
  const out: MenuEntry[] = [];
  if (pin) out.push({ label: t("desktop.models.pinned", { agent: pin.agent, model: pin.model }), icon: mdiPinOutline, disabled: true, onSelect: () => {} });
  if (!list) return [...out, { heading: t("desktop.models.loading") }];
  for (const p of list.providers) {
    out.push({ heading: providerName(p.provider) });
    if (p.error) out.push({ label: p.error, icon: mdiAlertCircleOutline, disabled: true, onSelect: () => {} });
    for (const m of p.models) {
      const ref = refOf(p.provider, m);
      const on = ref === current || m.id === current;
      out.push({ label: m.displayName || m.id, detail: modelDetail(m), icon: m.isDefault ? mdiStarOutline : mdiBrain, on, onSelect: () => act.choose(ref) });
    }
  }
  out.push("divider", { label: t("desktop.models.default_for_new"), icon: mdiStarOutline, onSelect: act.makeDefault });
  out.push({ label: t("desktop.models.refresh"), icon: mdiRefresh, onSelect: act.refresh });
  for (const p of list.notSetUp) out.push({ label: t("desktop.models.set_up", { provider: providerName(p) }), icon: mdiCogOutline, onSelect: () => act.setUp(p) });
  return out;
}

/**
 * The status bar's model button and menu (spec_061 MOD-10..14): the
 * conversation's model, else the workspace's; choosing one moves the
 * conversation to it (saying what the switch costs), or before a message
 * the workspace.
 */
export function ModelMenu({ dir, model, provider, sessionId }: { dir: string; model: string; provider: string; sessionId?: string }) {
  const snack = useSnackbar();
  const [list, setList] = useState<ListModelsResponse | null>(null);
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const current = model.includes("/") || !provider ? model : `${provider}/${model}`;
  const load = (refresh = false) => {
    if (refresh) setList(null);
    config.listModels({ workspace: dir, chatOnly: true, menu: true, refresh }).then(setList, (e) => snack(message(e), { error: true }));
    workspaces.listAgents({ workspace: dir }).then(
      (r) => setAgents(r.agents),
      () => {},
    );
  };
  const active = agents.find((a) => a.active);
  const pin = active?.pinnedModel ? { agent: active.displayName || active.name, model: active.pinnedModel } : null;
  const choose = async (ref: string) => {
    try {
      if (sessionId) {
        const r = await sessions.setSessionModel({ workspace: dir, sessionId, model: ref });
        publishStatus(dir, { conversationModel: ref });
        snack(modelNotice(r.preview));
      } else {
        await workspaces.setModel({ workspace: dir, ref });
        configChanged({ dir });
        snack(t("desktop.model.now", { model: ref }));
      }
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const makeDefault = async () => {
    try {
      await workspaces.setModel({ workspace: dir, ref: current });
      configChanged({ dir });
      snack(t("desktop.models.default_set", { model: current }));
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  return (
    <Menu
      placement="up end"
      className="wide-menu model-menu"
      searchAt={searchAt}
      trigger={(p) => (
        <button className="sb-item sb-button sb-model" title={t("desktop.models.title", { model: current })} {...p} onClick={() => (load(), p.onClick())}>
          <Icon path={mdiBrain} size="sm" />
          <span className="ellipsis">{model}</span>
        </button>
      )}
      items={modelEntries(list, current, pin, { choose: (r) => void choose(r), makeDefault: () => void makeDefault(), refresh: () => load(true), setUp: () => openSettings({ section: "providers" }) })}
    />
  );
}
