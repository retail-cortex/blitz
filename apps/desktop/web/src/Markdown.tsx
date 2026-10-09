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

// Markdown as React elements (react-markdown), never raw HTML: HTML in the
// text shows as text. Web links open in the system browser after a click,
// never in the window; relative links open the workspace file or folder
// they name (FIL-56), and #anchors scroll to a heading. Images aren't
// loaded (output can't make the app fetch anything); they show as links.
// ```mermaid blocks are drawn as diagrams (mermaid.tsx).
import { createContext, memo, useContext, type MouseEvent, type ReactNode } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { mdiImageOutline, mdiOpenInNew } from "@mdi/js";
import { files } from "./api";
import { CodeBlock } from "./CodeBlock";
import { openURL } from "./desktop";
import { message } from "./errors";
import { openFile, openFileAt, revealInTree } from "./events";
import { FileKind } from "./gen/blitz/v1/file_pb";
import { useFileLinks, useFileRef } from "./files/links";
import { resolveLink, slugger, type LinkTarget } from "./files/mdlinks";
import { t, useLanguage } from "./i18n";
import { MermaidBlock } from "./mermaid";
import { Icon, useSnackbar } from "./ui/controls";

/** The Markdown file being shown: its workspace and its path in it. */
export interface MarkdownDoc {
  dir: string;
  path: string;
}

const DocContext = createContext<MarkdownDoc | null>(null);

/** Says which file the Markdown inside is (a preview), so relative links start at its folder. */
export const MarkdownDocProvider = DocContext.Provider;

/** Scrolls the document el is in to the heading with id. */
export function scrollToHeading(el: Element | null, id: string): boolean {
  const h = el?.closest(".markdown")?.querySelector(`[data-anchor="${CSS.escape(id)}"]`);
  h?.scrollIntoView({ behavior: "smooth", block: "start" });
  return !!h;
}

// Opens a path in the workspace: a file in the editor (at a line, or at a
// heading of a Markdown file), a folder in the tree; one that isn't there
// says so.
async function openPath(dir: string, to: Extract<LinkTarget, { kind: "path" }>) {
  if (to.folder) return revealInTree({ dir, path: to.path });
  const slash = to.path.lastIndexOf("/");
  const parent = slash < 0 ? "" : to.path.slice(0, slash);
  const { entries } = await files.listDir({ workspace: dir, path: parent, showHidden: true });
  const entry = entries.find((e) => e.path === to.path);
  if (!entry) throw new Error(t("desktop.markdown.missing", { path: to.path }));
  if (entry.kind === FileKind.FOLDER) return revealInTree({ dir, path: to.path });
  if (to.anchor) return openFileAt({ dir, path: to.path }, to.anchor);
  openFile({ dir, path: to.path, line: to.line });
}

/**
 * Follows a link in a document (FIL-56): a web or mail link in the system
 * browser, #heading to it in el's document, a workspace path in the
 * editor or the tree; fail says why not.
 */
export function followLink(href: string, doc: { dir: string; path: string }, el: Element, fail: (message: string) => void) {
  follow(resolveLink(href, doc.path), href, doc, doc.dir, el, fail);
}

function follow(to: LinkTarget, href: string, doc: { path: string } | null, dir: string | undefined, el: Element, fail: (message: string) => void) {
  const failed = (err: unknown) => fail(message(err));
  if (to.kind === "url") openURL(to.href).catch(failed);
  else if (to.kind === "anchor") scrollToHeading(el, to.id);
  else if (to.kind === "outside") fail(t("desktop.markdown.outside", { href }));
  else if (doc && to.path === doc.path && !to.folder && !to.line) {
    if (to.anchor) scrollToHeading(el, to.anchor);
  } else if (dir !== undefined) openPath(dir, to).catch(failed);
}

function Link({ href, children }: { href?: string; children?: ReactNode }) {
  const snack = useSnackbar();
  const doc = useContext(DocContext);
  const links = useFileLinks();
  if (!href) return <>{children}</>;
  let to = resolveLink(href, doc ? doc.path : null);
  const dir = doc?.dir ?? links?.dir;
  // In the chat, only paths to files the workspace has are links to them.
  if (to.kind === "path" && !doc && !(links?.known.has(to.path) || (to.folder && to.path))) to = { kind: "url", href };
  if (to.kind === "anchor" && !doc) to = { kind: "url", href };
  const onClick = (e: MouseEvent<HTMLAnchorElement>) => {
    e.preventDefault();
    follow(to, href, doc, dir, e.currentTarget, (m) => snack(m, { error: true }));
  };
  return (
    <a href={href} title={href} onClick={onClick}>
      {children}
      {to.kind === "url" && <Icon path={mdiOpenInNew} size="sm" className="link-icon" />}
    </a>
  );
}

