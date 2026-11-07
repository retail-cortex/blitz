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
import { Code, ConnectError } from "@connectrpc/connect";
import { mdiAlertCircleOutline, mdiArrowLeft, mdiContentSaveOutline, mdiDeleteOutline, mdiFileDocumentOutline, mdiFormatListNumbered, mdiPlus, mdiUndoVariant } from "@mdi/js";
import { Button, Dialog, Field, Icon, IconButton, useSnackbar } from "@blitz/webui";
import { files } from "../api";
import { FileKind } from "../gen/blitz/v1/file_pb";
import { message } from "../errors";
import { t } from "../i18n";
import { DraftButton } from "./Draft";
import { MarkdownField } from "./MarkdownField";
import { ConfirmDialog, useDiscardGuard } from "./paneParts";
import { newSpecFile, newSpecText, specFiles, specsDir, specsIndex, type SpecFile } from "./specs";

/** The spec open in the form: its file, the text read and its version. */
interface OpenSpec {
  spec: SpecFile;
  base: string;
  version: string;
}

/**
 * The project's specification (spec_040_studio STU-90): the specs /spec
 * writes in .agents/specs, the index first, then in order of execution.
 * A spec opens over the list in the Markdown field (Visual or Source),
 * with Redraft under it; Save writes it back, refused when it changed on
 * disk since it was read. New adds the next number's spec.
 */
