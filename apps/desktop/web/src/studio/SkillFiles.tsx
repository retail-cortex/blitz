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
import { mdiAlertCircleOutline, mdiCheckCircleOutline, mdiContentSaveOutline, mdiDeleteOutline, mdiFileDocumentOutline, mdiFilePlusOutline, mdiLanguagePython, mdiLanguageTypescript, mdiPlay, mdiUndoVariant, mdiUpload } from "@mdi/js";
import { workspaces } from "../api";
import { message } from "../errors";
import { t } from "../i18n";
import type { SkillFile } from "../gen/blitz/v1/workspace_pb";
import { Button, Dialog, Icon, IconButton, Segmented, useSnackbar } from "../ui/controls";
import { CodeArea } from "./CodeArea";
import { DraftButton } from "./Draft";
import { filePath, languageOfScript, scriptLanguages, scriptTemplate, splitArgs, type ScriptLanguage } from "./skillForm";

const decoder = new TextDecoder();
const encoder = new TextEncoder();

/** A test run's result, as RunSkillScript returns it. */
interface RunResult {
  exitCode: number;
  stdout: string;
  stderr: string;
  timedOut: boolean;
  error: string;
}

/**
 * A skill's References or Scripts tab (spec_skills_013 SK-101, SK-102):
 * the files in references/ or scripts/, each opened in an editor and
 * saved on its own (Save file, ⌘S); a new file, an upload (references)
 * or a new script from a template; Delete, asked first. Scripts show
 * the policy's verdict on each, and the open one has an AI helper
 * (Draft) and a Test run with arguments. onFile hears the skill as each
 * write leaves it; onDirty whether the open file has unsaved edits.
 */
