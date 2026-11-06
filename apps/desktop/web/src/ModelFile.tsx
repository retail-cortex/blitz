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
import { mdiContentSaveOutline, mdiFileDocumentOutline, mdiFileMusicOutline, mdiFileVideoOutline, mdiImageOutline } from "@mdi/js";
import { files, workspaces } from "./api";
import { message } from "./errors";
import { t } from "./i18n";
import { Icon, IconButton, useSnackbar } from "@blitz/webui";

/** A file a model made, as the conversation shows it (spec_056 CA-71). */
export type ModelFileEntry = { kind: "file"; uri: string; name: string; mime: string; size: number };

/** Its icon by its type. */
function fileIcon(mime: string): string {
  if (mime.startsWith("image/")) return mdiImageOutline;
  if (mime.startsWith("audio/")) return mdiFileMusicOutline;
  if (mime.startsWith("video/")) return mdiFileVideoOutline;
  return mdiFileDocumentOutline;
}

/**
 * A file a model made: a picture shown as one, audio with a player,
 * anything else as a chip; each with Save, which writes it into the
 * workspace under its name. Its bytes come from the attachment store.
 */
export function ModelFile({ dir, entry }: { dir: string; entry: ModelFileEntry }) {
  const snack = useSnackbar();
  const [url, setUrl] = useState("");
  const [data, setData] = useState<Uint8Array | null>(null);
  const shown = entry.mime.startsWith("image/") || entry.mime.startsWith("audio/");

  useEffect(() => {
    let gone = false;
    let made = "";
    workspaces.readStoredFile({ workspace: dir, uri: entry.uri }).then(
      (r) => {
        if (gone) return;
        setData(r.data);
        if (shown) {
          made = URL.createObjectURL(new Blob([r.data as BlobPart], { type: r.mimeType || entry.mime }));
          setUrl(made);
        }
      },
      () => {},
    );
    return () => {
      gone = true;
      if (made) URL.revokeObjectURL(made);
    };
  }, [dir, entry.uri, entry.mime, shown]);

  const save = async () => {
    if (!data) return;
    try {
      await files.writeBinaryFile({ workspace: dir, path: entry.name, data });
      snack(t("desktop.model_file.saved", { path: entry.name }));
    } catch (e) {
      snack(message(e), { error: true });
    }
  };

  return (
    <div className="bubble-images" style={{ justifyContent: "flex-start", alignItems: "center" }} title={entry.name}>
      {url && entry.mime.startsWith("image/") ? (
        <img src={url} alt={entry.name} />
      ) : url && entry.mime.startsWith("audio/") ? (
        <audio controls src={url} aria-label={entry.name} />
      ) : (
        <span className="bubble-doc t-body-sm">
          <Icon path={fileIcon(entry.mime)} size="sm" />
          <span className="ellipsis">{entry.name}</span>
        </span>
      )}
      <IconButton icon={mdiContentSaveOutline} label={t("desktop.model_file.save", { name: entry.name })} small disabled={!data} onClick={() => void save()} />
    </div>
  );
}
