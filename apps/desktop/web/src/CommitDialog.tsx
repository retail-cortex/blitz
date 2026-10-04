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

// The commit dialog: what's staged, a message the model drafted from the
// staged diff on the user's behalf (theirs to edit), and Commit. With
// nothing staged it offers to stage every change first.
import { mdiAlertCircleOutline, mdiCreationOutline, mdiSourceCommit } from "@mdi/js";
import { useCallback, useEffect, useRef, useState } from "react";
import { workspaces } from "./api";
import { canCommit, statusKey, subject, subjectLimit } from "./commit";
import { message as errorMessage } from "./errors";
import type { StagedFile } from "./gen/blitz/v1/workspace_pb";
import { t, tn } from "./i18n";
import { Button, Dialog, Icon, useSnackbar } from "./ui/controls";

/** The commit dialog for the workspace in dir; onCommitted after a commit (to refresh). */
export function CommitDialog({ dir, onClose, onCommitted }: { dir: string; onClose: () => void; onCommitted?: () => void }) {
  const snack = useSnackbar();
  const [files, setFiles] = useState<StagedFile[] | null>(null);
  const [text, setText] = useState("");
  const [problem, setProblem] = useState("");
  const [error, setError] = useState("");
  const [drafting, setDrafting] = useState(false);
  const [committing, setCommitting] = useState(false);
  // In a ref: the parent's refresh re-renders it with a new function, which
  // mustn't start another draft.
  const committed = useRef(onCommitted);
  committed.current = onCommitted;

  const draft = useCallback(
    async (stageAll: boolean) => {
      setDrafting(true);
      setError("");
      setProblem("");
      try {
        const d = await workspaces.draftCommit({ workspace: dir, stageAll });
        setFiles(d.files);
        if (d.message) setText(d.message);
        setProblem(d.problem);
        if (stageAll) committed.current?.(); // the index changed: the tree's marks too
      } catch (e) {
        setError(errorMessage(e));
        setFiles((f) => f ?? []);
      } finally {
        setDrafting(false);
      }
    },
    [dir],
  );
  useEffect(() => {
    void draft(false);
  }, [draft]);

  const ready = canCommit({ files: files?.length ?? 0, message: text, busy: drafting || committing });
  const commit = async () => {
    if (!ready) return;
    setCommitting(true);
    setError("");
    try {
      const c = await workspaces.commit({ workspace: dir, message: text });
      snack(t("desktop.commit.done", { hash: c.hash, subject: c.subject }));
      onCommitted?.();
      onClose();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setCommitting(false);
    }
  };

  const first = subject(text);
  const nothing = files !== null && files.length === 0 && !drafting;
  return (
    <Dialog
      title={t("desktop.commit.title")}
      icon={mdiSourceCommit}
      wide
      className="commit-dialog"
      onClose={onClose}
      headerActions={
        files && files.length > 0 ? (
          <Button small icon={mdiCreationOutline} disabled={drafting || committing} onClick={() => void draft(false)} title={t("desktop.commit.redraft_hint")}>
            {t("desktop.commit.redraft")}
          </Button>
        ) : undefined
      }
      footer={
        <>
          <Button onClick={onClose}>{t("desktop.cancel")}</Button>
          <Button variant="filled" icon={mdiSourceCommit} disabled={!ready} onClick={() => void commit()}>
            {committing ? t("desktop.commit.committing") : files && files.length > 0 ? tn("desktop.commit.commit_n", files.length) : t("desktop.commit.commit")}
          </Button>
        </>
      }
    >
      {error && (
        <div className="card error row">
          <Icon path={mdiAlertCircleOutline} /> {error}
        </div>
      )}
      {nothing ? (
        <div className="commit-empty">
          <p className="t-title">{t("desktop.commit.nothing")}</p>
          <p className="muted">{t("desktop.commit.nothing.detail")}</p>
          <Button variant="filled" onClick={() => void draft(true)}>
            {t("desktop.commit.stage_all")}
          </Button>
        </div>
      ) : (
        <>
          <div className="field">
            <label htmlFor="commit-message">{t("desktop.commit.message")}</label>
            {drafting && <div className="progress" role="progressbar" aria-label={t("desktop.commit.drafting")} />}
            <textarea
              id="commit-message"
              className="input mono commit-message"
              rows={8}
              spellCheck
              value={text}
              disabled={drafting && text === ""}
              placeholder={drafting ? t("desktop.commit.drafting") : t("desktop.commit.placeholder")}
              onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                  e.preventDefault();
                  void commit();
                }
              }}
            />
            {(problem || text) && (
              <span className={`supporting ${problem || first.length > subjectLimit ? "error-text" : ""}`}>
                {problem ? t("desktop.commit.problem", { problem }) : t("desktop.commit.subject_length", { n: first.length, max: subjectLimit })}
              </span>
            )}
          </div>
          {files && files.length > 0 && (
            <div className="commit-files">
              <div className="t-label muted">{tn("desktop.commit.staged", files.length)}</div>
              <ul>
                {files.map((f) => (
                  <li key={f.path} className="row">
                    <span className={`commit-status s-${f.status}`} title={t(statusKey(f.status))}>
                      {f.status}
                    </span>
                    <span className="mono ellipsis" title={f.path}>
                      {f.path}
                    </span>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </>
      )}
    </Dialog>
  );
}