export function SkillFiles({ workspace, file, folder, onFile, onDirty }: { workspace: string; file: SkillFile; folder: "references" | "scripts"; onFile: (f: SkillFile) => void; onDirty: (dirty: boolean) => void }) {
  const snack = useSnackbar();
  const skill = file.skill!;
  const name = skill.name;
  const [open, setOpen] = useState("");
  const [text, setText] = useState("");
  const [base, setBase] = useState("");
  const [revision, setRevision] = useState(0);
  const [adding, setAdding] = useState("");
  const [language, setLanguage] = useState<ScriptLanguage>("python");
  const [deleting, setDeleting] = useState("");
  const [pending, setPending] = useState<(() => void) | null>(null);
  const [args, setArgs] = useState("");
  const [running, setRunning] = useState(false);
  const [run, setRun] = useState<RunResult | null>(null);
  const upload = useRef<HTMLInputElement>(null);

  const dirty = open !== "" && text !== base;
  useEffect(() => onDirty(dirty), [dirty, onDirty]);

  const paths = folder === "references" ? skill.references : skill.scripts.filter((s) => s.source.startsWith("scripts/")).map((s) => s.source);
  const leave = (go: () => void) => (dirty ? setPending(() => go) : go());

  const show = (path: string) =>
    leave(async () => {
      try {
        const r = await workspaces.readSkillFile({ workspace, name, path });
        const body = decoder.decode(r.content);
        setOpen(path);
        setText(body);
        setBase(body);
        setRevision((n) => n + 1);
        setRun(null);
      } catch (e) {
        snack(message(e), { error: true });
      }
    });
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
    leave(async () => {
      const body = folder === "scripts" ? scriptTemplate(languageOfScript(path) || language, adding.trim()) : `# ${adding.trim()}\n`;
      try {
        await write(path, encoder.encode(body));
        setAdding("");
        setOpen(path);
        setText(body);
        setBase(body);
        setRevision((n) => n + 1);
        setRun(null);
      } catch (e) {
        snack(message(e), { error: true });
      }
    });
  };
  const uploadFiles = async (files: FileList | null) => {
    for (const f of Array.from(files ?? [])) {
      try {
        await write(`references/${f.name}`, new Uint8Array(await f.arrayBuffer()));
        snack(t("desktop.skills.uploaded", { name: f.name }));
      } catch (e) {
        snack(message(e), { error: true });
      }
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

  return (
    <div className="stack skill-files" style={{ gap: 12 }}>
      <p className="t-body-sm muted">{t(folder === "references" ? "desktop.skills.references_hint" : skill.scripts.some((s) => !s.found) ? "desktop.skills.scripts_declared" : "desktop.skills.scripts_hint")}</p>
      <div className="row wrap" style={{ gap: 8 }}>
        <input className="input skill-file-name" value={adding} placeholder={t(folder === "references" ? "desktop.skills.new_reference" : "desktop.skills.new_script")} aria-label={t(folder === "references" ? "desktop.skills.new_reference" : "desktop.skills.new_script")} onChange={(e) => setAdding(e.target.value)} onKeyDown={(e) => e.key === "Enter" && create()} />
        {folder === "scripts" && (
          <Segmented
            value={language}
            options={[
              { value: "python", label: t("desktop.skills.lang.python"), icon: mdiLanguagePython },
              { value: "typescript", label: t("desktop.skills.lang.typescript"), icon: mdiLanguageTypescript },
            ]}
            onChange={(v) => setLanguage(v as ScriptLanguage)}
            label={t("desktop.skills.language")}
            small
          />
        )}
        <Button icon={mdiFilePlusOutline} disabled={!adding.trim()} onClick={create}>
          {t("desktop.skills.add_file")}
        </Button>
        {folder === "references" && (
          <>
            <Button icon={mdiUpload} onClick={() => upload.current?.click()}>
              {t("desktop.skills.upload")}
            </Button>
            <input ref={upload} type="file" multiple hidden onChange={(e) => void uploadFiles(e.target.files)} />
          </>
        )}
      </div>
      <div className="skill-files-split">
        <div className="list skill-file-list">
          {folder === "references" && paths.length === 0 && <p className="t-body-sm muted">{t("desktop.skills.no_references")}</p>}
          {folder === "scripts" && skill.scripts.length === 0 && <p className="t-body-sm muted">{t("desktop.skills.no_scripts")}</p>}
          {folder === "references" &&
            paths.map((p) => (
              <div key={p} className={`list-item ${p === open ? "active" : ""}`} role="button" tabIndex={0} onClick={() => show(p)} onKeyDown={(e) => e.key === "Enter" && show(p)}>
                <Icon path={mdiFileDocumentOutline} />
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
              return (
                <div key={s.name} className={`list-item ${s.source === open ? "active" : ""}`} role="button" tabIndex={editable ? 0 : -1} aria-disabled={!editable} title={s.allowed ? s.source : s.reasons.join("; ")} onClick={() => editable && show(s.source)} onKeyDown={(e) => e.key === "Enter" && editable && show(s.source)}>
                  <Icon path={s.language === "typescript" ? mdiLanguageTypescript : mdiLanguagePython} />
                  <span className="lines spacer">
                    <span className="ellipsis">{s.name}</span>
                    <small className={`ellipsis ${s.allowed ? "" : "error-text"}`}>
                      {s.allowed ? t(s.found ? "desktop.skills.script_found" : "desktop.skills.script_declared", { source: s.source }) : t("desktop.skills.script_blocked", { reasons: s.reasons.join("; ") })}
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
        <div className="stack skill-file-editor" style={{ gap: 8 }}>
          {!open && <p className="t-body-sm muted">{t("desktop.skills.choose_file")}</p>}
          {open && (
            <>
              <div className="row" style={{ gap: 8 }}>
                <code className="t-body-sm spacer ellipsis">{open}</code>
                {script && <DraftButton workspace={workspace} kind="script" name={script.name} description={skill.description} current={text} language={languageOfScript(open)} onUse={(v) => (setText(v), setRevision((n) => n + 1))} />}
                {dirty && (
                  <Button small icon={mdiUndoVariant} onClick={() => (setText(base), setRevision((n) => n + 1))}>
                    {t("desktop.agents.revert")}
                  </Button>
                )}
                <Button small variant="tonal" icon={mdiContentSaveOutline} disabled={!dirty} onClick={() => void save()}>
                  {t("desktop.skills.save_file")}
                </Button>
              </div>
              <CodeArea path={open} value={text} revision={revision} workspace={workspace} label={open} onChange={setText} onSave={() => void save()} />
              {script && (
                <div className="card outlined stack skill-test-run" style={{ gap: 8 }}>
                  <div className="row wrap" style={{ gap: 8 }}>
                    <span className="t-title-sm">{t("desktop.skills.test_run")}</span>
                    <input className="input mono spacer" value={args} placeholder={t("desktop.skills.args")} aria-label={t("desktop.skills.args")} onChange={(e) => setArgs(e.target.value)} onKeyDown={(e) => e.key === "Enter" && void testRun()} />
                    <Button variant="filled" icon={mdiPlay} disabled={running || !script.allowed} onClick={() => void testRun()}>
                      {running ? t("desktop.skills.running") : t("desktop.skills.run")}
                    </Button>
                  </div>
                  <span className="t-body-sm muted">{script.allowed ? t("desktop.skills.test_run_hint") : t("desktop.skills.script_blocked", { reasons: script.reasons.join("; ") })}</span>
                  {run && (
                    <div className="stack" style={{ gap: 4 }}>
                      <span className={`t-body-sm ${run.error || run.exitCode !== 0 ? "error-text" : ""}`}>{run.error || (run.timedOut ? t("desktop.skills.timed_out") : t("desktop.skills.exit", { code: run.exitCode }))}</span>
                      {run.stdout && <pre className="skill-run-output">{run.stdout}</pre>}
                      {run.stderr && <pre className="skill-run-output error-text">{run.stderr}</pre>}
                    </div>
                  )}
                </div>
              )}
            </>
          )}
        </div>
      </div>
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
    </div>
  );
}
