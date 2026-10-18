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

import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import {
  mdiAlertCircleOutline,
  mdiArrowLeft,
  mdiBash,
  mdiCheckCircleOutline,
  mdiChevronDown,
  mdiChevronRight,
  mdiConsoleLine,
  mdiContentSaveOutline,
  mdiDeleteOutline,
  mdiFileDocumentOutline,
  mdiFileImageOutline,
  mdiFilePdfBox,
  mdiFilePlusOutline,
  mdiFileTableOutline,
  mdiLanguageMarkdownOutline,
  mdiLanguageJavascript,
  mdiLanguagePython,
  mdiLanguageTypescript,
  mdiNodejs,
  mdiPackageVariantClosed,
  mdiPlus,
  mdiPowershell,
  mdiLoading,
  mdiPlay,
  mdiUndoVariant,
  mdiUpload,
} from "@mdi/js";
import { workspaces } from "../api";
import { message } from "../errors";
import { t } from "../i18n";
import type { SkillFile } from "../gen/blitz/v1/workspace_pb";
import { Preview } from "../files/Preview";
import { previewKind, previewOnly, type PreviewKind } from "../files/previewKind";
import { Button, Dialog, Icon, IconButton, Segmented, useSnackbar } from "../ui/controls";
import { CodeArea } from "./CodeArea";
import { DraftButton } from "./Draft";
import { activePackageFiles, filePath, languageOfScript, packageKind, packageTemplate, scriptLanguages, scriptTemplate, splitArgs, type ScriptLanguage } from "./skillForm";

const decoder = new TextDecoder();
const encoder = new TextEncoder();

/** Each script language's icon. */
const languageIcons: Record<ScriptLanguage, string> = { python: mdiLanguagePython, typescript: mdiLanguageTypescript, javascript: mdiLanguageJavascript, shell: mdiBash, cmd: mdiConsoleLine, powershell: mdiPowershell };

/** A reference's icon, by how it previews. */
const previewIcon = (k: PreviewKind) => (k === "pdf" ? mdiFilePdfBox : k === "image" || k === "svg" ? mdiFileImageOutline : k === "markdown" ? mdiLanguageMarkdownOutline : k === "table" ? mdiFileTableOutline : mdiFileDocumentOutline);

/** A test run's result, as RunSkillScript returns it. */
interface RunResult {
  exitCode: number;
  stdout: string;
  stderr: string;
  timedOut: boolean;
  error: string;
}

/**
 * A skill's References or Scripts tab (spec_skills_013 SK-101, SK-102),
 * as a list and a file, the way Studio's lists and forms are: the files
 * in references/ or scripts/ fill the tab (a script with its language,
 * where it comes from and the policy's verdict), with New file (and
 * Upload… for references); opening one gives it the whole tab, with a
 * back arrow. A text file opens in an editor and is saved on its own
 * (Save file, ⌘S); Markdown, SVG and tables switch between source and
 * preview; a PDF, an image or media shows as the Files editor shows it.
 * A script has Redraft and a Test run panel under it. onFile hears the
 * skill as each write leaves it; onDirty whether the open file has
 * unsaved edits; onOpen whether a file is open (the skill's buttons step
 * aside for the file's).
 */
