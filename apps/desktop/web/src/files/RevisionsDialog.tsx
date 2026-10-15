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
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { mdiHistory, mdiRestore } from "@mdi/js";
import { files } from "../api";
import { DiffView } from "../Changes";
import { message } from "../errors";
import type { FileRevision } from "../gen/blitz/v1/file_pb";
import { language, t } from "../i18n";
import { parseDiff } from "../turns";
import { Button, Dialog, useSnackbar } from "../ui/controls";

/**
 * View revisions (FIL-59a): the commits that changed a file, newest first,
 * how the file today differs from the one picked, and putting it back as
 * it was there (asked first; the index is left alone).
 */
export function RevisionsDialog({ dir, path, onClose, onRestored }: { dir: string; path: string; onClose: () => void; onRestored: () => void }) {
  const snack = useSnackbar();
  const name = path.slice(path.lastIndexOf("/") + 1);
  const [revisions, setRevisions] = useState<FileRevision[] | null>(null);
  const [error, setError] = useState("");
  const [picked, setPicked] = useState<FileRevision | null>(null);
  const [diff, setDiff] = useState<string | null>(null);
  const [confirming, setConfirming] = useState(false);

  useEffect(() => {
    let live = true;
    files.getFileHistory({ workspace: dir, path }).then(
      (r) => {
        if (!live) return;
        setRevisions(r.revisions);
        setPicked(r.revisions[0] ?? null);
      },
      (e) => live && setError(message(e)),
    );
    return () => {
      live = false;
    };
  }, [dir, path]);

  useEffect(() => {
    if (!picked) return;
    let live = true;
    setDiff(null);
    files.getFileRevisionDiff({ workspace: dir, path, commit: picked.commit }).then(
      (r) => live && setDiff(r.diff),
      (e) => live && setError(message(e)),
    );
    return () => {
      live = false;
    };
  }, [dir, path, picked]);

  const restore = async (rev: FileRevision) => {
    setConfirming(false);
    try {
      const { versions } = await files.statFiles({ workspace: dir, paths: [path] });
      await files.restoreFileRevision({ workspace: dir, path, commit: rev.commit, version: versions[path] ?? "" });
      snack(t("desktop.files.revisions.restored", { name, commit: rev.short }));
      onRestored();
      onClose();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };

  const when = (r: FileRevision) => (r.when ? timestampDate(r.when).toLocaleString(language(), { dateStyle: "medium", timeStyle: "short" }) : "");
  return (
    <Dialog
      title={t("desktop.files.revisions.title", { name })}
      icon={mdiHistory}
      large
      onClose={onClose}
      closeButton
      footer={
        picked && (
          <Button variant="filled" icon={mdiRestore} disabled={!diff} onClick={() => setConfirming(true)}>
            {t("desktop.files.revisions.restore")}
          </Button>
        )
      }
    >
      {error && <p className="error-text">{error}</p>}
      {!error && revisions?.length === 0 && <p className="muted">{t("desktop.files.revisions.none")}</p>}
      {revisions && revisions.length > 0 && (
        <div className="revisions">
          <ul className="revision-list" role="listbox" aria-label={t("desktop.files.revisions.list")}>
            {revisions.map((r) => (
              <li key={r.commit}>
                <button type="button" role="option" aria-selected={r.commit === picked?.commit} className={`revision ${r.commit === picked?.commit ? "selected" : ""}`} onClick={() => setPicked(r)}>
                  <span className="t-body-md ellipsis">{r.subject}</span>
                  <span className="t-body-sm muted ellipsis">
                    <code>{r.short}</code> · {r.author} · {when(r)}
                  </span>
                </button>
              </li>
            ))}
          </ul>
          <div className="revision-diff">
            {picked && diff && <p className="t-body-sm muted">{t("desktop.files.revisions.legend", { commit: picked.short })}</p>}
            {diff === null ? <p className="muted">{t("desktop.files.revisions.loading")}</p> : diff === "" ? <p className="muted">{t("desktop.files.revisions.same")}</p> : <DiffView files={parseDiff(diff)} compact />}
          </div>
        </div>
      )}
      {confirming && picked && (
        <Dialog
          title={t("desktop.files.revisions.confirm_title", { name, commit: picked.short })}
          icon={mdiRestore}
          onClose={() => setConfirming(false)}
          footer={
            <>
              <Button onClick={() => setConfirming(false)}>{t("desktop.cancel")}</Button>
              <Button variant="filled" danger onClick={() => void restore(picked)}>
                {t("desktop.files.revisions.restore")}
              </Button>
            </>
          }
        >
          <p>{t("desktop.files.revisions.confirm_body", { name, commit: picked.short, subject: picked.subject })}</p>
        </Dialog>
      )}
    </Dialog>
  );
}
