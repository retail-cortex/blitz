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
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { mdiAlertCircleOutline, mdiFileCompare, mdiRefresh, mdiSourceBranch, mdiTextBoxOutline, mdiUndoVariant } from "@mdi/js";
import { sessions, workspaces } from "./api";
import { message, reason } from "./errors";
import { language, t, tn } from "./i18n";
import type { Checkpoint } from "./gen/blitz/v1/workspace_pb";
import { languageFor } from "./highlight";
import { Markdown } from "./Markdown";
import { parseDiff, type FileDiff } from "./turns";
import { highlight } from "./highlight";
import { Button, Dialog, Icon, IconButton, Segmented, useSnackbar } from "./ui/controls";

const lang = (path: string) => languageFor(path);

/** Coloured, highlighted diff of one or more files. */
export function DiffView({ files, compact }: { files: FileDiff[]; compact?: boolean }) {
  return (
    <div className={`diff ${compact ? "compact" : ""}`}>
      {files.map((f) => (
        <div key={f.path} className="diff-file">
          <div className="diff-file-head">
            <code className="ellipsis">{f.path}</code>
            <span className="spacer" />
            <span className="add-count">+{f.added}</span>
            <span className="del-count">−{f.removed}</span>
          </div>
          <pre className="diff-lines hljs">
            {f.lines.map((l, i) => (
              <div key={i} className={`dl ${l.kind}`}>
                {l.kind === "add" || l.kind === "del" || l.kind === "ctx" ? (
                  <>
                    <span className="dl-sign">{l.text.slice(0, 1) || " "}</span>
                    {highlight(l.text.slice(1), lang(f.path))}
                  </>
                ) : (
                  l.text || " "
                )}
              </div>
            ))}
          </pre>
        </div>
      ))}
    </div>
  );
}

type Source = "session" | "git";

/**
 * What the agent changed in this session, file by file, beside its latest
 * summary of the work (its walkthrough), with the turns that changed
 * files and undo.
 */
export function Changes({ dir }: { dir: string }) {
  const snack = useSnackbar();
  const [source, setSource] = useState<Source>("session");
  const [files, setFiles] = useState<FileDiff[] | null>(null);
  const [selected, setSelected] = useState("");
  const [summary, setSummary] = useState("");
  const [checkpoints, setCheckpoints] = useState<Checkpoint[]>([]);
  const [error, setError] = useState("");
  const [conflict, setConflict] = useState("");

  const refresh = useCallback(async () => {
    setError("");
    try {
      const [d, s, c] = await Promise.all([
        workspaces.getDiff({ workspace: dir, git: source === "git" }),
        sessions.getActiveSession({ workspace: dir }),
        workspaces.listCheckpoints({ workspace: dir }),
      ]);
      const parsed = parseDiff(d.diff);
      setFiles(parsed);
      setSelected((cur) => (parsed.some((f) => f.path === cur) ? cur : (parsed[0]?.path ?? "")));
      const last = [...(s.session?.messages ?? [])].reverse().find((m) => m.role === "model");
      setSummary(last?.text ?? "");
      setCheckpoints(c.checkpoints);
    } catch (e) {
      setError(message(e));
    }
  }, [dir, source]);
  useEffect(() => {
    refresh();
  }, [refresh]);

  const undo = async (force = false) => {
    try {
      const res = await workspaces.undo({ workspace: dir, force });
      snack(t("desktop.changes.undid", { label: res.label, files: res.restored.join(", ") || t("desktop.changes.nothing_restored") }));
      if (res.error) snack(res.error.message, { error: true });
      refresh();
    } catch (e) {
      if (reason(e) === "UNDO_CONFLICT") setConflict(message(e));
      else snack(message(e), { error: true });
    }
  };

  const file = files?.find((f) => f.path === selected);
  const totals = (files ?? []).reduce((t, f) => ({ added: t.added + f.added, removed: t.removed + f.removed }), { added: 0, removed: 0 });
  return (
    <div className="changes">
      <div className="changes-toolbar">
        <Segmented<Source>
          label={t("desktop.changes.shown")}
          small
          value={source}
          onChange={setSource}
          options={[
            { value: "session", label: t("desktop.changes.session"), icon: mdiFileCompare },
            { value: "git", label: t("desktop.changes.git"), icon: mdiSourceBranch },
          ]}
        />
        {files && files.length > 0 && (
          <span className="t-body-sm muted">
            {tn("desktop.changes.count", files.length)} · <span className="add-count">+{totals.added}</span> <span className="del-count">−{totals.removed}</span>
          </span>
        )}
        <span className="spacer" />
        <IconButton icon={mdiRefresh} label={t("desktop.refresh")} onClick={refresh} />
        <Button variant="tonal" small icon={mdiUndoVariant} disabled={checkpoints.length === 0} onClick={() => undo()}>
          {t("desktop.changes.undo_last")}
        </Button>
      </div>
      {error && (
        <div className="card error row">
          <Icon path={mdiAlertCircleOutline} /> {error}
        </div>
      )}
      <div className="changes-body">
        <aside className="changes-side">
          <div className="t-label muted side-label">{t("desktop.changes.files")}</div>
          {files?.length === 0 && <p className="muted t-body-sm">{source === "session" ? t("desktop.changes.none_session") : t("desktop.changes.none_git")}</p>}
          <div className="list">
            {files?.map((f) => (
              <button key={f.path} className={`list-item file-item ${f.path === selected ? "active" : ""}`} onClick={() => setSelected(f.path)} title={f.path}>
                <span className="lines">
                  <span className="ellipsis mono">{f.path.split("/").pop()}</span>
                  <small className="ellipsis">{f.path}</small>
                </span>
                <span className="trailing t-body-sm">
                  <span className="add-count">+{f.added}</span>
                  <span className="del-count">−{f.removed}</span>
                </span>
              </button>
            ))}
          </div>
          {source === "session" && checkpoints.length > 0 && (
            <>
              <div className="t-label muted side-label">{t("desktop.changes.turns")}</div>
              <ul className="timeline">
                {checkpoints.map((c) => (
                  <li key={c.id}>
                    <span className="ellipsis">{c.label}</span>
                    <small className="muted">
                      {c.time && timestampDate(c.time).toLocaleTimeString(language())} · {tn("desktop.changes.count", c.files.length)}
                    </small>
                  </li>
                ))}
              </ul>
            </>
          )}
        </aside>
        <div className="changes-main">
          {summary && source === "session" && files && files.length > 0 && (
            <details className="card walkthrough" open>
              <summary className="row t-title-sm">
                <Icon path={mdiTextBoxOutline} size="sm" /> {t("desktop.changes.summary")}
              </summary>
              <Markdown text={summary} />
            </details>
          )}
          {file ? <DiffView files={[file]} /> : files === null ? <p className="muted">{t("desktop.loading")}</p> : null}
        </div>
      </div>
      {conflict && (
        <Dialog
          title={t("desktop.conflict.title")}
          icon={mdiAlertCircleOutline}
          onClose={() => setConflict("")}
          footer={
            <>
              <Button onClick={() => setConflict("")}>{t("desktop.conflict.keep")}</Button>
              <Button
                variant="filled"
                danger
                onClick={() => {
                  setConflict("");
                  undo(true);
                }}
              >
                {t("desktop.changes.undo_anyway")}
              </Button>
            </>
          }
        >
          <p className="muted">{conflict}</p>
        </Dialog>
      )}
    </div>
  );
}
