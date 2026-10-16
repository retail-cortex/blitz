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

// The Changes page and the workspace's branches (spec_desktop_024 DSK-90c):
// switch to one, create one, or merge one into the current branch. A
// merge that stops on conflicts waits in MergeBanner, as a pull's does.

import { useState } from "react";
import { mdiCheck, mdiChevronDown, mdiSourceBranch, mdiSourceBranchPlus, mdiSourceMerge } from "@mdi/js";
import { workspaces } from "./api";
import { filesTouched } from "./events";
import { t } from "./i18n";
import { PullOutcome, type Branch, type GetGitStatusResponse } from "./gen/blitz/v1/workspace_pb";
import { inProgress, mergeMessage, remoteErrorText } from "./gitRemote";
import { Button, Dialog, Field, Icon, Menu, useSnackbar, type MenuEntry } from "./ui/controls";

type Props = {
  dir: string;
  git: GetGitStatusResponse;
  /** The branch or the working tree changed: reload everything. */
  onChanged: () => void;
};

/** The current branch, as a button that opens the branches: switch, create, merge. */
export function BranchMenu({ dir, git, onChanged }: Props) {
  const snack = useSnackbar();
  const [branches, setBranches] = useState<Branch[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [creating, setCreating] = useState(false);
  const [merging, setMerging] = useState<string | null>(null);

  const load = () => {
    workspaces
      .listBranches({ workspace: dir })
      .then((r) => setBranches(r.branches))
      .catch((e) => {
        setBranches([]);
        snack(remoteErrorText(e), { error: true });
      });
  };
  const switchTo = async (name: string) => {
    setBusy(true);
    try {
      const res = await workspaces.switchBranch({ workspace: dir, branch: name });
      snack(t("desktop.git.switched", { branch: res.status?.branch ?? name }));
      filesTouched({ dir });
      onChanged();
    } catch (e) {
      snack(remoteErrorText(e), { error: true });
    } finally {
      setBusy(false);
    }
  };

  const others = (branches ?? []).filter((b) => !b.current);
  const row = (b: Branch): MenuEntry => ({
    label: b.name,
    detail: b.subject,
    icon: b.current ? mdiCheck : mdiSourceBranch,
    on: b.current,
    onSelect: () => {
      if (!b.current) void switchTo(b.name);
    },
    actions: b.current ? undefined : [{ icon: mdiSourceMerge, label: t("desktop.git.merge_title", { branch: git.branch }), onSelect: () => setMerging(b.name) }],
  });
  const local = (branches ?? []).filter((b) => !b.remote);
  const remote = (branches ?? []).filter((b) => b.remote);
  const items: MenuEntry[] = [
    { heading: t("desktop.git.branches_local") },
    ...(branches === null ? [{ label: t("desktop.loading"), disabled: true, onSelect: () => {} }] : local.map(row)),
    ...(remote.length > 0 ? [{ heading: t("desktop.git.branches_remote") }, ...remote.map(row)] : []),
    ...(branches !== null && others.length === 0 ? [{ label: t("desktop.git.no_other_branches"), disabled: true, onSelect: () => {} }] : []),
    "divider",
    { label: t("desktop.git.new_branch"), icon: mdiSourceBranchPlus, onSelect: () => setCreating(true) },
    { label: t("desktop.git.merge_into", { branch: git.branch }), icon: mdiSourceMerge, disabled: others.length === 0, onSelect: () => setMerging(others[0]?.name ?? "") },
  ];

  const blocked = busy || inProgress(git);
  return (
    <>
      <Menu
        className="branch-menu"
        items={items}
        trigger={({ onClick, ...aria }) => (
          <Button
            variant="tonal"
            small
            icon={mdiSourceBranch}
            disabled={blocked}
            title={inProgress(git) ? t("desktop.git.err.busy") : t("desktop.git.branches")}
            aria-label={`${t("desktop.git.branches")}: ${git.branch}`}
            {...aria}
            onClick={() => {
              if (!aria["aria-expanded"]) load();
              onClick();
            }}
          >
            <span className="ellipsis branch-name">{git.branch}</span>
            <Icon path={mdiChevronDown} size="sm" />
          </Button>
        )}
      />
      {creating && <NewBranchDialog dir={dir} from={git.branch} onClose={() => setCreating(false)} onChanged={onChanged} />}
      {merging !== null && <MergeDialog dir={dir} into={git.branch} branches={others} initial={merging} onClose={() => setMerging(null)} onChanged={onChanged} />}
    </>
  );
}

/** Asks for a new branch's name; it starts at the current commit, and Blitz switches to it. */
function NewBranchDialog({ dir, from, onClose, onChanged }: { dir: string; from: string; onClose: () => void; onChanged: () => void }) {
  const snack = useSnackbar();
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const create = async () => {
    const branch = name.trim();
    if (!branch || busy) return;
    setBusy(true);
    try {
      const res = await workspaces.createBranch({ workspace: dir, branch });
      snack(t("desktop.git.created", { branch: res.status?.branch ?? branch }));
      filesTouched({ dir });
      onChanged();
      onClose();
    } catch (e) {
      setError(remoteErrorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={t("desktop.git.new_branch_title")}
      icon={mdiSourceBranchPlus}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>{t("desktop.cancel")}</Button>
          <Button variant="filled" icon={mdiSourceBranchPlus} disabled={busy || !name.trim()} onClick={() => void create()}>
            {t("desktop.git.create_branch")}
          </Button>
        </>
      }
    >
      <Field label={t("desktop.git.branch_name")} supporting={t("desktop.git.new_branch_hint", { branch: from })} error={error}>
        {(id) => (
          <input
            id={id}
            className="input mono"
            autoFocus
            spellCheck={false}
            autoComplete="off"
            value={name}
            placeholder={t("desktop.git.branch_placeholder")}
            onChange={(e) => {
              setName(e.target.value);
              setError("");
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                void create();
              }
            }}
          />
        )}
      </Field>
    </Dialog>
  );
}

/** Picks a branch to merge into the current one, and says what merging does. */
function MergeDialog({ dir, into, branches, initial, onClose, onChanged }: { dir: string; into: string; branches: Branch[]; initial: string; onClose: () => void; onChanged: () => void }) {
  const snack = useSnackbar();
  const [from, setFrom] = useState(initial || branches[0]?.name || "");
  const [busy, setBusy] = useState(false);
  const merge = async () => {
    if (!from || busy) return;
    setBusy(true);
    try {
      const res = await workspaces.mergeBranch({ workspace: dir, branch: from });
      snack(mergeMessage(res, from, into), { error: res.outcome === PullOutcome.CONFLICTS });
      filesTouched({ dir });
      onChanged();
      onClose();
    } catch (e) {
      snack(remoteErrorText(e), { error: true });
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      title={t("desktop.git.merge_title", { branch: into })}
      icon={mdiSourceMerge}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>{t("desktop.cancel")}</Button>
          <Button variant="filled" icon={mdiSourceMerge} disabled={busy || !from} onClick={() => void merge()}>
            {t("desktop.git.merge")}
          </Button>
        </>
      }
    >
      <Field label={t("desktop.git.merge_from")} supporting={t("desktop.git.merge_hint", { from: from || "…", into })}>
        {(id) => (
          <select id={id} className="select" value={from} onChange={(e) => setFrom(e.target.value)}>
            {branches.map((b) => (
              <option key={b.name} value={b.name}>
                {b.name}
              </option>
            ))}
          </select>
        )}
      </Field>
    </Dialog>
  );
}
