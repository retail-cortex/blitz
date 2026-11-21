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

import type { MutableRefObject, ReactNode } from "react";
import { mdiChevronDown, mdiChevronRight, mdiFolderOpenOutline, mdiFolderOutline, mdiLinkVariant, mdiLockOutline } from "@mdi/js";
import { Icon } from "@blitz/webui";
import { FileKind, type FileEntry } from "../gen/blitz/v1/file_pb";
import { t } from "../i18n";
import { stagedKey } from "./gitMenu";
import { fileIcon } from "./icons";
import { NameInput } from "./ShelfDialogs";
import { gitLetters, type Naming } from "./shelfTypes";
import { isFolder, rangeOf, rowLabel, type Row, type Tree } from "./tree";

/** One row in the Files shelf tree, with compacted chains, inline rename, and truncation footer. */
export function ShelfTreeRow({
  row: r,
  shown,
  tree,
  selected,
  picked,
  active,
  dropInto,
  naming,
  problems,
  clip,
  more,
  anchor,
  isPicked,
  onSetSelected,
  onSetLead,
  onSetPicked,
  onToggle,
  onOpen,
  onFinishNaming,
  onStartRowDrag,
  onEndDrag,
  onContextMenu,
  onShowMore,
  renderNewRow,
}: {
  row: Row;
  shown: Row[];
  tree: Tree;
  selected: string | null;
  picked: ReadonlySet<string>;
  active: string | null;
  dropInto: string | null;
  naming: Naming | null;
  problems: ReadonlyMap<string, "error" | "warning">;
  clip: { paths: string[]; cut: boolean } | null;
  more: string | undefined;
  anchor: MutableRefObject<string | null>;
  isPicked: (path: string) => boolean;
  onSetSelected: (path: string | null) => void;
  onSetLead: (path: string | null) => void;
  onSetPicked: (picked: ReadonlySet<string>) => void;
  onToggle: (entry: FileEntry, open?: boolean) => Promise<void>;
  onOpen: (path: string) => void;
  onFinishNaming: (name: string) => Promise<void>;
  onStartRowDrag: (ev: React.DragEvent, path: string) => void;
  onEndDrag: () => void;
  onContextMenu: (pos: { x: number; y: number; entry: FileEntry }) => void;
  onShowMore: (folder: string) => void;
  renderNewRow: (folder: string, depth: number) => ReactNode;
}) {
  const { entry: e, depth } = r;
  const open = tree.expanded.has(e.path);
  const opens = isFolder(e) || !!r.nests;
  // A chain's folder renames on its row too (#110).
  const renamed = naming?.kind === "rename" ? (r.chain?.find((c) => c.path === naming.path) ?? (naming.path === e.path ? e : undefined)) : undefined;
  const renaming = !!renamed;
  const problem = problems.get(e.path);
  const cut = clip?.cut && clip.paths.some((p) => e.path === p || e.path.startsWith(p + "/"));
  // The folder of r's compacted chain a new name is being given in, if any.
  const chainNaming = naming && naming.kind !== "rename" ? r.chain?.find((c) => c.path === naming.dir)?.path : undefined;

  return (
    <div role="none">
      {renaming ? (
        <NameInput depth={depth} icon={isFolder(renamed!) ? mdiFolderOutline : fileIcon(renamed!.name)} initial={renamed!.name} label={t("desktop.files.rename")} onDone={onFinishNaming} />
      ) : (
        <button
          role="treeitem"
          aria-expanded={opens ? open : undefined}
          aria-selected={isPicked(e.path)}
          data-path={e.path}
          className={`tree-row ${e.path === selected ? "selected" : ""} ${picked.has(e.path) ? "picked" : ""} ${cut ? "cut" : ""} ${e.path === active ? "active" : ""} ${e.path === dropInto ? "drop-target" : ""} ${e.hidden ? "hidden-entry" : ""} git-${e.git || "clean"} ${problem ? `problem-${problem}` : ""}`}
          style={{ paddingLeft: 8 + depth * 14 }}
          title={[e.path, e.agentRule && t(`desktop.files.rule.${e.agentRule}.detail`), problem && t(`desktop.files.problem.${problem}`)].filter(Boolean).join("\n")}
          tabIndex={e.path === selected || (!selected && shown[0]?.entry.path === e.path) ? 0 : -1}
          draggable
          onDragStart={(ev) => onStartRowDrag(ev, e.path)}
          onDragEnd={onEndDrag}
          onClick={(ev) => {
            // ⌘ or Ctrl adds the row to the selection or takes it out;
            // Shift selects the rows from the last one clicked (FIL-74).
            if (ev.metaKey || ev.ctrlKey) {
              const all = new Set([...picked, ...(selected ? [selected] : [])]);
              if (all.has(e.path)) all.delete(e.path);
              else all.add(e.path);
              const lead = all.has(e.path) ? e.path : ([...all].pop() ?? null);
              if (lead) all.delete(lead);
              onSetLead(lead);
              onSetPicked(all);
              anchor.current = e.path;
              return;
            }
            if (ev.shiftKey && (anchor.current ?? selected)) {
              const from = anchor.current ?? selected!;
              onSetLead(e.path);
              onSetPicked(new Set(rangeOf(shown, from, e.path)));
              anchor.current = from;
              return;
            }
            onSetSelected(e.path);
            if (isFolder(e)) void onToggle(e);
            else onOpen(e.path);
          }}
          onContextMenu={(ev) => {
            ev.preventDefault();
            if (!isPicked(e.path)) onSetSelected(e.path);
            onContextMenu({ x: ev.clientX, y: ev.clientY, entry: e });
          }}
        >
          <span
            className="tree-twisty"
            onClick={(ev) => {
              // A file's twisty opens its nested files; the file itself opens with the row.
              if (!r.nests) return;
              ev.stopPropagation();
              void onToggle(e);
            }}
          >
            {opens && <Icon path={open ? mdiChevronDown : mdiChevronRight} size="sm" />}
          </span>
          <Icon path={isFolder(e) ? (open ? mdiFolderOpenOutline : mdiFolderOutline) : fileIcon(e.name)} size="sm" className="tree-icon" />
          <span className="ellipsis tree-name">
            {r.chain
              ? r.chain.map((c, k) => {
                  // Each folder of a compacted chain but the last
                  // (the row's own) is a target of its own: a click
                  // selects it, a drop goes into it, its menu is
                  // its (#110).
                  const last = k === r.chain!.length - 1;
                  return (
                    <span key={c.path}>
                      {k > 0 && <span className="tree-chain-sep">/</span>}
                      <span
                        data-folder={last ? undefined : c.path}
                        className={last ? undefined : `tree-chain-part ${c.path === selected ? "selected" : ""} ${c.path === dropInto ? "drop-target" : ""}`}
                        onClick={
                          last
                            ? undefined
                            : (ev) => {
                                if (ev.metaKey || ev.ctrlKey || ev.shiftKey) return;
                                ev.stopPropagation();
                                onSetSelected(c.path);
                              }
                        }
                        onContextMenu={
                          last
                            ? undefined
                            : (ev) => {
                                ev.preventDefault();
                                ev.stopPropagation();
                                onSetSelected(c.path);
                                onContextMenu({ x: ev.clientX, y: ev.clientY, entry: c });
                              }
                        }
                      >
                        {c.name}
                      </span>
                    </span>
                  );
                })
              : rowLabel(r)}
          </span>
          {e.kind === FileKind.SYMLINK && <Icon path={mdiLinkVariant} size="sm" className="muted" />}
          {e.agentRule && <Icon path={mdiLockOutline} size="sm" className="muted" />}
          {problem && !isFolder(e) && <span className="tree-problem" aria-label={t(`desktop.files.problem.${problem}`)} />}
          {e.git === "changed" ? <span className="git-dot" /> : e.git && (
              <span className={`git-letter ${e.staged ? `staged-${e.staged}` : ""}`} title={t(stagedKey(e.staged))}>
                {gitLetters[e.git] ?? ""}
              </span>
            )}
        </button>
      )}
      {/* A new name in any folder of a compacted chain goes under its row, open or not: the chain's first folders have none (#110). */}
      {isFolder(e) && (chainNaming ? renderNewRow(chainNaming, depth + 1) : open && renderNewRow(e.path, depth + 1))}
      {more !== undefined && (
        <button className="tree-row tree-more" style={{ paddingLeft: 8 + (more === "" ? 0 : (shown.find((x) => x.entry.path === more)?.depth ?? 0) + 1) * 14 }} onClick={() => onShowMore(more)}>
          <span className="tree-twisty" />
          <span className="ellipsis tree-name">{t("desktop.files.show_more", { n: (tree.children.get(more)?.length ?? 0).toLocaleString() })}</span>
        </button>
      )}
    </div>
  );
}
