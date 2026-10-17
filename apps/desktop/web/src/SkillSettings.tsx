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

import { useCallback, useEffect, useState } from "react";
import { mdiDeleteOutline, mdiDownload, mdiFileDocumentOutline, mdiFolderOpenOutline, mdiKeyOutline, mdiMagnify, mdiPlus, mdiRefresh } from "@mdi/js";
import { config, workspaces } from "./api";
import { revealPath } from "./desktop";
import { openFile } from "./events";
import { message } from "./errors";
import type { InstalledSkill, RegistrySkill, SkillRegistry } from "./gen/blitz/v1/config_pb";
import type { SkillInfo } from "./gen/blitz/v1/workspace_pb";
import { t, tn } from "./i18n";
import { skillFile, skillWhere, skillWhereOrder } from "./skillList";
import { useApp } from "./state";
import { Button, Icon, IconButton, useSnackbar } from "./ui/controls";

/**
 * Settings › Skills (spec_skills_013 SK-96): the skills the active
 * workspace finds (its .agents/skills, the user's, the registries' and
 * plugins'), the skill registries, such as Castor, the skills they offer,
 * and those installed from them. A skill installs for the user or into
 * the active workspace; open sessions see it at once. onOpen: a skill's
 * file was opened in the editor (Settings closes).
 */
