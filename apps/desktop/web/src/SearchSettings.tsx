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

import { useEffect, useState } from "react";
import { mdiAlertCircleOutline, mdiRefresh } from "@mdi/js";
import { config, workspaces } from "./api";
import { message } from "./errors";
import { configChanged } from "./events";
import type { SearchStatus } from "./gen/blitz/v1/workspace_pb";
import { t, tn } from "./i18n";
import { searchSources, toggleSource, type SearchSource } from "./search";
import { Button, Chip, Field, Icon, Switch, useSnackbar } from "./ui/controls";

/** Embedding models to suggest: Gemini's, OpenAI's, and a local one. */
const embeddingModels = ["gemini/text-embedding-004", "openai/text-embedding-3-small", "openai/text-embedding-3-large", "ollama/nomic-embed-text"];

/**
 * Settings › Workspaces › Search (spec_search_035): whether the workspace
 * keeps an index, where a search looks by default, search by meaning (an
 * embedding model) and summaries (a model describing files), each saved in
 * the workspace's settings and applied at once; and how the index is.
 */
export function SearchSettings({ workspace, status, onChanged }: { workspace: string; status?: SearchStatus; onChanged: () => void }) {
  const snack = useSnackbar();
  const settings = status?.settings;
  const [embedRef, setEmbedRef] = useState("");
  const [enrichRef, setEnrichRef] = useState("");
  const [limit, setLimit] = useState("");
  useEffect(() => setEmbedRef(settings?.embeddingModel ?? ""), [settings?.embeddingModel]);
  useEffect(() => setEnrichRef(settings?.enrichModel ?? ""), [settings?.enrichModel]);
  useEffect(() => setLimit(settings?.enrichDailyLimit ? String(settings.enrichDailyLimit) : ""), [settings?.enrichDailyLimit]);

  const set = async (key: string, value: string, done?: string) => {
    try {
      await config.setConfigValue({ workspace, key, value });
      configChanged({ dir: workspace });
      onChanged();
      if (done) snack(done);
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const commit = (key: string, value: string, was: string) => {
    if (value.trim() !== was) set(key, value.trim(), t("desktop.rs.search.saved"));
  };

  if (!settings) return <p className="t-body-sm muted">{t("desktop.search.scanning")}</p>;
  const sources = (settings.sources.length ? settings.sources : ["files", "documents"]) as SearchSource[];
  const items = status?.items ?? {};
  return (
    <div className="stack search-settings" style={{ gap: 12 }}>
      <label className="row" style={{ gap: 12 }}>
        <Switch label={t("desktop.rs.search.enabled")} checked={settings.enabled} onChange={(on) => set("search.enabled", on ? "" : "false", on ? t("desktop.rs.search.on") : t("desktop.rs.search.off"))} />
        <span>{t("desktop.rs.search.enabled")}</span>
      </label>
      <p className="t-body-sm muted">{t("desktop.rs.search.enabled_help")}</p>

      {settings.enabled && (
        <>
          <Field label={t("desktop.rs.search.sources")} supporting={t("desktop.rs.search.sources_help")}>
            {() => (
              <div className="row" style={{ gap: 8, flexWrap: "wrap" }} role="group" aria-label={t("desktop.rs.search.sources")}>
                {searchSources.map((s) => (
                  <Chip key={s} selected={sources.includes(s)} aria-pressed={sources.includes(s)} onClick={() => set("search.sources", toggleSource(sources, s).join(","))}>
                    {t(`desktop.search.source.${s}`)}
                  </Chip>
                ))}
              </div>
            )}
          </Field>

          <label className="row" style={{ gap: 12 }}>
            <Switch label={t("desktop.rs.search.ignored")} checked={settings.includeIgnored} onChange={(on) => set("search.include_ignored", on ? "true" : "", t("desktop.rs.search.saved"))} />
            <span>{t("desktop.rs.search.ignored")}</span>
          </label>
          <p className="t-body-sm muted">{t("desktop.rs.search.ignored_help")}</p>

          <Field label={t("desktop.rs.search.meaning")} supporting={t("desktop.rs.search.meaning_help")}>
            {(id) => (
              <>
                <input
                  id={id}
                  className="input mono"
                  list={`${id}-models`}
                  value={embedRef}
                  placeholder={t("desktop.rs.search.meaning_off")}
                  spellCheck={false}
                  onChange={(e) => setEmbedRef(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()}
                  onBlur={() => commit("search.embedding_model", embedRef, settings.embeddingModel)}
                />
                <datalist id={`${id}-models`}>
                  {embeddingModels.map((m) => (
                    <option key={m} value={m} />
                  ))}
                </datalist>
              </>
            )}
          </Field>

          <label className="row" style={{ gap: 12 }}>
            <Switch label={t("desktop.rs.search.enrich")} checked={settings.enrich} onChange={(on) => set("search.enrich", on ? "true" : "", on ? t("desktop.rs.search.enrich_on") : t("desktop.rs.search.enrich_off"))} />
            <span>{t("desktop.rs.search.enrich")}</span>
          </label>
          <p className="t-body-sm muted">{t("desktop.rs.search.enrich_help")}</p>
          {settings.enrich && (
            <div className="pairs">
              <Field label={t("desktop.rs.search.enrich_model")}>
                {(id) => (
                  <input
                    id={id}
                    className="input mono"
                    value={enrichRef}
                    placeholder={t("desktop.rs.search.enrich_model_default")}
                    spellCheck={false}
                    onChange={(e) => setEnrichRef(e.target.value)}
                    onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()}
                    onBlur={() => commit("search.enrich_model", enrichRef, settings.enrichModel)}
                  />
                )}
              </Field>
              <Field label={t("desktop.rs.search.enrich_limit")}>
                {(id) => (
                  <input
                    id={id}
                    className="input"
                    type="number"
                    min={0}
                    value={limit}
                    onChange={(e) => setLimit(e.target.value)}
                    onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()}
                    onBlur={() => commit("search.enrich_daily_limit", limit, String(settings.enrichDailyLimit || ""))}
                  />
                )}
              </Field>
            </div>
          )}

          {status?.problem && (
            <p className="card warn row t-body-sm" role="alert">
              <Icon path={mdiAlertCircleOutline} size="sm" />
              <span>{status.problem}</span>
            </p>
          )}
          <div className="row" style={{ gap: 12, alignItems: "center" }}>
            <span className="t-body-sm muted" style={{ flex: 1 }}>
              {!status?.enabled || status.scanning || !status.lastScan
                ? t("desktop.search.scanning")
                : [
                    ...searchSources.map((s) => `${t(`desktop.search.source.${s}`)} ${items[s] ?? 0}`),
                    ...(status.embeddingModel ? [t("desktop.search.embedded", { count: status.embedded })] : []),
                    ...(status.enriched ? [t("desktop.search.enriched", { count: status.enriched })] : []),
                    ...(status.unreadable ? [tn("desktop.rs.search.unreadable", status.unreadable)] : []),
                  ].join(" · ")}
            </span>
            <Button small icon={mdiRefresh} disabled={!status?.enabled || status.scanning} onClick={() => workspaces.reindex({ workspace }).then(onChanged, (e) => snack(message(e), { error: true }))}>
              {t("desktop.search.reindex")}
            </Button>
          </div>
        </>
      )}
    </div>
  );
}