export function SkillFiles({ workspace, file, folder, onFile, onDirty, onOpen }: { workspace: string; file: SkillFile; folder: "references" | "scripts"; onFile: (f: SkillFile) => void; onDirty: (dirty: boolean) => void; onOpen: (open: boolean) => void }) {
  const snack = useSnackbar();
  const skill = file.skill!;
  const name = skill.name;
  const [open, setOpen] = useState("");
  const [text, setText] = useState("");
  const [base, setBase] = useState("");
  const [revision, setRevision] = useState(0);
  const [view, setView] = useState<"source" | "preview">("source");
  const [creating, setCreating] = useState(false);
  const [adding, setAdding] = useState("");
  const [language, setLanguage] = useState<ScriptLanguage>("python");
  const [deleting, setDeleting] = useState("");
  const [pending, setPending] = useState<(() => void) | null>(null);
  const [runOpen, setRunOpen] = useState(false);
  const [args, setArgs] = useState("");
  const [running, setRunning] = useState(false);
  const [run, setRun] = useState<RunResult | null>(null);
  const upload = useRef<HTMLInputElement>(null);

  const dirty = open !== "" && text !== base;
  useEffect(() => onDirty(dirty), [dirty, onDirty]);
  useEffect(() => onOpen(open !== ""), [open, onOpen]);

  // The skill's folder within the workspace, for previews ("" when it isn't in it).
  const rel = skill.dir.startsWith(workspace + "/") ? skill.dir.slice(workspace.length + 1) : "";
  const kind = open ? previewKind(open) : null;
  const binary = previewOnly(kind) && rel !== "";
  const paths = folder === "references" ? skill.references : skill.scripts.filter((s) => s.source.startsWith("scripts/")).map((s) => s.source);
  const leave = (go: () => void) => (dirty ? setPending(() => go) : go());

  const openFile = (path: string, body: string) => {
    setOpen(path);
    setText(body);
    setBase(body);
    setRevision((n) => n + 1);
    setView(previewKind(path) === "markdown" || previewKind(path) === "table" ? "preview" : "source");
    setRun(null);
  };
  const show = (path: string) =>
    leave(async () => {
      if (previewOnly(previewKind(path)) && rel) return openFile(path, ""); // shown, not read as text
      try {
        const r = await workspaces.readSkillFile({ workspace, name, path });
        openFile(path, decoder.decode(r.content));
      } catch (e) {
        snack(message(e), { error: true });
      }
    });
  const close = () => leave(() => setOpen(""));
  const write = async (path: string, content: Uint8Array) => {
    const r = await workspaces.writeSkillFile({ workspace, name, path, content });
    if (r.file) onFile(r.file);
  };
  const save = async () => {
    if (!open || !dirty) return;
    try {
      await write(open, encoder.encode(text));
      setBase(text);
      snack(t("desktop.skills.file_saved", { path: open }));
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const create = () => {
    const path = filePath(folder, adding, folder === "scripts" ? scriptLanguages[language] : ".md");
    if (!path) return snack(t("desktop.skills.file_bad"), { error: true });
    if (paths.includes(path)) return snack(t("desktop.skills.file_exists", { path }), { error: true });
    const body = folder === "scripts" ? scriptTemplate(languageOfScript(path) || language, adding.trim()) : `# ${adding.trim()}\n`;
    void write(path, encoder.encode(body)).then(
      () => {
        setAdding("");
        setCreating(false);
        openFile(path, body);
      },
      (e) => snack(message(e), { error: true }),
    );
  };
  // Upload… adds files to references/; Replace… puts a new file in place of the open one.
  const uploadFiles = async (list: FileList | null, replace: string) => {
    for (const f of Array.from(list ?? [])) {
      const path = replace || `references/${f.name}`;
      try {
        await write(path, new Uint8Array(await f.arrayBuffer()));
        snack(t("desktop.skills.uploaded", { name: path.replace(/^references\//, "") }));
        if (replace) setRevision((n) => n + 1);
      } catch (e) {
        snack(message(e), { error: true });
      }
      if (replace) break;
    }
    if (upload.current) upload.current.value = "";
  };
  const remove = async () => {
    const path = deleting;
    setDeleting("");
    try {
      const r = await workspaces.deleteSkillFile({ workspace, name, path });
      if (r.file) onFile(r.file);
      if (open === path) {
        setOpen("");
        setText("");
        setBase("");
      }
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const script = folder === "scripts" ? skill.scripts.find((s) => s.source === open) : undefined;
  // Packages (SK-107): the package files the folder has, the first of each kind counting.
  const packages = skill.packageFiles;
  const active = activePackageFiles(packages);
  const addPackageFile = (path: string) => {
    const body = packageTemplate(path);
    void write(path, encoder.encode(body)).then(
      () => openFile(path, body),
      (e) => snack(message(e), { error: true }),
    );
  };
  const testRun = async () => {
    if (!script) return;
    if (dirty) await save();
    setRunning(true);
    setRun(null);
    try {
      setRun(await workspaces.runSkillScript({ workspace, name, script: script.name, args: splitArgs(args) }));
    } catch (e) {
      setRun({ exitCode: -1, stdout: "", stderr: "", timedOut: false, error: message(e) });
    } finally {
      setRunning(false);
    }
  };
  const hidden = <input ref={upload} type="file" multiple={!open} hidden onChange={(e) => void uploadFiles(e.target.files, open && binary ? open : "")} />;

  const dialogs = (
    <>
      {deleting &&
        createPortal(
          <Dialog
            title={t("desktop.skills.delete_file_title", { path: deleting })}
            icon={mdiDeleteOutline}
            onClose={() => setDeleting("")}
            footer={
              <>
                <Button onClick={() => setDeleting("")}>{t("desktop.cancel")}</Button>
                <Button variant="filled" danger onClick={() => void remove()}>
                  {t("desktop.workers.delete")}
                </Button>
              </>
            }
          >
            <p>{t("desktop.skills.delete_file_body", { path: deleting })}</p>
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
                    go();
                  }}
                >
                  {t("desktop.agents.discard")}
                </Button>
              </>
            }
          >
            <p>{t("desktop.agents.discard_body", { name: open })}</p>
          </Dialog>,
          document.body,
        )}
    </>
  );

  // A file, given the whole tab.
  if (open) {
    const toggles = kind === "markdown" || kind === "svg" || kind === "table";
    return (
      <div className="stack skill-file-view" style={{ gap: 10 }}>
        <div className="row wrap skill-file-head" style={{ gap: 8 }}>
          <IconButton icon={mdiArrowLeft} label={t(folder === "references" ? "desktop.skills.back_references" : "desktop.skills.back_scripts")} onClick={close} />
          <code className="spacer ellipsis" title={open}>
            {open}
          </code>
          {dirty && <span className="t-body-sm muted">{t("desktop.agents.unsaved")}</span>}
          {toggles && rel && (
            <Segmented
              small
              value={view}
              label={t("desktop.skills.view")}
              options={[
                { value: "source", label: t("desktop.skills.source") },
                { value: "preview", label: t("desktop.skills.preview") },
              ]}
              onChange={setView}
            />
          )}
          {script && <DraftButton workspace={workspace} kind="script" name={script.name} description={skill.description} current={text} language={languageOfScript(open)} onUse={(v) => (setText(v), setView("source"), setRevision((n) => n + 1))} />}
          {binary && (
            <Button small icon={mdiUpload} onClick={() => upload.current?.click()}>
              {t("desktop.skills.replace")}
            </Button>
          )}
          {dirty && (
            <Button small icon={mdiUndoVariant} onClick={() => (setText(base), setRevision((n) => n + 1))}>
              {t("desktop.agents.revert")}
            </Button>
          )}
          {!binary && (
            <Button small variant="filled" icon={mdiContentSaveOutline} disabled={!dirty} onClick={() => void save()}>
              {t("desktop.skills.save_file")}
            </Button>
          )}
          <IconButton icon={mdiDeleteOutline} label={t("desktop.skills.delete_file", { path: open })} onClick={() => setDeleting(open)} />
          {hidden}
        </div>
        {script && !script.allowed && (
          <div className="card error row small-card">
            <Icon path={mdiAlertCircleOutline} size="sm" /> {t("desktop.skills.script_blocked", { reasons: script.reasons.join("; ") })}
          </div>
        )}
        <div className="skill-file-body">
          {binary ? (
            <div className="skill-file-preview">
              <Preview key={revision} dir={workspace} path={`${rel}/${open}`} kind={kind} />
            </div>
          ) : toggles && view === "preview" && rel ? (
            <div className="skill-file-preview">
              <Preview dir={workspace} path={`${rel}/${open}`} kind={kind} text={text} />
            </div>
          ) : (
            <CodeArea path={open} value={text} revision={revision} workspace={workspace} label={open} onChange={setText} onSave={() => void save()} />
          )}
        </div>
        {script && (
          <div className={`card outlined skill-test-run ${runOpen ? "open" : ""}`}>
            <div role="button" tabIndex={0} className="row skill-test-toggle" aria-expanded={runOpen} onClick={() => setRunOpen((o) => !o)} onKeyDown={(e) => (e.key === "Enter" || e.key === " ") && (e.preventDefault(), setRunOpen((o) => !o))}>
              <Icon path={runOpen ? mdiChevronDown : mdiChevronRight} size="sm" />
              <span className="t-title-sm">{t("desktop.skills.test_run")}</span>
              {run && !runOpen && <span className={`t-body-sm ${run.error || run.exitCode !== 0 ? "error-text" : "muted"}`}>{run.error || (run.timedOut ? t("desktop.skills.timed_out") : t("desktop.skills.exit", { code: run.exitCode }))}</span>}
            </div>
            {runOpen && (
              <div className="stack" style={{ gap: 8 }}>
                <div className="row" style={{ gap: 8 }}>
                  <input className="input mono spacer" autoFocus value={args} placeholder={t("desktop.skills.args")} aria-label={t("desktop.skills.args")} onChange={(e) => setArgs(e.target.value)} onKeyDown={(e) => e.key === "Enter" && void testRun()} />
                  <Button variant="filled" icon={running ? mdiLoading : mdiPlay} iconSpin={running} disabled={running || !script.allowed} onClick={() => void testRun()}>
                    {running ? t("desktop.skills.running") : t("desktop.skills.run")}
                  </Button>
                </div>
                <span className="t-body-sm muted">{t("desktop.skills.test_run_hint")}</span>
                {run && (
                  <div className="stack" style={{ gap: 4 }}>
                    <span className={`t-body-sm ${run.error || run.exitCode !== 0 ? "error-text" : ""}`}>{run.error || (run.timedOut ? t("desktop.skills.timed_out") : t("desktop.skills.exit", { code: run.exitCode }))}</span>
                    {run.stdout && <pre className="skill-run-output">{run.stdout}</pre>}
                    {run.stderr && <pre className="skill-run-output error-text">{run.stderr}</pre>}
                  </div>
                )}
              </div>
            )}
          </div>
        )}
        {dialogs}
      </div>
    );
  }

  // The list, filling the tab.
  return (
    <div className="stack skill-files" style={{ gap: 12 }}>
      <div className="row wrap" style={{ gap: 8 }}>
        <p className="t-body-sm muted spacer">{t(folder === "references" ? "desktop.skills.references_hint" : skill.scripts.some((s) => !s.found) ? "desktop.skills.scripts_declared" : "desktop.skills.scripts_hint")}</p>
        {folder === "references" && (
          <Button icon={mdiUpload} onClick={() => upload.current?.click()}>
            {t("desktop.skills.upload")}
          </Button>
        )}
        <Button variant="tonal" icon={mdiFilePlusOutline} onClick={() => setCreating(true)}>
          {t(folder === "references" ? "desktop.skills.new_file" : "desktop.skills.new_script_button")}
        </Button>
        {hidden}
      </div>
      {creating && (
        <div className="card outlined row wrap skill-new-file" style={{ gap: 8 }}>
          <input className="input skill-file-name" autoFocus value={adding} placeholder={t(folder === "references" ? "desktop.skills.new_reference" : "desktop.skills.new_script")} aria-label={t(folder === "references" ? "desktop.skills.new_reference" : "desktop.skills.new_script")} onChange={(e) => setAdding(e.target.value)} onKeyDown={(e) => (e.key === "Enter" ? create() : e.key === "Escape" && setCreating(false))} />
          {folder === "scripts" && (
            <select className="select skill-language" value={language} aria-label={t("desktop.skills.language")} onChange={(e) => setLanguage(e.target.value as ScriptLanguage)}>
              {(Object.keys(scriptLanguages) as ScriptLanguage[]).map((l) => (
                <option key={l} value={l}>
                  {t(`desktop.skills.lang.${l}`)} ({scriptLanguages[l]})
                </option>
              ))}
            </select>
          )}
          <Button variant="filled" disabled={!adding.trim()} onClick={create}>
            {t("desktop.skills.create_file")}
          </Button>
          <Button onClick={() => (setCreating(false), setAdding(""))}>{t("desktop.cancel")}</Button>
        </div>
      )}
      <div className="list skill-file-list">
        {folder === "references" && paths.length === 0 && <p className="t-body-sm muted">{t("desktop.skills.no_references")}</p>}
        {folder === "scripts" && skill.scripts.length === 0 && <p className="t-body-sm muted">{t("desktop.skills.no_scripts")}</p>}
        {folder === "references" &&
          paths.map((p) => (
            <div key={p} className="list-item" role="button" tabIndex={0} onClick={() => show(p)} onKeyDown={(e) => e.key === "Enter" && show(p)}>
              <Icon path={previewIcon(previewKind(p))} />
              <span className="ellipsis spacer" title={p}>
                {p.replace(/^references\//, "")}
              </span>
              <span className="trailing">
                <IconButton icon={mdiDeleteOutline} label={t("desktop.skills.delete_file", { path: p })} small className="hover-only" onClick={(e) => (e.stopPropagation(), setDeleting(p))} />
              </span>
            </div>
          ))}
        {folder === "scripts" &&
          skill.scripts.map((s) => {
            const editable = s.source.startsWith("scripts/");
            const lang = (languageOfScript(s.source) || s.language) as ScriptLanguage;
            const known = lang in languageIcons;
            return (
              <div key={s.name} className="list-item" role="button" tabIndex={editable ? 0 : -1} aria-disabled={!editable} title={s.allowed ? s.source : s.reasons.join("; ")} onClick={() => editable && show(s.source)} onKeyDown={(e) => e.key === "Enter" && editable && show(s.source)}>
                <Icon path={known ? languageIcons[lang] : mdiFileDocumentOutline} />
                <span className="lines spacer">
                  <span className="ellipsis">{s.name}</span>
                  <small className={`ellipsis ${s.allowed ? "" : "error-text"}`}>
                    {[known ? t(`desktop.skills.lang.${lang}`) : s.language, s.deps.length ? t("desktop.skills.packages_n", { n: s.deps.length }) : "", s.allowed ? t(s.found ? "desktop.skills.script_found" : "desktop.skills.script_declared", { source: s.source }) : t("desktop.skills.script_blocked", { reasons: s.reasons.join("; ") })].filter(Boolean).join(" · ")}
                  </small>
                </span>
                <span className="trailing">
                  <Icon path={s.allowed ? mdiCheckCircleOutline : mdiAlertCircleOutline} size="sm" className={s.allowed ? "muted" : "error-text"} />
                  {editable && <IconButton icon={mdiDeleteOutline} label={t("desktop.skills.delete_file", { path: s.source })} small className="hover-only" onClick={(e) => (e.stopPropagation(), setDeleting(s.source))} />}
                </span>
              </div>
            );
          })}
      </div>
      {folder === "scripts" && (
        <section className="stack skill-packages" style={{ gap: 6 }}>
          <div className="row wrap" style={{ gap: 8 }}>
            <span className="t-title-sm spacer">{t("desktop.skills.packages")}</span>
            {!packages.some((p) => packageKind(p) === "python") && (
              <Button small icon={mdiPlus} onClick={() => addPackageFile("scripts/requirements.txt")}>
                {t("desktop.skills.add_package", { file: "requirements.txt" })}
              </Button>
            )}
            {!packages.some((p) => packageKind(p) === "node") && (
              <Button small icon={mdiPlus} onClick={() => addPackageFile("scripts/package.json")}>
                {t("desktop.skills.add_package", { file: "package.json" })}
              </Button>
            )}
          </div>
          <span className="t-body-sm muted">{t("desktop.skills.packages_hint")}</span>
          <div className="list">
            {packages.map((p) => (
              <div key={p} className="list-item" role="button" tabIndex={0} onClick={() => show(p)} onKeyDown={(e) => e.key === "Enter" && show(p)}>
                <Icon path={packageKind(p) === "node" ? mdiNodejs : mdiLanguagePython} />
                <span className="lines spacer">
                  <span className="ellipsis">{p}</span>
                  <small className="ellipsis">{active.has(p) ? t(packageKind(p) === "node" ? "desktop.skills.packages_node" : "desktop.skills.packages_python") : t("desktop.skills.packages_unused")}</small>
                </span>
                <span className="trailing">
                  <Icon path={mdiPackageVariantClosed} size="sm" className={active.has(p) ? "muted" : "error-text"} />
                  <IconButton icon={mdiDeleteOutline} label={t("desktop.skills.delete_file", { path: p })} small className="hover-only" onClick={(e) => (e.stopPropagation(), setDeleting(p))} />
                </span>
              </div>
            ))}
          </div>
        </section>
      )}
      {dialogs}
    </div>
  );
}