export function SkillSettings({ onOpen }: { onOpen?: () => void }) {
  const snack = useSnackbar();
  const { prefs } = useApp();
  const active = prefs.workspaces.find((w) => w.dir === prefs.active && w.open)?.dir ?? "";
  const [registries, setRegistries] = useState<SkillRegistry[]>([]);
  const [installed, setInstalled] = useState<InstalledSkill[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [url, setUrl] = useState("");
  const [token, setToken] = useState("");
  const [query, setQuery] = useState("");
  const [found, setFound] = useState<RegistrySkill[] | null>(null);
  const [problems, setProblems] = useState<string[]>([]);
  const [scope, setScope] = useState("");

  const load = useCallback(async () => {
    try {
      const r = await config.listSkillRegistries({});
      setRegistries(r.registries);
      setInstalled(r.installed);
      setError("");
    } catch (e) {
      setError(message(e));
    }
  }, []);
  useEffect(() => void load(), [load]);

  // Runs a change, says how it went, and reloads the lists.
  const act = async (run: () => Promise<string>) => {
    setBusy(true);
    try {
      snack(await run());
      await load();
    } catch (e) {
      snack(message(e), { error: true });
    } finally {
      setBusy(false);
    }
  };

  const add = () =>
    act(async () => {
      const r = await config.addSkillRegistry({ url: url.trim(), token: token.trim() });
      setUrl("");
      setToken("");
      return tn("desktop.skills.added", r.skills, { name: r.registry?.name ?? "" });
    });

  const search = async () => {
    setBusy(true);
    try {
      const r = await config.searchRegistrySkills({ query });
      setFound(r.skills);
      setProblems(r.problems);
    } catch (e) {
      snack(message(e), { error: true });
    } finally {
      setBusy(false);
    }
  };

  const install = (s: RegistrySkill) =>
    act(async () => {
      const r = await config.installRegistrySkill({ ref: `${s.registry}/${s.name}`, workspace: scope });
      return t("desktop.skills.installed", { name: s.name, version: r.skill?.version ?? s.version });
    });

  return (
    <div className="stack skill-settings" style={{ gap: 16 }}>
      <p className="t-body-sm muted">{t("desktop.skills.intro")}</p>
      {error && <p className="error-text">{error}</p>}
      <FoundSkills workspace={active} registryDirs={installed.map((i) => i.dir)} onOpen={onOpen} />

      <section className="stack" style={{ gap: 8 }}>
        <span className="t-title-sm">{t("desktop.skills.registries")}</span>
        {registries.length === 0 ? (
          <p className="t-body-sm muted">{t("desktop.skills.no_registries")}</p>
        ) : (
          <div className="list">
            {registries.map((r) => (
              <div key={r.name} className="rule">
                <strong>{r.name}</strong>
                <code className="ellipsis spacer" title={r.url}>
                  {r.url}
                </code>
                {r.hasToken && (
                  <span title={t("desktop.skills.has_token")} aria-label={t("desktop.skills.has_token")}>
                    <Icon path={mdiKeyOutline} size="sm" />
                  </span>
                )}
                <IconButton icon={mdiDeleteOutline} label={t("desktop.skills.remove", { name: r.name })} small disabled={busy} onClick={() => act(async () => (await config.removeSkillRegistry({ name: r.name }), t("desktop.skills.removed", { name: r.name })))} />
              </div>
            ))}
          </div>
        )}
        <div className="card stack" style={{ gap: 8 }}>
          <label className="field">
            <span className="t-label">{t("desktop.skills.url")}</span>
            <input className="input mono" value={url} disabled={busy} onChange={(e) => setUrl(e.target.value)} placeholder={t("desktop.skills.url_placeholder")} spellCheck={false} />
          </label>
          <label className="field">
            <span className="t-label">{t("desktop.skills.token")}</span>
            <input className="input mono" type="password" value={token} disabled={busy} onChange={(e) => setToken(e.target.value)} placeholder={t("desktop.skills.token_placeholder")} autoComplete="off" />
          </label>
          <div className="row" style={{ justifyContent: "flex-end" }}>
            <Button small variant="filled" icon={mdiPlus} disabled={busy || !url.trim()} onClick={() => void add()}>
              {t("desktop.skills.add")}
            </Button>
          </div>
        </div>
      </section>

      {registries.length > 0 && (
        <section className="stack" style={{ gap: 8 }}>
          <span className="t-title-sm">{t("desktop.skills.find")}</span>
          <div className="row skill-search" style={{ gap: 8 }}>
            <input
              className="input"
              value={query}
              disabled={busy}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && void search()}
              placeholder={t("desktop.skills.search_placeholder")}
              aria-label={t("desktop.skills.find")}
            />
            <select className="select" value={scope} onChange={(e) => setScope(e.target.value)} aria-label={t("desktop.skills.install_for")}>
              <option value="">{t("desktop.skills.for_me")}</option>
              {active && <option value={active}>{t("desktop.skills.for_workspace", { name: active.slice(active.lastIndexOf("/") + 1) })}</option>}
            </select>
            <Button small icon={mdiMagnify} disabled={busy} onClick={() => void search()}>
              {t("desktop.skills.search")}
            </Button>
          </div>
          {problems.map((p) => (
            <p key={p} className="card warn t-body-sm">
              {p}
            </p>
          ))}
          {found && found.length === 0 && <p className="t-body-sm muted">{t("desktop.skills.none_found")}</p>}
          {found && found.length > 0 && (
            <div className="list">
              {found.map((s) => (
                <div key={`${s.registry}/${s.name}`} className="rule">
                  <div className="stack spacer" style={{ gap: 2, minWidth: 0 }}>
                    <span>
                      <strong>{s.name}</strong> <span className="t-body-sm muted">{s.version} · {s.registry}</span>
                    </span>
                    <span className="t-body-sm muted ellipsis">{s.description}</span>
                  </div>
                  <Button small icon={mdiDownload} disabled={busy} onClick={() => void install(s)}>
                    {t("desktop.skills.install")}
                  </Button>
                </div>
              ))}
            </div>
          )}
        </section>
      )}

      {installed.length > 0 && (
        <section className="stack" style={{ gap: 8 }}>
          <div className="row" style={{ gap: 8 }}>
            <span className="t-title-sm spacer">{t("desktop.skills.installed_title")}</span>
            <Button
              small
              icon={mdiRefresh}
              disabled={busy}
              onClick={() =>
                act(async () => {
                  const r = await config.updateRegistrySkills({});
                  if (r.problems.length) throw new Error(r.problems.join("\n"));
                  return r.updated.length ? tn("desktop.skills.updated", r.updated.length) : t("desktop.skills.up_to_date");
                })
              }
            >
              {t("desktop.skills.update_all")}
            </Button>
          </div>
          <div className="list">
            {installed.map((i) => (
              <div key={i.dir} className="rule">
                <div className="stack spacer" style={{ gap: 2, minWidth: 0 }}>
                  <span>
                    <strong>{i.name}</strong> <span className="t-body-sm muted">{i.version} · {i.registry}</span>
                  </span>
                  <code className="t-body-sm ellipsis" title={i.dir}>
                    {i.dir}
                  </code>
                </div>
                <IconButton
                  icon={mdiDeleteOutline}
                  label={t("desktop.skills.uninstall", { name: i.name })}
                  small
                  disabled={busy}
                  onClick={() => act(async () => (await config.uninstallRegistrySkill({ name: i.name, workspace: workspaceOf(i.dir) }), t("desktop.skills.uninstalled", { name: i.name })))}
                />
              </div>
            ))}
          </div>
        </section>
      )}
    </div>
  );
}

