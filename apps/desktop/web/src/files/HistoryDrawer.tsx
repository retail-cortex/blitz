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

import { useCallback, useEffect, useMemo, useState, type MouseEvent } from "react";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import { mdiClose, mdiHistory, mdiRestore } from "@mdi/js";
import { files } from "../api";
import { DiffView } from "../Changes";
import { message } from "../errors";
import { RevisionDiff, type FileRevision, type RestoredFile } from "../gen/blitz/v1/file_pb";
import { language, t, tn } from "../i18n";
import { parseDiff } from "../turns";
import { Button, Dialog, IconButton, Segmented, useSnackbar } from "../ui/controls";
import { Drawer } from "../ui/Drawer";
import { authorHue, changeShare, dateGroup, sameGroup, type DateGroup } from "./history";
import { layoutGraph, type GraphRow } from "./historyGraph";

/** Rows' height and the lanes' spacing, in pixels. */
const rowHeight = 52;
const laneWidth = 14;
const pageSize = 100;

/**
 * The history of a file, a folder or the workspace (FIL-59b): its commits
 * as a graph, by date, with who made them and how much they changed. One
 * commit selected shows what it changed, or how it differs from now;
 * Shift-click (or ⌘-click) a second to compare the two. Restore puts a
 * file, or a folder's files, back as they were (files added since stay).
 */
