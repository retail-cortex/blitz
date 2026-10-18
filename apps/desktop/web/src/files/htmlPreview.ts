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

// An HTML file's preview (FIL-54): the page as written, with what it
// takes from the workspace by relative path (its stylesheets, scripts,
// images and media) put inline, since it's shown from text in a sandboxed
// frame that can reach neither the app nor the files.

/** Reads a workspace file: its text (stylesheets, scripts) or a data: URL of its bytes (images, media). */
export interface HtmlLoaders {
  text: (path: string) => Promise<string>;
  dataURL: (path: string) => Promise<string>;
}

/** Whether ref names a file beside the page (not a URL, a data: URI, an anchor or an absolute path). */
export function isRelative(ref: string | null | undefined): ref is string {
  const r = (ref ?? "").trim();
  return r !== "" && !/^([a-z][a-z0-9+.-]*:|\/\/|\/|#)/i.test(r);
}

/** ref, relative to the page at page (a workspace path), as a workspace path ("" when it leaves the workspace). */
export function resolveRef(page: string, ref: string): string {
  const parts = page.split("/").slice(0, -1);
  for (const p of ref.split(/[?#]/)[0].split("/")) {
    if (p === "" || p === ".") continue;
    if (p === "..") {
      if (!parts.length) return "";
      parts.pop();
    } else parts.push(decodeURIComponent(p));
  }
  return parts.join("/");
}

/** The page at path, with its relative stylesheets and scripts inlined and its images and media as data: URLs; what can't be read stays as it was. */
export async function inlineHtml(html: string, path: string, load: HtmlLoaders): Promise<string> {
  const doc = new DOMParser().parseFromString(html, "text/html");
  const jobs: Promise<void>[] = [];
  const at = (ref: string) => resolveRef(path, ref);
  for (const link of Array.from(doc.querySelectorAll("link[href]"))) {
    const href = link.getAttribute("href");
    const rel = (link.getAttribute("rel") ?? "").toLowerCase().split(/\s+/);
    if (!isRelative(href) || !at(href)) continue;
    if (rel.includes("stylesheet")) {
      jobs.push(
        load.text(at(href)).then(
          (css) => {
            const style = doc.createElement("style");
            style.textContent = css;
            link.replaceWith(style);
          },
          () => {},
        ),
      );
    } else if (rel.includes("icon")) {
      jobs.push(load.dataURL(at(href)).then((u) => link.setAttribute("href", u), () => {}));
    }
  }
  for (const script of Array.from(doc.querySelectorAll("script[src]"))) {
    const src = script.getAttribute("src");
    if (!isRelative(src) || !at(src)) continue;
    jobs.push(
      load.text(at(src)).then(
        (js) => {
          script.removeAttribute("src");
          script.textContent = js;
        },
        () => {},
      ),
    );
  }
  for (const [selector, attr] of [["img[src], source[src], video[src], audio[src], input[src], embed[src]", "src"], ["video[poster]", "poster"]] as const) {
    for (const el of Array.from(doc.querySelectorAll(selector))) {
      const ref = el.getAttribute(attr);
      if (!isRelative(ref) || !at(ref)) continue;
      jobs.push(load.dataURL(at(ref)).then((u) => el.setAttribute(attr, u), () => {}));
    }
  }
  await Promise.all(jobs);
  const doctype = /^\s*<!doctype[^>]*>/i.exec(html)?.[0] ?? "";
  return `${doctype}${doc.documentElement.outerHTML}`;
}