// Inline code that names a file in the workspace opens it in the editor.
function InlineCode({ text }: { text: string }) {
  const ref = useFileRef(text);
  if (!ref) return <code className="inline-code">{text}</code>;
  return (
    <code
      className="inline-code file-link"
      role="link"
      tabIndex={0}
      title={t("desktop.files.open_path", { path: ref.path })}
      onClick={(e) => {
        // Inside a link, the file wins.
        e.preventDefault();
        e.stopPropagation();
        ref.open();
      }}
      onKeyDown={(e) => e.key === "Enter" && ref.open()}
    >
      {text}
    </code>
  );
}

// The text in a heading (react-markdown's hast node), for its id.
type Hast = { type: string; value?: string; children?: Hast[]; properties?: Record<string, unknown>; position?: { start: { line: number } } };
function textOf(n: Hast | undefined): string {
  if (!n) return "";
  if (n.type === "text") return n.value ?? "";
  return (n.children ?? []).map(textOf).join("");
}

const components: Components = {
  a: ({ href, children }) => <Link href={href}>{children}</Link>,
  // A picture shows as a link to it; Export as PDF puts the picture in
  // (files/printPage.ts), by the marks on its wrapper.
  img: ({ src, alt }) =>
    typeof src === "string" && src ? (
      <span className="md-image" data-src={src} data-alt={alt ?? ""}>
        <Link href={src}>
          <Icon path={mdiImageOutline} size="sm" /> {alt || "image"}
        </Link>
      </span>
    ) : (
      <span>{alt}</span>
    ),
  pre: ({ children }) => <>{children}</>,
  code: ({ className, children }) => {
    const text = String(children ?? "");
    const lang = /language-([\w+-]+)/.exec(className ?? "")?.[1];
    if (lang === "mermaid") return <MermaidBlock source={text.replace(/\n$/, "")} />;
    // Fenced blocks have a language or span lines; the rest is inline code.
    if (lang || text.includes("\n")) return <CodeBlock lang={lang ?? ""}>{text.replace(/\n$/, "")}</CodeBlock>;
    return <InlineCode text={text} />;
  },
  table: ({ children }) => (
    <div className="table-wrap">
      <table>{children}</table>
    </div>
  ),
};

// A document's headings get ids (as GitHub's), for #anchor links, and a
// # beside them that scrolls there.
function headings(): Components {
  const next = slugger();
  const heading =
    (Tag: "h1" | "h2" | "h3" | "h4" | "h5" | "h6") =>
    ({ node, children }: { node?: unknown; children?: ReactNode }) => {
      const id = next(textOf(node as Hast));
      return (
        <Tag data-anchor={id} data-line={(node as Hast | undefined)?.position?.start.line}>
          {children}
          <a className="heading-anchor" href={`#${id}`} aria-label={t("desktop.markdown.anchor")} onClick={(e) => (e.preventDefault(), scrollToHeading(e.currentTarget, id))}>
            #
          </a>
        </Tag>
      );
    };
  return { h1: heading("h1"), h2: heading("h2"), h3: heading("h3"), h4: heading("h4"), h5: heading("h5"), h6: heading("h6") };
}

// A document's elements keep the source line each starts at (data-line),
// so a search hit can show where its line is in the preview.
function sourceLines() {
  const mark = (n: Hast) => {
    if (n.type === "element" && n.position) n.properties = { ...n.properties, dataLine: n.position.start.line };
    n.children?.forEach(mark);
  };
  return (tree: Hast) => mark(tree);
}
const docPlugins = [sourceLines];

/** Markdown rendered (see the file's header for what it never does). */
export const Markdown = memo(function Markdown({ text }: { text: string }) {
  useLanguage();
  const doc = useContext(DocContext);
  return (
    <div className="markdown">
      <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={doc ? docPlugins : undefined} components={doc ? { ...components, ...headings() } : components} skipHtml>
        {text}
      </ReactMarkdown>
    </div>
  );
});