export function HistoryDrawer({ dir, path, folder, onClose, onRestored }: { dir: string; path: string; folder: boolean; onClose: () => void; onRestored: () => void }) {
  const snack = useSnackbar();
  const name = path ? path.slice(path.lastIndexOf("/") + 1) : dir.replace(/\/+$/, "").slice(dir.replace(/\/+$/, "").lastIndexOf("/") + 1);
  const [revisions, setRevisions] = useState<FileRevision[] | null>(null);
  const [more, setMore] = useState(false);
  const [error, setError] = useState("");
  const [selected, setSelected] = useState<string[]>([]);
  const [against, setAgainst] = useState<"parent" | "now">(folder ? "parent" : "now");
  const [diff, setDiff] = useState<{ text: string; truncated: boolean } | null>(null);
  const [confirming, setConfirming] = useState<{ rev: FileRevision; files?: RestoredFile[] } | null>(null);

  const load = useCallback(
    async (skip: number) => {
      try {
        const r = await files.getFileHistory({ workspace: dir, path, limit: pageSize, skip });
        setRevisions((old) => [...(skip ? (old ?? []) : []), ...r.revisions]);
        setMore(r.more);
        if (!skip && r.revisions[0]) setSelected([r.revisions[0].commit]);
      } catch (e) {
        setError(message(e));
      }
    },
    [dir, path],
  );
  useEffect(() => void load(0), [load]);

  const byCommit = useMemo(() => new Map((revisions ?? []).map((r, i) => [r.commit, i])), [revisions]);
  // Two selected: compared from the older to the newer.
  const pair = selected.length === 2 ? [...selected].sort((a, b) => (byCommit.get(b) ?? 0) - (byCommit.get(a) ?? 0)) : null;
  useEffect(() => {
    if (!selected.length) return;
    let live = true;
    setDiff(null);
    const req = pair
      ? { workspace: dir, path, commit: pair[0], against: RevisionDiff.OTHER, otherCommit: pair[1] }
      : { workspace: dir, path, commit: selected[0], against: against === "parent" ? RevisionDiff.PARENT : RevisionDiff.NOW };
    files.getFileRevisionDiff(req).then(
      (r) => live && setDiff({ text: r.diff, truncated: r.truncated }),
      (e) => live && setError(message(e)),
    );
    return () => {
      live = false;
    };
    // pair follows from selected
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dir, path, selected, against]);

  const select = (r: FileRevision, e: MouseEvent) => {
    const adding = (e.shiftKey || e.metaKey || e.ctrlKey) && selected.length > 0 && selected[0] !== r.commit;
    setSelected(adding ? [selected[0], r.commit] : [r.commit]);
  };

  const graph = useMemo(() => layoutGraph((revisions ?? []).map((r) => ({ commit: r.commit, parents: r.parents }))), [revisions]);
  const lanes = Math.max(1, ...graph.map((g) => g.width));
  const largest = Math.max(0, ...(revisions ?? []).map((r) => r.added + r.removed));
  const now = new Date();

  const startRestore = async (rev: FileRevision) => {
    if (!folder) return setConfirming({ rev });
    try {
      const r = await files.restoreFileRevision({ workspace: dir, path, commit: rev.commit, preview: true });
      if (!r.files.length) return snack(t("desktop.history.nothing_to_restore", { commit: rev.short }));
      setConfirming({ rev, files: r.files });
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const restore = async (rev: FileRevision) => {
    setConfirming(null);
    try {
      let version = "";
      if (!folder) version = (await files.statFiles({ workspace: dir, paths: [path] })).versions[path] ?? "";
      const r = await files.restoreFileRevision({ workspace: dir, path, commit: rev.commit, version });
      snack(folder ? tn("desktop.history.restored_files", r.files.length, { commit: rev.short }) : t("desktop.files.revisions.restored", { name, commit: rev.short }));
      onRestored();
      if (!folder) onClose();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };

  const one = selected.length === 1 ? (revisions ?? []).find((r) => r.commit === selected[0]) : undefined;
  const restoreLabel = !folder ? "desktop.files.revisions.restore" : path ? "desktop.history.restore_folder" : "desktop.history.restore_workspace";
  return (
    <Drawer
      title={t(path ? "desktop.files.revisions.title" : "desktop.history.workspace_title", { name })}
      icon={mdiHistory}
      className="history-drawer"
      onClose={onClose}
      footer={
        one && (
          <Button variant="filled" icon={mdiRestore} onClick={() => void startRestore(one)}>
            {t(restoreLabel)}
          </Button>
        )
      }
    >
      {error && <p className="error-text">{error}</p>}
      {revisions?.length === 0 && <p className="muted">{t(folder ? "desktop.history.none" : "desktop.files.revisions.none")}</p>}
      {revisions && revisions.length > 0 && (
        <div className="revisions history">
          <div className="revision-list" role="listbox" aria-label={t("desktop.files.revisions.list")} aria-multiselectable="true">
            {revisions.map((r, i) => {
              const when = r.when ? timestampDate(r.when) : now;
              const group = dateGroup(when, now);
              const previous = i > 0 && revisions[i - 1].when ? dateGroup(timestampDate(revisions[i - 1].when!), now) : null;
              return (
                <div key={r.commit}>
                  {(!previous || !sameGroup(previous, group)) && (
                    <div className="history-group">
                      <GraphCell row={i > 0 ? passThrough(graph[i - 1]) : null} lanes={lanes} height={28} />
                      <span className="t-label muted">{groupLabel(group)}</span>
                    </div>
                  )}
                  <button
                    type="button"
                    role="option"
                    aria-selected={selected.includes(r.commit)}
                    className={`history-row ${selected.includes(r.commit) ? "selected" : ""}`}
                    onClick={(e) => select(r, e)}
                    title={r.subject}
                  >
                    <GraphCell row={graph[i]} lanes={lanes} height={rowHeight} merge={r.parents.length > 1} />
                    <span className="history-text">
                      <span className="t-body-md ellipsis">{r.subject}</span>
                      <span className="t-body-sm muted ellipsis">
                        <code>{r.short}</code> · <span className="author-dot" style={{ background: `hsl(${authorHue(r.email || r.author)} 55% 50%)` }} /> {r.author} · {ago(when, now)}
                      </span>
                    </span>
                    <span className="history-size" title={t("desktop.history.size", { added: r.added, removed: r.removed, files: r.files })}>
                      <span className="history-bar" style={{ width: `${changeShare(r.added + r.removed, largest) * 100}%` }}>
                        <span className="add" style={{ flexGrow: r.added }} />
                        <span className="del" style={{ flexGrow: r.removed }} />
                      </span>
                      <span className="t-body-sm muted">
                        +{r.added} −{r.removed}
                      </span>
                    </span>
                  </button>
                </div>
              );
            })}
            {more && (
              <Button small onClick={() => void load(revisions.length)}>
                {t("desktop.history.older")}
              </Button>
            )}
          </div>
          <div className="revision-diff">
            <div className="history-head">
              {pair ? (
                <>
                  <span className="t-body-sm spacer">{t("desktop.history.comparing", { from: revisions[byCommit.get(pair[0]) ?? 0]?.short ?? "", to: revisions[byCommit.get(pair[1]) ?? 0]?.short ?? "" })}</span>
                  <IconButton icon={mdiClose} label={t("desktop.history.clear")} small onClick={() => setSelected([pair[1]])} />
                </>
              ) : (
                <>
                  <Segmented
                    small
                    label={t("desktop.history.show")}
                    value={against}
                    onChange={setAgainst}
                    options={[
                      { value: "parent", label: t("desktop.history.its_changes") },
                      { value: "now", label: t("desktop.history.vs_now") },
                    ]}
                  />
                  <span className="t-body-sm muted spacer">{t("desktop.history.compare_hint")}</span>
                </>
              )}
            </div>
            {diff?.truncated && <p className="t-body-sm muted">{t("desktop.history.truncated")}</p>}
            {diff === null ? (
              <p className="muted">{t("desktop.files.revisions.loading")}</p>
            ) : diff.text === "" ? (
              <p className="muted">{t("desktop.history.no_difference")}</p>
            ) : (
              <DiffView files={parseDiff(diff.text)} compact />
            )}
          </div>
        </div>
      )}
      {confirming && (
        <Dialog
          title={t(folder ? "desktop.history.confirm_folder_title" : "desktop.files.revisions.confirm_title", { name, commit: confirming.rev.short })}
          icon={mdiRestore}
          onClose={() => setConfirming(null)}
          footer={
            <>
              <Button onClick={() => setConfirming(null)}>{t("desktop.cancel")}</Button>
              <Button variant="filled" danger onClick={() => void restore(confirming.rev)}>
                {t(restoreLabel)}
              </Button>
            </>
          }
        >
          {confirming.files ? (
            <>
              <p>{tn("desktop.history.confirm_folder_body", confirming.files.length, { subject: confirming.rev.subject })}</p>
              <ul className="history-files">
                {confirming.files.slice(0, 20).map((f) => (
                  <li key={f.path}>
                    <code>{f.path}</code>
                    {f.recreated && <span className="muted"> · {t("desktop.history.recreated")}</span>}
                  </li>
                ))}
              </ul>
              {confirming.files.length > 20 && <p className="t-body-sm muted">{t("desktop.history.and_more", { count: confirming.files.length - 20 })}</p>}
            </>
          ) : (
            <p>{t("desktop.files.revisions.confirm_body", { name, commit: confirming.rev.short, subject: confirming.rev.subject })}</p>
          )}
        </Dialog>
      )}
    </Drawer>
  );
}

/** The lanes still going down below a row: a date heading's straight lines. */
function passThrough(row: GraphRow | undefined): GraphRow | null {
  if (!row) return null;
  const going = new Map<number, number>(); // lane: colour
  for (const s of row.segments) if (s.half === "bottom") going.set(s.to, s.color);
  return {
    lane: -1,
    color: 0,
    width: row.width,
    segments: [...going].flatMap(([lane, color]) => [
      { from: lane, to: lane, half: "top" as const, color },
      { from: lane, to: lane, half: "bottom" as const, color },
    ]),
  };
}

/** One row's lanes: its lines, and its commit's dot (none for a date heading). */
function GraphCell({ row, lanes, height, merge }: { row: GraphRow | null; lanes: number; height: number; merge?: boolean }) {
  const width = lanes * laneWidth + 8;
  const mid = height / 2;
  const x = (lane: number) => 8 + lane * laneWidth;
  return (
    <svg className="history-graph" width={width} height={height} viewBox={`0 0 ${width} ${height}`} aria-hidden="true">
      {row?.segments.map((s, i) => {
        const [y1, y2] = s.half === "top" ? [0, mid] : [mid, height];
        const [x1, x2] = [x(s.from), x(s.to)];
        const d = x1 === x2 ? `M${x1} ${y1}V${y2}` : `M${x1} ${y1}C${x1} ${y1 + (y2 - y1) * 0.7} ${x2} ${y1 + (y2 - y1) * 0.3} ${x2} ${y2}`;
        return <path key={i} d={d} className={`lane lane-${s.color % 8}`} />;
      })}
      {row && row.lane >= 0 && <circle cx={x(row.lane)} cy={mid} r={merge ? 3.5 : 4.5} className={`node lane-${row.color % 8} ${merge ? "merge" : ""}`} />}
    </svg>
  );
}

function groupLabel(g: DateGroup): string {
  if (g.kind !== "older") return t(`desktop.history.group.${g.kind}`);
  return new Date(g.year, g.month, 1).toLocaleDateString(language(), { year: "numeric", month: "long" });
}

/** How long ago, in words ("3 hours ago"). */
function ago(when: Date, now: Date): string {
  const s = Math.round((when.getTime() - now.getTime()) / 1000);
  const fmt = new Intl.RelativeTimeFormat(language(), { numeric: "auto" });
  const units: [Intl.RelativeTimeFormatUnit, number][] = [
    ["year", 31_536_000],
    ["month", 2_592_000],
    ["week", 604_800],
    ["day", 86_400],
    ["hour", 3_600],
    ["minute", 60],
  ];
  for (const [unit, secs] of units) if (Math.abs(s) >= secs) return fmt.format(Math.round(s / secs), unit);
  return fmt.format(0, "minute");
}
