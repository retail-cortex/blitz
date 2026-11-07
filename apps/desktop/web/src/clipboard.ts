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

// Copying to the clipboard: plain text, or rendered output as the text and
// formatting the user sees.

/** Puts text on the clipboard; rejects when there's no clipboard to use. */
export async function copyText(text: string): Promise<void> {
  if (!navigator.clipboard) throw new Error("no clipboard");
  await navigator.clipboard.writeText(text);
}

/**
 * Puts a picture on the clipboard (Pictures' Copy, spec_058 PIC-53). The
 * clipboard takes PNG, so a JPEG or WebP is redrawn as one first.
 */
export async function copyPicture(data: Uint8Array, mime: string): Promise<void> {
  if (typeof ClipboardItem === "undefined" || !navigator.clipboard?.write) throw new Error("no clipboard");
  let png = new Blob([data as BlobPart], { type: mime });
  if (mime !== "image/png") {
    const bitmap = await createImageBitmap(png);
    const canvas = document.createElement("canvas");
    canvas.width = bitmap.width;
    canvas.height = bitmap.height;
    canvas.getContext("2d")?.drawImage(bitmap, 0, 0);
    png = await new Promise<Blob>((ok, fail) => canvas.toBlob((b) => (b ? ok(b) : fail(new Error("no picture"))), "image/png"));
  }
  await navigator.clipboard.write([new ClipboardItem({ "image/png": png })]);
}

/**
 * Copies what el shows, without its controls (code blocks' language and
 * copy button): as HTML, so a rich editor keeps the formatting, and as the
 * text the user sees, for everywhere else. Where the clipboard only takes
 * text, it copies the text.
 */
export async function copyRendered(el: HTMLElement): Promise<void> {
  // innerText follows the layout, so the controls are hidden while it's read.
  el.classList.add("copying");
  const text = el.innerText.trim();
  el.classList.remove("copying");
  const clone = el.cloneNode(true) as HTMLElement;
  clone.querySelectorAll(".code-head, button, svg").forEach((n) => n.remove());
  if (typeof ClipboardItem !== "undefined" && navigator.clipboard?.write) {
    try {
      await navigator.clipboard.write([
        new ClipboardItem({
          "text/html": new Blob([clone.innerHTML], { type: "text/html" }),
          "text/plain": new Blob([text], { type: "text/plain" }),
        }),
      ]);
      return;
    } catch {
      // WebKitGTK may refuse HTML: fall back to the text.
    }
  }
  await copyText(text);
}