export function SpecsPane({ workspace, onDirty }: { workspace: string; onDirty?: (dirty: boolean) => void }) {
  const snack = useSnackbar();
  const [specs, setSpecs] = useState<SpecFile[]>([]);
  const [error, setError] = useState("");
  const [open, setOpen] = useState<OpenSpec | null>(null);
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState("");
  const [adding, setAdding] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);

  const refresh = useCallback(async () => {
    try {
      const r = await files.listDir({ workspace, path: specsDir, showHidden: true });
      setSpecs(specFiles(r.entries.filter((e) => e.kind === FileKind.FILE).map((e) => e.name)));
      setError("");
    } catch (e) {
      // No folder yet: no specs.
      if (e instanceof ConnectError && e.code === Code.NotFound) setSpecs([]);
      else setError(message(e));
    }
  }, [workspace]);
  useEffect(() => void refresh(), [refresh]);

  const dirty = open !== null && text !== open.base;
  useEffect(() => onDirty?.(dirty), [dirty, onDirty]);
  const { leave, discard } = useDiscardGuard(dirty);

  const show = (spec: SpecFile) =>
    leave(async () => {
      try {
        const r = await files.readFile({ workspace, path: spec.path });
        setOpen({ spec, base: r.text, version: r.version });
        setText(r.text);
        setProblem("");
      } catch (e) {
        snack(message(e), { error: true });
      }
    });
  const close = () =>
    leave(() => {
      setOpen(null);
      void refresh();
    });

  const save = async () => {
    if (!open || busy) return;
    setBusy(true);
    setProblem("");
    try {
      const r = await files.writeFile({ workspace, path: open.spec.path, text, version: open.version });
      setOpen({ ...open, base: text, version: r.version });
      snack(t("desktop.specs.saved", { name: open.spec.file }));
    } catch (e) {
      setProblem(message(e));
    } finally {
      setBusy(false);
    }
  };
  const add = async (title: string) => {
    const file = newSpecFile(title, specs);
    if (!file) return;
    setAdding(null);
    const spec = specFiles([file])[0];
    try {
      const body = newSpecText(title);
      const r = await files.writeFile({ workspace, path: spec.path, text: body, version: "" });
      await refresh();
      setOpen({ spec, base: body, version: r.version });
      setText(body);
      setProblem("");
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const remove = async () => {
    setDeleting(false);
    if (!open) return;
    try {
      await files.deleteFile({ workspace, path: open.spec.path });
      snack(t("desktop.specs.deleted", { name: open.spec.file }));
      setOpen(null);
      await refresh();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };

  const label = (s: SpecFile) => (s.file === specsIndex ? t("desktop.specs.index") : s.number ? `${String(s.number).padStart(3, "0")} · ${s.title}` : s.title);
  return (
    <div className={`slide-pane specs-pane ${open ? "is-open" : ""}`}>
      <aside className="slide-list" inert={open !== null}>
        <div className="row side-head">
          <span className="t-title-sm spacer">{t("desktop.specs.title")}</span>
          <IconButton icon={mdiPlus} label={t("desktop.specs.new")} small onClick={() => setAdding("")} />
        </div>
        <p className="t-body-sm muted agent-dir">{t("desktop.workers.in", { path: specsDir })}</p>
        {error && (
          <div className="card error row small-card">
            <Icon path={mdiAlertCircleOutline} size="sm" /> {error}
          </div>
        )}
        {!error && specs.length === 0 && <p className="t-body-sm muted">{t("desktop.specs.none")}</p>}
        <div className="list">
          {specs.map((s) => (
            <button key={s.path} className="list-item" title={s.path} onClick={() => void show(s)}>
              <Icon path={s.file === specsIndex ? mdiFormatListNumbered : mdiFileDocumentOutline} />
              <span className="lines">
                <span className="ellipsis">{label(s)}</span>
                <small className="ellipsis">{s.file}</small>
              </span>
            </button>
          ))}
        </div>
      </aside>
      <section className="slide-detail" inert={open === null}>
        {open && (
          <>
            <div className="row slide-back">
              <IconButton icon={mdiArrowLeft} label={t("desktop.specs.back")} onClick={close} />
              <h2 className="t-headline spacer ellipsis">{label(open.spec)}</h2>
              {dirty && <span className="t-body-sm muted">{t("desktop.agents.unsaved")}</span>}
            </div>
            <div className="worker agent-form">
              <MarkdownField
                key={open.spec.path}
                label={open.spec.file}
                supporting={t("desktop.specs.hint")}
                value={text}
                onValue={setText}
                dir={workspace}
                path={open.spec.path}
                workspace={workspace}
                rows={18}
                height={440}
                onSave={() => void save()}
                tools={<DraftButton workspace={workspace} kind="spec" name={open.spec.title} description="" current={text} onUse={setText} />}
              />
              {problem && (
                <div className="card error row small-card" style={{ gap: 8 }}>
                  <Icon path={mdiAlertCircleOutline} size="sm" />
                  <span className="t-body-sm">{problem}</span>
                </div>
              )}
            </div>
            <div className="row wrap agent-actions slide-actions">
              <span className="t-body-sm muted spacer ellipsis" title={open.spec.path}>
                {t("desktop.agents.where", { path: open.spec.path })}
              </span>
              <Button danger icon={mdiDeleteOutline} onClick={() => setDeleting(true)}>
                {t("desktop.specs.delete")}
              </Button>
              {dirty && (
                <Button icon={mdiUndoVariant} onClick={() => setText(open.base)}>
                  {t("desktop.agents.revert")}
                </Button>
              )}
              <Button onClick={close}>{t("desktop.close")}</Button>
              <Button variant="filled" icon={mdiContentSaveOutline} disabled={busy || !dirty} onClick={() => void save()}>
                {t("desktop.specs.save")}
              </Button>
            </div>
          </>
        )}
      </section>
      {adding !== null && (
        <Dialog
          title={t("desktop.specs.new")}
          icon={mdiPlus}
          onClose={() => setAdding(null)}
          footer={
            <>
              <Button onClick={() => setAdding(null)}>{t("desktop.cancel")}</Button>
              <Button variant="filled" disabled={!newSpecFile(adding, specs)} onClick={() => void add(adding)}>
                {t("desktop.specs.create")}
              </Button>
            </>
          }
        >
          <Field label={t("desktop.specs.new_title")} supporting={newSpecFile(adding, specs) ? t("desktop.workers.in", { path: `${specsDir}/${newSpecFile(adding, specs)}` }) : t("desktop.specs.new_hint")}>
            {(id) => (
              <input
                id={id}
                className="input"
                autoFocus
                value={adding}
                onChange={(e) => setAdding(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && newSpecFile(adding, specs)) void add(adding);
                }}
              />
            )}
          </Field>
        </Dialog>
      )}
      {deleting && open && (
        <ConfirmDialog title={t("desktop.specs.delete_title", { name: open.spec.file })} icon={mdiDeleteOutline} confirm={t("desktop.specs.delete")} onConfirm={() => void remove()} onClose={() => setDeleting(false)}>
          <p>{t("desktop.specs.delete_body", { path: open.spec.path })}</p>
        </ConfirmDialog>
      )}
      {discard(open?.spec.file ?? "")}
    </div>
  );
}
