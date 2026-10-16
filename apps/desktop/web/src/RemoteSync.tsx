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

// The Changes page and the workspace's remote (spec_desktop_024 DSK-90b):
// Pull and Push with the ahead and behind counts, and a pull's conflicts —
// the merge or rebase waiting, and each conflicted file.

import { useEffect, useState } from "react";
import { mdiArrowDown, mdiArrowUp, mdiCheck, mdiClose, mdiFileEditOutline, mdiRobotOutline, mdiSourceMerge } from "@mdi/js";
import { workspaces } from "./api";
import { compose, filesTouched, openFile, showView } from "./events";
import { t, tn } from "./i18n";
import { ConflictChoice, GitOperation, PullOutcome, type GetGitStatusResponse } from "./gen/blitz/v1/workspace_pb";
import { inProgress, pullMessage, remoteErrorText, resolvePrompt, syncTitle } from "./gitRemote";
import { Button, Dialog, Icon, useSnackbar } from "./ui/controls";

type Props = {
  dir: string;
  git: GetGitStatusResponse;
  /** A fresh git status (a fetch): the counts change, nothing else. */
  onStatus: (git: GetGitStatusResponse) => void;
  /** The working tree changed (a pull, a resolved file): reload everything. */
  onChanged: () => void;
};

/** Pull and Push, with what each would bring or send; fetches once on showing. */
export function RemoteButtons({ dir, git, onStatus, onChanged }: Props) {
  const snack = useSnackbar();
  const [busy, setBusy] = useState<"" | "pull" | "push">("");
  useEffect(() => {
    let live = true;
    workspaces
      .fetchRemote({ workspace: dir })
      .then((r) => live && r.status && onStatus(r.status))
      .catch(() => {}); // offline, or no sign-in: the counts stay as they were, and Pull says why
    return () => {
      live = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once per workspace
  }, [dir]);

  const pull = async () => {
    setBusy("pull");
    try {
      const res = await workspaces.pull({ workspace: dir });
      snack(pullMessage(res, git.upstream), { error: res.outcome === PullOutcome.CONFLICTS });
      filesTouched({ dir });
      onChanged();
    } catch (e) {
      snack(remoteErrorText(e), { error: true });
    } finally {
      setBusy("");
    }
  };
  const push = async () => {
    setBusy("push");
    try {
      const res = await workspaces.push({ workspace: dir });
      snack(t("desktop.git.pushed", { target: res.target }));
      if (res.status) onStatus(res.status);
    } catch (e) {
      snack(remoteErrorText(e), { error: true });
    } finally {
      setBusy("");
    }
  };

  const blocked = busy !== "" || inProgress(git);
  const title = syncTitle(git);
  return (
    <>
      <Button variant="tonal" small icon={mdiArrowDown} disabled={blocked || !git.upstream} onClick={() => void pull()} title={title}>
        {busy === "pull" ? t("desktop.git.pulling") : t("desktop.git.pull")}
        {git.behind > 0 && <span className="sync-count">{git.behind}</span>}
      </Button>
      <Button variant="tonal" small icon={mdiArrowUp} disabled={blocked} onClick={() => void push()} title={title}>
        {busy === "push" ? t("desktop.git.pushing") : git.upstream ? t("desktop.git.push") : t("desktop.git.publish")}
        {git.ahead > 0 && <span className="sync-count">{git.ahead}</span>}
      </Button>
    </>
  );
}

/** A merge or rebase a pull left waiting: its conflicts, and the ways out. */
export function MergeBanner({ dir, git, onChanged }: Omit<Props, "onStatus">) {
  const snack = useSnackbar();
  const [busy, setBusy] = useState(false);
  const [aborting, setAborting] = useState(false);
  const rebase = git.operation === GitOperation.REBASE;
  const left = git.conflicts.length;

  const run = async (f: () => Promise<unknown>, done: string) => {
    setBusy(true);
    try {
      await f();
      if (done) snack(done);
      filesTouched({ dir });
      onChanged();
    } catch (e) {
      snack(remoteErrorText(e), { error: true });
    } finally {
      setBusy(false);
    }
  };
  const askAgent = () => {
    showView({ dir, view: "chat" });
    compose({ dir, text: resolvePrompt(git), run: true });
  };

  return (
    <div className="card merge-banner" role="status">
      <div className="row">
        <Icon path={mdiSourceMerge} />
        <span className="t-title-sm spacer">
          {rebase ? t("desktop.git.rebasing", { upstream: git.upstream }) : t("desktop.git.merging", { upstream: git.upstream })}
        </span>
      </div>
      <p className="muted t-body-sm">{left > 0 ? tn("desktop.git.conflicts_left", left) : rebase ? t("desktop.git.ready_rebase") : t("desktop.git.ready_merge")}</p>
      <div className="row wrap">
        {left > 0 && (
          <Button variant="tonal" small icon={mdiRobotOutline} disabled={busy} onClick={askAgent}>
            {t("desktop.git.ask_agent")}
          </Button>
        )}
        <span className="spacer" />
        <Button small icon={mdiClose} disabled={busy} onClick={() => setAborting(true)}>
          {rebase ? t("desktop.git.abort_rebase") : t("desktop.git.abort_merge")}
        </Button>
        <Button variant="filled" small icon={mdiCheck} disabled={busy || left > 0} onClick={() => void run(() => workspaces.completeMerge({ workspace: dir }), rebase ? "" : t("desktop.git.merged"))}>
          {rebase ? t("desktop.git.continue_rebase") : t("desktop.git.complete_merge")}
        </Button>
      </div>
      {aborting && (
        <Dialog
          title={rebase ? t("desktop.git.abort_rebase") : t("desktop.git.abort_merge")}
          icon={mdiSourceMerge}
          onClose={() => setAborting(false)}
          footer={
            <>
              <Button onClick={() => setAborting(false)}>{t("desktop.cancel")}</Button>
              <Button
                variant="filled"
                danger
                onClick={() => {
                  setAborting(false);
                  void run(() => workspaces.abortMerge({ workspace: dir }), t("desktop.git.aborted"));
                }}
              >
                {rebase ? t("desktop.git.abort_rebase") : t("desktop.git.abort_merge")}
              </Button>
            </>
          }
        >
          <p className="muted">{t("desktop.git.abort_detail")}</p>
        </Dialog>
      )}
    </div>
  );
}

/** A conflicted file's ways out: open it, keep a side, or mark it resolved. */
export function ConflictBar({ dir, path, onChanged }: { dir: string; path: string; onChanged: () => void }) {
  const snack = useSnackbar();
  const [busy, setBusy] = useState(false);
  const resolve = async (choice: ConflictChoice) => {
    setBusy(true);
    try {
      await workspaces.resolveConflict({ workspace: dir, path, choice });
      filesTouched({ dir });
      onChanged();
    } catch (e) {
      snack(remoteErrorText(e), { error: true });
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="conflict-bar row wrap">
      <span className="t-body-sm spacer">{t("desktop.git.file_conflicted")}</span>
      <Button small icon={mdiFileEditOutline} onClick={() => openFile({ dir, path })}>
        {t("desktop.git.open_file")}
      </Button>
      <Button variant="tonal" small disabled={busy} onClick={() => void resolve(ConflictChoice.MINE)}>
        {t("desktop.git.keep_mine")}
      </Button>
      <Button variant="tonal" small disabled={busy} onClick={() => void resolve(ConflictChoice.THEIRS)}>
        {t("desktop.git.take_theirs")}
      </Button>
      <Button variant="filled" small icon={mdiCheck} disabled={busy} onClick={() => void resolve(ConflictChoice.RESOLVED)}>
        {t("desktop.git.mark_resolved")}
      </Button>
    </div>
  );
}
