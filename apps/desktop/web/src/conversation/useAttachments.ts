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

import { useCallback, useState } from "react";
import { workspaces } from "../api";
import { acceptedAs, describeImage, isDocument, rejectReason, type Accepted, type Attachment, type WebBlock } from "../attachments";
import { message } from "../errors";
import type { AddWebDetail } from "../events";
import type { GetSettingsResponse } from "../gen/blitz/v1/workspace_pb";
import { t } from "../i18n";

/** Manages file/image uploads, workspace image references, web blocks, and database chips for the composer. */
export function useAttachments({
  dir,
  settings,
  snack,
}: {
  dir: string;
  settings?: GetSettingsResponse;
  snack: (text: string, opts?: { error?: boolean }) => void;
}) {
  const [attachments, setAttachments] = useState<Attachment[]>([]);

  // Images for the next prompt: each uploads as soon as it's added.
  const imagesOn = settings?.imagesEnabled ?? true;
  // What the active agent's model takes (GetSettings accepted_media).
  const accepted: Accepted[] = settings?.acceptedMedia ?? [];

  const addFiles = useCallback(
    (files: File[]) => {
      if (!imagesOn) {
        snack(t("desktop.images.off"), { error: true });
        return;
      }
      for (const f of files) {
        const why = rejectReason(f, accepted, settings?.model ?? "");
        if (why) {
          snack(why, { error: true });
          continue;
        }
        const doc = isDocument(f);
        const kind = acceptedAs(accepted, f)?.kind ?? "image";
        const a: Attachment = { key: `${Date.now()}-${Math.random()}`, name: f.name || t("desktop.attach.pasted"), url: kind === "image" ? URL.createObjectURL(f) : "", document: doc, kind };
        setAttachments((list) => [...list, a]);
        (async () => {
          try {
            const res = await workspaces.addImage({ workspace: dir, name: a.name, data: new Uint8Array(await f.arrayBuffer()) });
            const img = res.image!;
            setAttachments((list) => list.map((x) => (x.key === a.key ? { ...x, id: img.id, detail: describeImage(img) } : x)));
          } catch (e) {
            setAttachments((list) => list.map((x) => (x.key === a.key ? { ...x, error: message(e) } : x)));
          }
        })();
      }
    },
    [dir, imagesOn, snack, accepted, settings?.model],
  );

  // An image or PDF in the workspace (an @mention of one, or Add to context).
  const addImagePath = useCallback(
    (path: string) => {
      if (!imagesOn) return;
      const name = path.slice(path.lastIndexOf("/") + 1);
      const a: Attachment = { key: `${Date.now()}-${Math.random()}`, name, url: "", document: /\.pdf$/i.test(path), kind: acceptedAs(accepted, { name, type: "" })?.kind };
      setAttachments((list) => [...list, a]);
      workspaces.loadImage({ workspace: dir, path }).then(
        (res) => setAttachments((list) => list.map((x) => (x.key === a.key ? { ...x, id: res.image!.id, detail: describeImage(res.image!) } : x))),
        (e) => setAttachments((list) => list.map((x) => (x.key === a.key ? { ...x, error: message(e) } : x))),
      );
    },
    [dir, imagesOn, accepted],
  );

  const removeAttachment = (key: string) =>
    setAttachments((list) => {
      const gone = list.find((a) => a.key === key);
      if (gone) URL.revokeObjectURL(gone.url);
      return list.filter((a) => a.key !== key);
    });

  // A database, or a query's result, from the chat box's database menu: a
  // chip, read when it was added (spec_059_data DAT-43).
  const addData = useCallback((b: WebBlock) => {
    const detail = b.kind === "database" ? t("desktop.data.chip_database") : t("desktop.data.chip_read", { time: new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) });
    setAttachments((list) => (b.kind === "database" && list.some((a) => a.web?.kind === "database" && a.web.connection === b.connection) ? list : [...list, { key: `${Date.now()}-${Math.random()}`, name: b.title, url: "", web: b, detail }]));
  }, []);

  // A web page in the editor adds its content (a chip of its own, sent as
  // the turn's context) or a screenshot (an image, as a pasted one).
  const addWebDetail = useCallback(
    (d: AddWebDetail) => {
      if (d.image) addFiles([new File([d.image.data as BlobPart], d.image.name, { type: "image/png" })]);
      const b = d.block;
      if (!b) return;
      const name = b.title || b.url;
      setAttachments((list) => [...list, { key: `${Date.now()}-${Math.random()}`, name, url: "", web: b, detail: t(`desktop.web.added.${b.kind}`, { url: b.url }) }]);
    },
    [addFiles],
  );

  const clearAttachments = () => {
    const n = attachments.length;
    attachments.forEach((a) => URL.revokeObjectURL(a.url));
    setAttachments([]);
    return n;
  };

  return {
    attachments,
    setAttachments,
    imagesOn,
    accepted,
    addFiles,
    addImagePath,
    removeAttachment,
    addData,
    addWebDetail,
    clearAttachments,
  };
}
