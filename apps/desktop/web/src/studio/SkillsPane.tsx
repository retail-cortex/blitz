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
import { createPortal } from "react-dom";
import {
  mdiAlertCircleOutline,
  mdiArrowLeft,
  mdiBookOpenVariant,
  mdiClose,
  mdiCodeBraces,
  mdiContentCopy,
  mdiContentSaveOutline,
  mdiDeleteOutline,
  mdiFileDocumentMultipleOutline,
  mdiLinkVariant,
  mdiPlus,
  mdiPuzzleOutline,
  mdiRefresh,
  mdiShieldAlertOutline,
  mdiSitemapOutline,
  mdiUndoVariant,
} from "@mdi/js";
import { ToolPicker } from "../AgentEditor";
import { workspaces } from "../api";
import { message } from "../errors";
import { t } from "../i18n";
import type { SkillFile, SkillInfo } from "../gen/blitz/v1/workspace_pb";
import { SuggestTextarea } from "../SecretSuggest";
import { SkillReviewDialog } from "../SkillReview";
import { forgetSkills } from "../skillMention";
import { Button, Chip, Dialog, Field, Icon, IconButton, Tabs, useSnackbar } from "../ui/controls";
import { DraftButton } from "./Draft";
import { SkillFiles } from "./SkillFiles";
import { emptySkillForm, formFromSkill, kindOf, sameSkillForm, skillName, specFromForm, validSkillName, type SkillForm, type SkillKind } from "./skillForm";

/** Each kind's icon (SK-97). */
const kindIcons: Record<SkillKind, string> = { simple: mdiPuzzleOutline, references: mdiBookOpenVariant, scripts: mdiCodeBraces, meta: mdiSitemapOutline };

/** What slides in over the list: a skill (by name), or a new one (""). */
type Open = { name: string } | null;

type Tab = "general" | "references" | "scripts";

/**
 * The workspace's skills (Studio › Skills, spec_skills_013 SK-100–SK-103):
 * its own, with their kind, fill the pane, with New and **Add from…**
 * (copy any skill the workspace sees, or reference it in the open meta
 * skill). A skill's form slides in over the list, as an agent's does,
 * with three tabs: General (name, description, tools, agents, the skills
 * it combines, its instructions with Draft), References and Scripts (its
 * files). Back, Cancel and Save return to the list; with unsaved edits,
 * leaving asks first (onDirty tells the drawer too).
 */