/** The workspace a project skill's folder is in (<dir>/.agents/skills/<name>), else "". */
export function workspaceOf(dir: string): string {
  const m = /^(.*)\/\.agents\/skills\/[^/]+$/.exec(dir);
  return m ? m[1] : "";
}

/**
 * The skills the workspace finds, by where they come from (SK-96a): its
 * own open in the editor, the others show in the file manager.
 */
function FoundSkills({ workspace, registryDirs, onOpen }: { workspace: string; registryDirs: string[]; onOpen?: () => void }) {
  const snack = useSnackbar();
  const [skills, setSkills] = useState<SkillInfo[] | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    if (!workspace) return;
    let live = true;
    workspaces.listSkills({ workspace }).then(
      (r) => live && (setSkills(r.skills), setError("")),
      (e) => live && setError(message(e)),
    );
    return () => {
      live = false;
    };
  }, [workspace]);
  const groups = skillWhereOrder
    .map((where) => ({ where, list: (skills ?? []).filter((s) => skillWhere(s.dir, workspace, registryDirs) === where) }))
    .filter((g) => g.list.length > 0);
  return (
    <section className="stack" style={{ gap: 12 }}>
      <span className="t-title-sm">{t("desktop.skills.found")}</span>
      {!workspace ? (
        <p className="t-body-sm muted">{t("desktop.skills.found.no_workspace")}</p>
      ) : error ? (
        <p className="error-text">{error}</p>
      ) : skills && groups.length === 0 ? (
        <p className="t-body-sm muted">{t("desktop.skills.found.none")}</p>
      ) : (
        groups.map((g) => (
          <div key={g.where} className="stack" style={{ gap: 4 }}>
            <span className="t-label muted">{t(`desktop.skills.where.${g.where}`)}</span>
            <div className="list">
              {g.list.map((s) => {
                const file = skillFile(s.dir, workspace);
                return (
                  <div key={s.dir || s.name} className="rule">
                    <div className="stack spacer" style={{ gap: 2, minWidth: 0 }}>
                      <span>
                        <strong>{s.name}</strong> {s.version && <span className="t-body-sm muted">{s.version}</span>}
                      </span>
                      {s.description && <span className="t-body-sm muted ellipsis" title={s.description}>{s.description}</span>}
                      {s.dir && (
                        <code className="t-body-sm ellipsis" title={s.dir}>
                          {s.dir}
                        </code>
                      )}
                    </div>
                    {file ? (
                      <IconButton
                        icon={mdiFileDocumentOutline}
                        label={t("desktop.skills.open", { name: s.name })}
                        small
                        onClick={() => {
                          openFile({ dir: workspace, path: file });
                          onOpen?.();
                        }}
                      />
                    ) : (
                      s.dir && <IconButton icon={mdiFolderOpenOutline} label={t("desktop.skills.reveal", { name: s.name })} small onClick={() => void revealPath(s.dir).catch((e) => snack(message(e), { error: true }))} />
                    )}
                  </div>
                );
              })}
            </div>
          </div>
        ))
      )}
    </section>
  );
}
