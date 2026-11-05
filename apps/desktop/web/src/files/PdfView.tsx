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

// A PDF drawn with pdf.js, the same in every web view (WebKitGTK has no
// viewer of its own for a PDF in the page). Loaded only when a PDF opens.
import { useEffect, useRef, useState } from "react";
import type { PDFDocumentProxy } from "pdfjs-dist";
import workerUrl from "pdfjs-dist/build/pdf.worker.min.mjs?url";
import { t } from "../i18n";
import { Button } from "@blitz/webui";

/** The pages drawn at first, and added by Show more pages. */
const pageStep = 100;

/** Draws each page of a PDF (its bytes) at the pane's width, the first pageStep, then more when asked. */
export function PdfView({ data, name }: { data: Uint8Array; name: string }) {
  const host = useRef<HTMLDivElement>(null);
  const [error, setError] = useState("");
  const [doc, setDoc] = useState<PDFDocumentProxy>();
  const [shown, setShown] = useState(pageStep);
  // The document, loaded once per file.
  useEffect(() => {
    let live = true;
    let destroy: (() => void) | undefined;
    setDoc(undefined);
    setShown(pageStep);
    (async () => {
      const pdfjs = await import("pdfjs-dist");
      // A tab closed while pdf.js loaded: nothing to open.
      if (!live) return;
      pdfjs.GlobalWorkerOptions.workerSrc = workerUrl;
      // pdf.js takes the buffer over: give it a copy.
      const task = pdfjs.getDocument({ data: data.slice() });
      destroy = () => void task.destroy();
      const d = await task.promise;
      if (live) setDoc(d);
    })().catch((e) => live && setError(String(e?.message ?? e)));
    return () => {
      live = false;
      destroy?.();
      if (host.current) host.current.replaceChildren();
    };
  }, [data, name]);
  // Its pages up to shown, after those already drawn.
  useEffect(() => {
    if (!doc) return;
    let live = true;
    (async () => {
      const width = (host.current?.clientWidth ?? 0) - 32;
      const ratio = window.devicePixelRatio || 1;
      for (let n = (host.current?.childElementCount ?? 0) + 1; n <= Math.min(doc.numPages, shown) && live; n++) {
        const page = await doc.getPage(n);
        if (!live || !host.current) return;
        const base = page.getViewport({ scale: 1 });
        const viewport = page.getViewport({ scale: Math.max(0.5, width / base.width) });
        const canvas = document.createElement("canvas");
        canvas.width = Math.floor(viewport.width * ratio);
        canvas.height = Math.floor(viewport.height * ratio);
        canvas.style.width = `${Math.floor(viewport.width)}px`;
        canvas.setAttribute("aria-label", t("desktop.files.pdf_page", { n, name }));
        host.current.appendChild(canvas);
        await page.render({ canvas, viewport, transform: ratio === 1 ? undefined : [ratio, 0, 0, ratio, 0, 0] }).promise;
      }
    })().catch((e) => live && setError(String(e?.message ?? e)));
    return () => {
      live = false;
    };
  }, [doc, shown, name]);
  if (error) return <p className="editor-note error-text">{t("desktop.files.pdf_failed", { error })}</p>;
  const pages = doc?.numPages ?? 0;
  return (
    <div className="preview preview-pdf">
      <div ref={host} className="pdf-pages" />
      {pages > shown && (
        <div className="row" style={{ gap: 8 }}>
          <span className="t-body-sm muted">{t("desktop.files.pdf_more", { shown, pages })}</span>
          <Button small onClick={() => setShown((n) => n + pageStep)}>
            {t("desktop.files.pdf_show_more")}
          </Button>
        </div>
      )}
    </div>
  );
}
