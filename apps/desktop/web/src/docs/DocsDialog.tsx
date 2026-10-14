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

// The user docs in the app (spec_desktop_024 DSK-103): the docs of this
// release, from the page itself (docs/content.ts, its own chunk), with
// their navigation, search, and back and forward. A link to a page the app
// doesn't carry, and "Open on the web", go to the website.
import { useEffect, useMemo, useRef, useState } from "react";
import { mdiArrowLeft, mdiArrowRight, mdiBookOpenPageVariantOutline, mdiFormatListBulleted, mdiMagnify, mdiOpenInNew } from "@mdi/js";
import { openURL } from "../desktop";
import { t } from "../i18n";
import { DocsLinksProvider, Markdown, type DocsLinks } from "../Markdown";
import { Button, Dialog, Icon, IconButton } from "../ui/controls";
import { docLink, docPicture, searchDocs, siteURLOf, type DocNode, type DocPage } from "./index";
import { loadAppDocs, type AppDocs as Docs } from "./load";

/** Where the docs open: a page by its site path ("" the home page), at a heading. */
export interface DocsAt {
  page: string;
  anchor?: string;
}

function Tree({ node, at, open, depth = 0 }: { node: DocNode; at: string; open: (path: string) => void; depth?: number }) {
  return (
    <>
      {node.children.map((n) => (
        <div key={n.page.path}>
          <button className={`list-item docs-nav-item ${n.page.path === at ? "active" : ""}`} style={{ paddingLeft: 12 + depth * 14 }} onClick={() => open(n.page.path)}>
            {n.page.title}
          </button>
          {(at === n.page.path || at.startsWith(n.page.path + "/")) && n.children.length > 0 && <Tree node={n} at={at} open={open} depth={depth + 1} />}
        </div>
      ))}
    </>
  );
}

/** The docs viewer, open on a page. */
export function DocsDialog({ initial, onClose }: { initial: DocsAt; onClose: () => void }) {
  const [docs, setDocs] = useState<Docs | null>(null);
  const [history, setHistory] = useState<DocsAt[]>([initial]);
  const [index, setIndex] = useState(0);
  const [query, setQuery] = useState("");
  // In a narrow window the navigation folds behind Contents (app.css).
  const [navOpen, setNavOpen] = useState(false);
  const pane = useRef<HTMLDivElement>(null);
  useEffect(() => {
    void loadAppDocs().then(setDocs);
  }, []);
  const at = history[index];
  const page: DocPage | undefined = docs?.pages.get(at.page) ?? docs?.pages.get("");
  const go = (path: string, anchor?: string) => {
    setHistory((h) => [...h.slice(0, index + 1), { page: path, anchor }]);
    setIndex((i) => i + 1);
    setQuery("");
    setNavOpen(false);
  };
  const back = () => setIndex((i) => Math.max(0, i - 1));
  const forward = () => setIndex((i) => Math.min(history.length - 1, i + 1));
  // A new page starts at its top, or at the heading asked for.
  useEffect(() => {
    const el = pane.current;
    if (!el) return;
    const h = at.anchor ? el.querySelector(`[data-anchor="${CSS.escape(at.anchor)}"]`) : null;
    if (h) h.scrollIntoView({ block: "start" });
    else el.scrollTop = 0;
    // A table's cells carry their column's name, for the narrow layout,
    // where each row stacks (app.css).
    for (const table of el.querySelectorAll("table")) {
      const names = [...table.querySelectorAll("thead th")].map((th) => th.textContent?.trim() ?? "");
      for (const row of table.querySelectorAll("tbody tr")) [...row.children].forEach((td, i) => td.setAttribute("data-label", names[i] ?? ""));
    }
  }, [at, page]);
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey)) return;
      if (e.key === "[") (e.preventDefault(), back());
      else if (e.key === "]") (e.preventDefault(), forward());
    };
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  });
  const links = useMemo<DocsLinks | null>(
    () =>
      docs && page
        ? {
            link: (href) => docLink(page, href, new Set(docs.pages.keys())),
            picture: (src) => docPicture(page, src, docs.pictures),
            open: go,
          }
        : null,
    // go reads index; a new page or step makes new links
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [docs, page, index],
  );
  const hits = docs && query.trim() ? searchDocs([...docs.pages.values()], query) : null;
  const trail = page ? page.path.split("/").map((_, i, parts) => parts.slice(0, i + 1).join("/")) : [];
  return (
    <Dialog
      title={t("desktop.docs.title")}
      icon={mdiBookOpenPageVariantOutline}
      onClose={onClose}
      large
      headerActions={
        <>
          <IconButton icon={mdiArrowLeft} label={t("desktop.docs.back")} disabled={index === 0} onClick={back} />
          <IconButton icon={mdiArrowRight} label={t("desktop.docs.forward")} disabled={index === history.length - 1} onClick={forward} />
        </>
      }
      footer={
        <>
          <Button onClick={() => void openURL(siteURLOf(page?.path ?? "", at.anchor)).catch(() => {})}>
            <Icon path={mdiOpenInNew} size="sm" /> {t("desktop.docs.web")}
          </Button>
          <Button onClick={onClose}>{t("desktop.done")}</Button>
        </>
      }
    >
      <div className={`settings docs ${navOpen ? "nav-open" : ""}`}>
        <div className="settings-nav list docs-nav">
          <label className="docs-search">
            <Icon path={mdiMagnify} size="sm" />
            <input className="input" type="search" value={query} placeholder={t("desktop.docs.search")} aria-label={t("desktop.docs.search")} onChange={(e) => setQuery(e.target.value)} data-autofocus="" />
          </label>
          {docs && (
            <>
              <button className={`list-item docs-nav-item ${at.page === "" ? "active" : ""}`} onClick={() => go("")}>
                {docs.tree.page.title}
              </button>
              <Tree node={docs.tree} at={page?.path ?? ""} open={(p) => go(p)} />
            </>
          )}
        </div>
        <div className="settings-pane docs-pane" ref={pane}>
          <button className="link-button docs-contents" aria-expanded={navOpen} onClick={() => setNavOpen((o) => !o)}>
            <Icon path={mdiFormatListBulleted} size="sm" /> {t("desktop.docs.contents")}
          </button>
          {!docs ? (
            <p className="muted">{t("desktop.checking")}</p>
          ) : hits ? (
            hits.length === 0 ? (
              <p className="muted">{t("desktop.docs.none", { query })}</p>
            ) : (
              <div className="docs-hits">
                {hits.map((h) => (
                  <button key={h.page.path} className="list-item docs-hit" onClick={() => go(h.page.path)}>
                    <strong>{h.page.title}</strong>
                    <span className="muted">{h.snippet}</span>
                  </button>
                ))}
              </div>
            )
          ) : page && links ? (
            <>
              {trail.length > 1 && (
                <nav className="docs-trail muted" aria-label={t("desktop.docs.trail")}>
                  {trail.slice(0, -1).map((p) => (
                    <button key={p} className="link-button" onClick={() => go(p)}>
                      {docs.pages.get(p)?.title ?? p}
                    </button>
                  ))}
                </nav>
              )}
              <DocsLinksProvider value={links}>
                <Markdown text={page.body.trim().startsWith("# ") ? page.body : `# ${page.title}\n\n${page.body}`} />
              </DocsLinksProvider>
            </>
          ) : null}
        </div>
      </div>
    </Dialog>
  );
}
