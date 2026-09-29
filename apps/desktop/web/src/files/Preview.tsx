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

import { lazy, Suspense, useEffect, useState } from "react";
import { mdiOpenInNew } from "@mdi/js";
import { files } from "../api";
import { inApp, openDocument } from "../desktop";
import { message } from "../errors";
import { t } from "../i18n";
import { Button, useSnackbar } from "../ui/controls";
import { Markdown } from "../Markdown";
import type { PreviewKind } from "./preview";

const PdfView = lazy(() => import("./PdfView").then((m) => ({ default: m.PdfView })));

/**
 * A file shown rather than edited (FIL-54): Markdown rendered from the
 * editor's current text, an SVG from it as an image, and images and PDFs
 * from the file's bytes (ReadPreview), PDFs drawn with pdf.js.
 */
export function Preview({ dir, path, kind, text }: { dir: string; path: string; kind: PreviewKind; text?: string }) {
  if (kind === "markdown") {
    return (
      <div className="preview preview-markdown">
        <Markdown text={text ?? ""} />
      </div>
    );
  }
  if (kind === "svg") return <SvgPreview text={text ?? ""} name={path} />;
  return <FilePreview dir={dir} path={path} kind={kind} />;
}

function SvgPreview({ text, name }: { text: string; name: string }) {
  // As an image, so nothing in it runs.
  const [url, setURL] = useState("");
  useEffect(() => {
    const u = URL.createObjectURL(new Blob([text], { type: "image/svg+xml" }));
    setURL(u);
    return () => URL.revokeObjectURL(u);
  }, [text]);
  if (!url) return null;
  return (
    <div className="preview preview-image">
      <img src={url} alt={name} />
    </div>
  );
}

function FilePreview({ dir, path, kind }: { dir: string; path: string; kind: PreviewKind }) {
  const [url, setURL] = useState("");
  const [bytes, setBytes] = useState<Uint8Array>();
  const [error, setError] = useState("");
  const [size, setSize] = useState("");
  const snack = useSnackbar();
  useEffect(() => {
    let live = true;
    let made = "";
    setURL("");
    setError("");
    files.readPreview({ workspace: dir, path }).then(
      (r) => {
        if (!live) return;
        if (kind === "pdf") setBytes(r.data);
        made = URL.createObjectURL(new Blob([r.data as Uint8Array<ArrayBuffer>], { type: r.mime }));
        setURL(made);
      },
      (e) => live && setError(message(e)),
    );
    return () => {
      live = false;
      if (made) URL.revokeObjectURL(made);
    };
  }, [dir, path, kind]);
  const external = inApp() && (
    <Button small icon={mdiOpenInNew} onClick={() => openDocument(`${dir}/${path}`).catch((e) => snack(message(e), { error: true }))}>
      {t("desktop.files.open_viewer")}
    </Button>
  );
  if (error) return <p className="editor-note error-text">{error}</p>;
  if (!url) return <p className="editor-note muted">{t("desktop.checking")}</p>;
  if (kind === "pdf")
    return (
      <>
        <Suspense fallback={<p className="editor-note muted">{t("desktop.checking")}</p>}>{bytes && <PdfView data={bytes} name={path} />}</Suspense>
        {external && <div className="preview-bar">{external}</div>}
      </>
    );
  return (
    <div className="preview preview-image">
      <img src={url} alt={path} onLoad={(e) => setSize(`${e.currentTarget.naturalWidth} × ${e.currentTarget.naturalHeight}`)} />
      <span className="row" style={{ gap: 8 }}>
        {size && <span className="preview-size t-body-sm muted">{size}</span>}
        {external}
      </span>
    </div>
  );
}
