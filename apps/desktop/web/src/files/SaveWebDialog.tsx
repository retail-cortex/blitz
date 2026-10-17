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
import { mdiContentSaveOutline, mdiFilePdfBox, mdiLanguageHtml5, mdiLanguageMarkdownOutline } from "@mdi/js";
import { files } from "../api";
import { previewContent, previewPrintPDF } from "../desktop";
import { message } from "../errors";
import { filesTouched, openFile } from "../events";
import { FileKind } from "../gen/blitz/v1/file_pb";
import { t } from "../i18n";
import { Button, Dialog, Field, Segmented, useSnackbar } from "../ui/controls";
import { paperFor } from "./printPage";
import { pageHTML, saveName, savePath, type SaveFormat } from "./webSave";

// The folder a workspace's pages were last saved in, for the next.
const lastFolder = new Map<string, string>();
let lastFormat: SaveFormat = "md";

/**
 * Save to workspace (spec_electron_038 EL-106): the web page as Markdown,
 * HTML or PDF, in a folder of the workspace (made if it isn't there),
 * under a name of the user's; a file that's there is replaced only when
 * the user says.
 */
export function SaveWebDialog({ dir, id, url, title, onClose }: { dir: string; id: string; url: string; title: string; onClose: () => void }) {
  const [format, setFormat] = useState<SaveFormat>(lastFormat);
  const [folder, setFolder] = useState(lastFolder.get(dir) ?? "");
  const [name, setName] = useState(() => saveName(url, title));
  const [folders, setFolders] = useState<string[]>([]);
  const [error, setError] = useState("");
  // The file the user agreed to replace, and its version.
  const [replacing, setReplacing] = useState<{ path: string; version: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const snack = useSnackbar();

  // The workspace's folders, to choose from (typing a new one makes it).
  useEffect(() => {
    let live = true;
    files.listDir({ workspace: dir, path: "" }).then(
      (r) => live && setFolders(r.entries.filter((e) => e.kind === FileKind.FOLDER).map((e) => e.path)),
      () => {},
    );
    return () => {
      live = false;
    };
  }, [dir]);

  const target = savePath(folder, name, format);
  const save = async () => {
    if ("error" in target) return setError(t(target.error));
    const { path } = target;
    setBusy(true);
    setError("");
    try {
      const { versions } = await files.statFiles({ workspace: dir, paths: [path] });
      const version = versions[path] ?? "";
      if (version && replacing?.path !== path) {
        setReplacing({ path, version });
        setError(t("desktop.web.save.exists", { name: path }));
        return;
      }
      const parent = path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "";
      if (parent) await files.listDir({ workspace: dir, path: parent }).catch(() => files.createFolder({ workspace: dir, path: parent }));
      if (format === "pdf") {
        const data = await previewPrintPDF(id, paperFor(navigator.language));
        await files.writeBinaryFile({ workspace: dir, path, data, version });
      } else {
        const c = await previewContent(id);
        const text = format === "md" ? (await import("./webMarkdown")).pageMarkdown(c.html, c.url || url, c.title || title) : pageHTML(c.html, c.url || url);
        await files.writeFile({ workspace: dir, path, text, version });
      }
      lastFolder.set(dir, folder.trim());
      lastFormat = format;
      filesTouched({ dir });
      snack(t("desktop.web.save.saved", { name: path }), { action: { label: t("desktop.web.save.open"), run: () => openFile({ dir, path }) } });
      onClose();
    } catch (e) {
      setError(t("desktop.web.save.failed", { error: message(e) }));
    } finally {
      setBusy(false);
    }
  };

  const enter = (e: React.KeyboardEvent) => {
    if (e.key === "Enter") {
      e.preventDefault();
      void save();
    }
  };
  const change = (f: () => void) => {
    f();
    setError("");
    setReplacing(null);
  };
  const shown = "path" in target ? target.path : "";
  return (
    <Dialog
      title={t("desktop.web.save.title")}
      icon={mdiContentSaveOutline}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>{t("desktop.cancel")}</Button>
          <Button variant="filled" icon={mdiContentSaveOutline} disabled={busy || !name.trim()} onClick={() => void save()}>
            {replacing && replacing.path === shown ? t("desktop.web.save.replace") : t("desktop.web.save.save")}
          </Button>
        </>
      }
    >
      <Field label={t("desktop.web.save.format")} supporting={t(`desktop.web.save.format_${format}`)}>
        {() => (
          <Segmented<SaveFormat>
            label={t("desktop.web.save.format")}
            value={format}
            onChange={(f) => change(() => setFormat(f))}
            options={[
              { value: "md", label: t("desktop.web.save.md"), icon: mdiLanguageMarkdownOutline },
              { value: "html", label: t("desktop.web.save.html"), icon: mdiLanguageHtml5 },
              { value: "pdf", label: t("desktop.web.save.pdf"), icon: mdiFilePdfBox },
            ]}
          />
        )}
      </Field>
      <Field label={t("desktop.web.save.folder")} supporting={t("desktop.web.save.folder_hint")}>
        {(fid) => (
          <>
            <input id={fid} className="input mono" list={`${fid}-folders`} spellCheck={false} autoComplete="off" value={folder} placeholder={t("desktop.web.save.folder_placeholder")} onChange={(e) => change(() => setFolder(e.target.value))} onKeyDown={enter} />
            <datalist id={`${fid}-folders`}>
              {folders.map((f) => (
                <option key={f} value={f} />
              ))}
            </datalist>
          </>
        )}
      </Field>
      <Field label={t("desktop.web.save.name")} supporting={shown && t("desktop.web.save.goes", { path: shown })} error={error}>
        {(nid) => <input id={nid} className="input" autoFocus spellCheck={false} autoComplete="off" value={name} onChange={(e) => change(() => setName(e.target.value))} onKeyDown={enter} />}
      </Field>
    </Dialog>
  );
}