export function SkillsPane({ workspace, onDirty }: { workspace: string; onDirty?: (dirty: boolean) => void }) {
  const snack = useSnackbar();
  const [all, setAll] = useState<SkillInfo[]>([]);
  const [error, setError] = useState("");
  const [open, setOpen] = useState<Open>(null);
  const [file, setFile] = useState<SkillFile | null>(null);
  const [form, setForm] = useState<SkillForm>(emptySkillForm);
  const [base, setBase] = useState<SkillForm>(emptySkillForm);
  const [named, setNamed] = useState(false); // the name was typed, not made from the description
  const [tab, setTab] = useState<Tab>("general");
  const [problems, setProblems] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [filesDirty, setFilesDirty] = useState(false);
  // A reference or script open in its tab: the file's buttons stand in for the skill's.
  const [fileOpen, setFileOpen] = useState(false);
  const [pending, setPending] = useState<(() => void) | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [adding, setAdding] = useState(false);
  const [reviewing, setReviewing] = useState(false);
  const [tools, setTools] = useState<string[]>([]);
  const [agents, setAgents] = useState<string[]>([]);

  const refresh = useCallback(async () => {
    forgetSkills(workspace); // the composer's \skill list asks again
    try {
      setAll((await workspaces.listSkills({ workspace })).skills);
      setError("");
    } catch (e) {
      setError(message(e));
    }
  }, [workspace]);
  useEffect(() => {
    void refresh();
    workspaces.listTools({ workspace, all: true }).then(
      (r) => setTools(r.tools.map((x) => x.name)),
      () => {},
    );
    workspaces.listAgents({ workspace }).then(
      (r) => setAgents(r.agents.map((a) => a.name)),
      () => {},
    );
  }, [workspace, refresh]);

  const own = all.filter((s) => s.project);
  const dirty = open !== null && (!sameSkillForm(form, base) || filesDirty);
  useEffect(() => onDirty?.(dirty), [dirty, onDirty]);

  const leave = (go: () => void) => (dirty ? setPending(() => go) : go());
  const load = (f: SkillFile) => {
    const loaded = formFromSkill(f);
    setFile(f);
    setForm(loaded);
    setBase(loaded);
  };
  const show = (name: string) =>
    leave(async () => {
      try {
        const r = await workspaces.getSkillFile({ workspace, name });
        if (!r.file) return;
        load(r.file);
        setOpen({ name });
        setNamed(true);
        setTab("general");
        setProblems([]);
      } catch (e) {
        snack(message(e), { error: true });
      }
    });
  const startNew = () =>
    leave(() => {
      setFile(null);
      setForm(emptySkillForm);
      setBase(emptySkillForm);
      setNamed(false);
      setOpen({ name: "" });
      setTab("general");
      setProblems([]);
    });
  const close = () =>
    leave(() => {
      setOpen(null);
      setFile(null);
      setFilesDirty(false);
      void refresh();
    });

  const set = <K extends keyof SkillForm>(k: K, v: SkillForm[K]) => setForm((f) => ({ ...f, [k]: v }));
  const nameError = form.name && !validSkillName(form.name.trim()) ? t("desktop.skills.invalid_name") : undefined;
  const save = async () => {
    if (busy) return;
    setBusy(true);
    setProblems([]);
    try {
      const r = await workspaces.saveSkill({ workspace, previousName: open?.name ?? "", hash: file?.hash ?? "", skill: specFromForm(form) });
      if (r.problems.length || !r.file) {
        setProblems(r.problems);
        return;
      }
      snack(t("desktop.skills.saved", { name: r.file.spec?.name ?? "" }));
      setOpen(null);
      setFile(null);
      setFilesDirty(false);
      await refresh();
    } catch (e) {
      setProblems([message(e)]);
    } finally {
      setBusy(false);
    }
  };
  const remove = async () => {
    setDeleting(false);
    const name = open?.name ?? "";
    try {
      await workspaces.deleteSkill({ workspace, name });
      snack(t("desktop.skills.deleted", { name }));
      setOpen(null);
      setFile(null);
      await refresh();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };

  const saved = open !== null && open.name !== "" && file !== null;
  const title = open?.name === "" && !form.name ? t("desktop.skills.new") : form.name || open?.name || "";
  const where = file?.path || (form.name ? `.agents/skills/${form.name.trim()}/SKILL.md` : "");
  return (
    <div className={`slide-pane skills-pane ${open ? "is-open" : ""}`}>
      <aside className="slide-list" inert={open !== null}>
        <div className="row side-head">
          <span className="t-title-sm spacer">{t("desktop.skills.project_title")}</span>
          <IconButton icon={mdiContentCopy} label={t("desktop.skills.add_from")} small onClick={() => setAdding(true)} />
          <IconButton icon={mdiPlus} label={t("desktop.skills.new")} small onClick={startNew} />
          <IconButton icon={mdiRefresh} label={t("desktop.refresh")} small onClick={() => void refresh()} />
        </div>
        <p className="t-body-sm muted agent-dir">{t("desktop.workers.in", { path: ".agents/skills" })}</p>
        {error && (
          <div className="card error row small-card">
            <Icon path={mdiAlertCircleOutline} size="sm" /> {error}
          </div>
        )}
        {own.length === 0 && !error && (
          <p className="muted t-body-sm">{t("desktop.skills.none")}</p>
        )}
        <div className="list">
          {own.map((s) => {
            const k = kindOf(s);
            const broken = s.blocked.length > 0 && s.scripts.length > 0;
            return (
              <button key={s.name} className="list-item" title={s.dir} onClick={() => show(s.name)}>
                <Icon path={broken ? mdiAlertCircleOutline : s.needsReview ? mdiShieldAlertOutline : kindIcons[k]} className={broken ? "error-text" : s.needsReview ? "warn-text" : undefined} />
                <span className="lines">
                  <span className="ellipsis">{s.name}</span>
                  <small className="ellipsis">{[s.needsReview ? t("desktop.review.pending") : "", t(`desktop.skills.kind.${k}`), s.description].filter(Boolean).join(" · ")}</small>
                </span>
              </button>
            );
          })}
        </div>
      </aside>
      <section className="slide-detail" inert={open === null}>
        {open && (
          <>
            <div className="row slide-back">
              <IconButton icon={mdiArrowLeft} label={t("desktop.skills.back")} onClick={close} />
              <h2 className="t-headline spacer ellipsis">{title}</h2>
              {dirty && <span className="t-body-sm muted">{t("desktop.agents.unsaved")}</span>}
            </div>
            <Tabs
              value={tab}
              label={t("desktop.skills.tabs")}
              onChange={(v) => {
                setFileOpen(false);
                setTab(v);
              }}
              tabs={[
                { value: "general", label: t("desktop.skills.tab.general") },
                { value: "references", label: t("desktop.skills.tab.references"), count: file?.skill?.references.length },
                { value: "scripts", label: t("desktop.skills.tab.scripts"), count: file?.skill?.scripts.length },
              ]}
            />
            {file?.skill?.needsReview && (
              <div className="card warn row wrap skill-review-banner" style={{ gap: 8 }}>
                <Icon path={mdiShieldAlertOutline} size="sm" />
                <span className="t-body-sm spacer">{t("desktop.review.banner")}</span>
                <Button small variant="tonal" onClick={() => setReviewing(true)}>
                  {t("desktop.review.open")}
                </Button>
              </div>
            )}
            {reviewing && file?.skill && (
              <SkillReviewDialog
                name={file.skill.name}
                review={{ dir: file.skill.dir, hash: file.skill.hash, scripts: file.skill.scripts.map((s) => ({ name: s.name, language: s.language, source: s.source, deps: s.deps })) }}
                onDone={(trusted) => {
                  setReviewing(false);
                  if (trusted) void workspaces.getSkillFile({ workspace, name: file.skill!.name }).then((r) => r.file && setFile(r.file), () => {});
                }}
              />
            )}
            {tab === "general" && (
              <div className="worker agent-form skill-form">
                <div className="pair">
                  <Field label={t("desktop.skills.description")} supporting={t("desktop.skills.description_hint")}>
                    {(id) => (
                      <input
                        id={id}
                        className="input"
                        value={form.description}
                        onChange={(e) => {
                          const v = e.target.value;
                          setForm((f) => ({ ...f, description: v, name: named ? f.name : skillName(v.split(/[.:]/)[0]) }));
                        }}
                      />
                    )}
                  </Field>
                  <Field label={t("desktop.skills.name")} error={nameError} supporting={t("desktop.skills.name_hint")}>
                    {(id) => (
                      <input
                        id={id}
                        className="input mono"
                        value={form.name}
                        onChange={(e) => {
                          setNamed(true);
                          set("name", e.target.value);
                        }}
                      />
                    )}
                  </Field>
                </div>
                <div className="row field-tools">
                  <span className="spacer" />
                  <DraftButton workspace={workspace} kind="skill" name={form.name} description={form.description} current={form.instructions} onUse={(v) => set("instructions", v)} />
                </div>
                <Field label={t("desktop.skills.instructions")} supporting={t("desktop.skills.instructions_hint", { name: form.name || "name" })}>
                  {(id) => <SuggestTextarea id={id} className="input agent-prompt" value={form.instructions} rows={10} spellCheck={false} placeholder={t("desktop.skills.instructions_placeholder")} onValue={(v) => set("instructions", v)} workspace={workspace} />}
                </Field>
                <div className="stack" style={{ gap: 6 }}>
                  <span className="t-label">{t("desktop.skills.members")}</span>
                  <span className="t-body-sm muted">{t("desktop.skills.members_hint")}</span>
                  <div className="row wrap" style={{ gap: 6 }}>
                    {form.members.map((m) => (
                      <Chip key={m} icon={mdiLinkVariant} onClick={() => set("members", form.members.filter((x) => x !== m))} aria-label={t("desktop.skills.member_remove", { name: m })}>
                        {m} <Icon path={mdiClose} size="sm" />
                      </Chip>
                    ))}
                    <select
                      className="select skill-member-add"
                      value=""
                      aria-label={t("desktop.skills.member_add")}
                      onChange={(e) => e.target.value && set("members", [...form.members, e.target.value])}
                    >
                      <option value="">{t("desktop.skills.member_add")}</option>
                      {all
                        .filter((s) => s.name !== form.name && !form.members.includes(s.name))
                        .map((s) => (
                          <option key={s.name} value={s.name}>
                            {s.name}
                          </option>
                        ))}
                    </select>
                  </div>
                </div>
                <div className="stack" style={{ gap: 6 }}>
                  <span className="t-label">{t("desktop.skills.agents")}</span>
                  <span className="t-body-sm muted">{form.agents.length ? t("desktop.skills.agents_some") : t("desktop.skills.agents_all")}</span>
                  <div className="row wrap" style={{ gap: 6 }}>
                    {[...new Set([...agents, ...form.agents])].map((a) => {
                      const on = form.agents.includes(a);
                      return (
                        <Chip key={a} selected={on} aria-pressed={on} onClick={() => set("agents", on ? form.agents.filter((x) => x !== a) : [...form.agents, a])}>
                          {a}
                        </Chip>
                      );
                    })}
                  </div>
                </div>
                <div className="stack" style={{ gap: 6 }}>
                  <span className="t-label">{t("desktop.skills.tools")}</span>
                  <span className="t-body-sm muted">{t("desktop.skills.tools_hint")}</span>
                  <ToolPicker offered={tools} chosen={form.tools} onChange={(v) => set("tools", v)} />
                </div>
                {problems.length > 0 && (
                  <div className="card error stack" role="alert" style={{ gap: 4 }}>
                    {problems.map((p) => (
                      <span key={p} className="row t-body-sm" style={{ gap: 6 }}>
                        <Icon path={mdiAlertCircleOutline} size="sm" /> {p}
                      </span>
                    ))}
                  </div>
                )}
              </div>
            )}
            {tab !== "general" && !saved && (
              <p className="t-body-sm muted">
                <Icon path={mdiFileDocumentMultipleOutline} size="sm" /> {t("desktop.skills.save_first")}
              </p>
            )}
            {tab !== "general" && saved && file && <SkillFiles key={tab} workspace={workspace} file={file} folder={tab} onFile={setFile} onDirty={setFilesDirty} onOpen={setFileOpen} />}
            {!(tab !== "general" && fileOpen) && (
            <div className="row wrap agent-actions slide-actions">
              <span className="t-body-sm muted spacer ellipsis" title={where}>
                {where && t("desktop.agents.where", { path: where })}
              </span>
              {saved && (
                <Button danger icon={mdiDeleteOutline} onClick={() => setDeleting(true)}>
                  {t("desktop.skills.delete")}
                </Button>
              )}
              {saved && !sameSkillForm(form, base) && (
                <Button icon={mdiUndoVariant} onClick={() => setForm(base)}>
                  {t("desktop.agents.revert")}
                </Button>
              )}
              <Button onClick={close}>{t("desktop.cancel")}</Button>
              <Button variant="filled" icon={mdiContentSaveOutline} disabled={busy || !form.name.trim() || !!nameError || (saved && sameSkillForm(form, base))} onClick={() => void save()}>
                {t("desktop.skills.save")}
              </Button>
            </div>
            )}
          </>
        )}
      </section>
      {adding && (
        <AddSkill
          workspace={workspace}
          skills={all}
          meta={open !== null ? form.name || t("desktop.skills.new") : ""}
          onReference={(name) => {
            set("members", [...new Set([...form.members, name])]);
            setAdding(false);
            setTab("general");
          }}
          onCopied={(f) => {
            setAdding(false);
            void refresh().then(() => show(f.spec?.name ?? ""));
          }}
          onClose={() => setAdding(false)}
        />
      )}
      {deleting &&
        createPortal(
          <Dialog
            title={t("desktop.skills.delete_title", { name: open?.name ?? "" })}
            icon={mdiDeleteOutline}
            onClose={() => setDeleting(false)}
            footer={
              <>
                <Button onClick={() => setDeleting(false)}>{t("desktop.cancel")}</Button>
                <Button variant="filled" danger onClick={() => void remove()}>
                  {t("desktop.skills.delete")}
                </Button>
              </>
            }
          >
            <p>{t("desktop.skills.delete_body", { path: file?.skill?.dir ?? "" })}</p>
          </Dialog>,
          document.body,
        )}
      {pending &&
        createPortal(
          <Dialog
            title={t("desktop.agents.discard_title")}
            icon={mdiUndoVariant}
            onClose={() => setPending(null)}
            footer={
              <>
                <Button onClick={() => setPending(null)}>{t("desktop.cancel")}</Button>
                <Button
                  variant="filled"
                  danger
                  onClick={() => {
                    const go = pending;
                    setPending(null);
                    setFilesDirty(false);
                    go();
                  }}
                >
                  {t("desktop.agents.discard")}
                </Button>
              </>
            }
          >
            <p>{t("desktop.agents.discard_body", { name: form.name || t("desktop.agents.unnamed") })}</p>
          </Dialog>,
          document.body,
        )}
    </div>
  );
}

/**
 * Add from…: every skill the workspace sees, searched by name and
 * description, each with **Copy** (into .agents/skills under a name of
 * its own, to edit) and, with a skill's form open, **Reference** (it
 * becomes one of the skills that one combines). meta names that skill.
 */
function AddSkill({ workspace, skills, meta, onReference, onCopied, onClose }: { workspace: string; skills: SkillInfo[]; meta: string; onReference: (name: string) => void; onCopied: (f: SkillFile) => void; onClose: () => void }) {
  const snack = useSnackbar();
  const [query, setQuery] = useState("");
  const [copying, setCopying] = useState("");
  const [name, setName] = useState("");
  const [problems, setProblems] = useState<string[]>([]);
  const q = query.trim().toLowerCase();
  const found = skills.filter((s) => !q || s.name.includes(q) || s.description.toLowerCase().includes(q));
  const copy = async () => {
    setProblems([]);
    try {
      const r = await workspaces.copySkill({ workspace, from: copying, name: name.trim() });
      if (r.problems.length || !r.file) return setProblems(r.problems);
      snack(t("desktop.skills.copied", { name: name.trim() }));
      onCopied(r.file);
    } catch (e) {
      setProblems([message(e)]);
    }
  };
  return createPortal(
    <Dialog title={t("desktop.skills.add_from")} icon={mdiContentCopy} onClose={onClose} footer={<Button onClick={onClose}>{t("desktop.close")}</Button>}>
      <div className="stack add-skill" style={{ gap: 10 }}>
        <input className="input" autoFocus value={query} placeholder={t("desktop.skills.search")} aria-label={t("desktop.skills.search")} onChange={(e) => setQuery(e.target.value)} />
        <div className="list add-skill-list">
          {found.length === 0 && <p className="t-body-sm muted">{t("desktop.skills.search_none")}</p>}
          {found.map((s) => (
            <div key={s.name} className="list-item">
              <Icon path={kindIcons[kindOf(s)]} />
              <span className="lines spacer">
                <span className="ellipsis">
                  {s.name} <small className="muted">· {t(s.project ? "desktop.skills.from_project" : s.dir ? "desktop.skills.from_user" : "desktop.skills.from_builtin")}</small>
                </span>
                <small className="ellipsis">{s.description}</small>
              </span>
              <span className="trailing">
                {meta && (
                  <Button small icon={mdiLinkVariant} onClick={() => onReference(s.name)} title={t("desktop.skills.reference_hint", { name: meta })}>
                    {t("desktop.skills.reference")}
                  </Button>
                )}
                <Button
                  small
                  icon={mdiContentCopy}
                  onClick={() => {
                    setCopying(s.name);
                    setName(s.project ? `${s.name}-copy` : s.name);
                    setProblems([]);
                  }}
                >
                  {t("desktop.skills.copy")}
                </Button>
              </span>
            </div>
          ))}
        </div>
        {copying && (
          <div className="card outlined stack" style={{ gap: 8 }}>
            <span className="t-body-sm">{t("desktop.skills.copy_as", { name: copying })}</span>
            <div className="row" style={{ gap: 8 }}>
              <input className="input mono spacer" value={name} aria-label={t("desktop.skills.name")} onChange={(e) => setName(e.target.value)} onKeyDown={(e) => e.key === "Enter" && void copy()} />
              <Button variant="filled" disabled={!validSkillName(name.trim())} onClick={() => void copy()}>
                {t("desktop.skills.copy")}
              </Button>
            </div>
            {problems.map((p) => (
              <span key={p} className="t-body-sm error-text">
                {p}
              </span>
            ))}
          </div>
        )}
      </div>
    </Dialog>,
    document.body,
  );
}
