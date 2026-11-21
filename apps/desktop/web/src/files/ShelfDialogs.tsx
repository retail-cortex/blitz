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
import { mdiFileReplaceOutline, mdiTrayArrowDown, mdiUndoVariant } from "@mdi/js";
import { Button, Dialog, Icon } from "@blitz/webui";
import { GitAction, type FileEntry } from "../gen/blitz/v1/file_pb";
import { t, tn } from "../i18n";
import { isFolder, validName } from "./tree";
import { someNames, type OnConflict } from "./upload";
import type { Incoming } from "./shelfTypes";

/**
 * Naming a new file or folder, or renaming one: Enter keeps it, Escape or
 * leaving the field drops it.
 */
export function NameInput({ depth, icon, initial, label, onDone }: { depth: number; icon: string; initial: string; label: string; onDone: (name: string) => void }) {
  const [value, setValue] = useState(initial);
  const ref = useRef<HTMLInputElement>(null);
  const done = useRef(false);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.focus();
    // Select the name without its extension.
    const dot = initial.lastIndexOf(".");
    el.setSelectionRange(0, dot > 0 ? dot : initial.length);
  }, [initial]);
  const finish = (name: string) => {
    if (done.current) return;
    done.current = true;
    onDone(name);
  };
  return (
    <div className="tree-row naming" style={{ paddingLeft: 8 + depth * 14 }}>
      <span className="tree-twisty" />
      <Icon path={icon} size="sm" className="tree-icon" />
      <input
        ref={ref}
        className={`input tree-input ${value && !validName(value) ? "invalid" : ""}`}
        aria-label={label}
        value={value}
        spellCheck={false}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          e.stopPropagation();
          if (e.key === "Enter") finish(value);
          if (e.key === "Escape") finish("");
        }}
        onBlur={() => finish("")}
      />
    </div>
  );
}

/** Confirmation dialogs for git discard and incoming computer file drops. */
export function ShelfDialogs({
  discarding,
  onCancelDiscard,
  onConfirmDiscard,
  incoming,
  onCancelIncoming,
  onChooseHow,
  onBringIn,
}: {
  discarding: FileEntry | null;
  onCancelDiscard: () => void;
  onConfirmDiscard: (entry: FileEntry, action: GitAction) => void;
  incoming: Incoming | null;
  onCancelIncoming: () => void;
  onChooseHow: (inc: Incoming, move: boolean) => void;
  onBringIn: (inc: Incoming, onConflict: OnConflict) => void;
}) {
  const folderName = (into: string) => (into ? into.split("/").pop()! : t("desktop.files.drop.workspace"));
  return (
    <>
      {/* At the page's level: the shelf's glass blur would otherwise be the
          frame its dialogs centre in. */}
      {discarding &&
        createPortal(
          <Dialog
            title={t("desktop.files.git.discard_title", { name: discarding.name })}
            icon={mdiUndoVariant}
            onClose={onCancelDiscard}
            footer={
              <>
                <Button onClick={onCancelDiscard}>{t("desktop.cancel")}</Button>
                <Button
                  variant="filled"
                  danger
                  onClick={() => {
                    const e = discarding;
                    onCancelDiscard();
                    onConfirmDiscard(e, GitAction.DISCARD);
                  }}
                >
                  {t("desktop.files.git.discard_confirm")}
                </Button>
              </>
            }
          >
            <p>{t(isFolder(discarding) ? "desktop.files.git.discard_folder_body" : "desktop.files.git.discard_body", { path: discarding.path })}</p>
          </Dialog>,
          document.body,
        )}
      {incoming?.step === "how" &&
        createPortal(
          <Dialog
            title={t("desktop.files.drop.title", { folder: folderName(incoming.into) })}
            icon={mdiTrayArrowDown}
            onClose={onCancelIncoming}
            footer={
              <>
                <Button onClick={onCancelIncoming}>{t("desktop.cancel")}</Button>
                <Button onClick={() => onChooseHow(incoming, true)}>{t("desktop.files.drop.move")}</Button>
                <Button variant="filled" onClick={() => onChooseHow(incoming, false)}>
                  {t("desktop.files.drop.copy")}
                </Button>
              </>
            }
          >
            <p>{tn("desktop.files.drop.body", incoming.items.length, { names: someNames(incoming.items.map((i) => i.name)), folder: folderName(incoming.into) })}</p>
            <p className="muted t-body-sm">{t("desktop.files.drop.move_detail")}</p>
          </Dialog>,
          document.body,
        )}
      {incoming?.step === "conflict" &&
        createPortal(
          <Dialog
            title={tn("desktop.files.drop.conflict_title", incoming.conflicts.length, { names: someNames(incoming.conflicts), folder: folderName(incoming.into) })}
            icon={mdiFileReplaceOutline}
            onClose={onCancelIncoming}
            footer={
              <>
                <Button onClick={onCancelIncoming}>{t("desktop.cancel")}</Button>
                <Button onClick={() => onBringIn(incoming, "skip")}>{t("desktop.files.drop.skip")}</Button>
                <Button onClick={() => onBringIn(incoming, "keep")}>{t("desktop.files.drop.keep_both")}</Button>
                <Button variant="filled" danger onClick={() => onBringIn(incoming, "replace")}>
                  {t("desktop.files.drop.replace")}
                </Button>
              </>
            }
          >
            <p>{t("desktop.files.drop.conflict_body")}</p>
          </Dialog>,
          document.body,
        )}
    </>
  );
}
