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

// ```mermaid blocks drawn as diagrams (spec_files_029 FIL-55), in the chat
// and in Markdown previews. Mermaid loads the first time one shows. It
// runs in strict mode: labels are text, no scripts or click handlers, and
// its SVG passes through DOMPurify before it reaches the page.
import { useEffect, useRef, useState } from "react";
import { mdiCheck, mdiCodeTags, mdiContentCopy, mdiGraphOutline } from "@mdi/js";
import { CodeBlock } from "./CodeBlock";
import { t } from "./i18n";
import { useApp } from "./state";
import type { Theme } from "./theme";
import { diagramControls, panZoom } from "./panzoom";
import { IconButton } from "./ui/controls";

type Mermaid = (typeof import("mermaid"))["default"];

let loading: Promise<Mermaid> | null = null;
let configured = "";
let seq = 0;
// The block's language, as its head shows it.
const lang = "mermaid";

// Rendered diagrams by theme and source: a chat re-renders often.
const cache = new Map<string, string>();
const cacheMax = 64;

/** The CSS tokens the diagrams take their colors from. */
export const mermaidTokens = [
  "--md-surface-container-lowest",
  "--md-surface-container",
  "--md-surface-container-high",
  "--md-primary",
  "--md-primary-container",
  "--md-on-primary-container",
  "--md-secondary-container",
  "--md-tertiary-container",
  "--md-on-surface",
  "--md-on-surface-variant",
  "--md-outline",
  "--md-outline-variant",
  "--md-font",
] as const;

/**
 * Mermaid's base theme in the window's colors (token reads a CSS
 * variable's value; one that's empty leaves Mermaid's own).
 */
export function mermaidConfig(theme: Theme, token: (name: (typeof mermaidTokens)[number]) => string) {
  const v: Record<string, string> = {};
  const set = (key: string, name: (typeof mermaidTokens)[number]) => {
    const value = token(name).trim();
    if (value) v[key] = value;
  };
  set("background", "--md-surface-container-lowest");
  set("primaryColor", "--md-primary-container");
  set("primaryTextColor", "--md-on-primary-container");
  set("primaryBorderColor", "--md-primary");
  set("secondaryColor", "--md-secondary-container");
  set("tertiaryColor", "--md-tertiary-container");
  set("lineColor", "--md-on-surface-variant");
  set("textColor", "--md-on-surface");
  set("mainBkg", "--md-primary-container");
  set("nodeBorder", "--md-primary");
  set("clusterBkg", "--md-surface-container");
  set("clusterBorder", "--md-outline-variant");
  set("edgeLabelBackground", "--md-surface-container-high");
  set("noteBkgColor", "--md-surface-container-high");
  set("noteTextColor", "--md-on-surface");
  set("noteBorderColor", "--md-outline");
  set("actorBkg", "--md-primary-container");
  set("actorBorder", "--md-primary");
  set("actorTextColor", "--md-on-primary-container");
  set("signalColor", "--md-on-surface");
  set("signalTextColor", "--md-on-surface");
  set("fontFamily", "--md-font");
  return {
    startOnLoad: false,
    securityLevel: "strict" as const,
    theme: "base" as const,
    darkMode: theme === "dark",
    fontSize: 14,
    themeVariables: v,
  };
}

async function load(theme: Theme): Promise<Mermaid> {
  loading ??= import("mermaid").then((m) => m.default);
  const m = await loading;
  if (configured !== theme) {
    const style = getComputedStyle(document.documentElement);
    m.initialize(mermaidConfig(theme, (name) => style.getPropertyValue(name)));
    configured = theme;
  }
  return m;
}

/** Draws source as SVG in theme's colors; throws what's wrong with it. */
export async function drawDiagram(source: string, theme: Theme): Promise<string> {
  const key = `${theme}\0${source}`;
  const hit = cache.get(key);
  if (hit) return hit;
  const m = await load(theme);
  const id = `mermaid-${++seq}`;
  try {
    const { svg } = await m.render(id, source);
    if (cache.size >= cacheMax) cache.delete(cache.keys().next().value!);
    cache.set(key, svg);
    return svg;
  } finally {
    // A failed render leaves its scratch element behind.
    document.getElementById(`d${id}`)?.remove();
  }
}

/**
 * Draws source as SVG for paper, in Mermaid's light default theme
 * whatever the window's: Export as PDF from a dark window. The window's
 * next diagram sets its own theme again.
 */
export async function drawForPrint(source: string): Promise<string> {
  loading ??= import("mermaid").then((m) => m.default);
  const m = await loading;
  m.initialize({ startOnLoad: false, securityLevel: "strict", theme: "default", fontSize: 14 });
  configured = "";
  const id = `mermaid-${++seq}`;
  try {
    return (await m.render(id, source)).svg;
  } finally {
    document.getElementById(`d${id}`)?.remove();
  }
}

/** A diagram drawn as a picture: PNG bytes at twice its size, and its size in CSS pixels. */
export interface DiagramPicture {
  png: Uint8Array;
  width: number;
  height: number;
}

/**
 * Draws a diagram as a PNG, for documents that can't run Mermaid (Export
 * as Word): in Mermaid's light default theme, its labels SVG text (Word
 * and a canvas can't draw HTML labels), at twice its size for sharpness.
 */
export async function drawAsPicture(source: string): Promise<DiagramPicture> {
  loading ??= import("mermaid").then((m) => m.default);
  const m = await loading;
  m.initialize({ startOnLoad: false, securityLevel: "strict", theme: "default", fontSize: 14, htmlLabels: false, flowchart: { htmlLabels: false } });
  configured = "";
  const id = `mermaid-${++seq}`;
  let svg: string;
  try {
    svg = (await m.render(id, source)).svg;
  } finally {
    document.getElementById(`d${id}`)?.remove();
  }
  const { width, height } = svgSize(svg);
  const img = new Image();
  img.src = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(sized(svg, width, height))}`;
  await img.decode();
  const scale = 2;
  const canvas = document.createElement("canvas");
  canvas.width = Math.ceil(width * scale);
  canvas.height = Math.ceil(height * scale);
  const g = canvas.getContext("2d");
  if (!g) throw new Error("no canvas to draw the diagram on");
  g.fillStyle = "#fff";
  g.fillRect(0, 0, canvas.width, canvas.height);
  g.drawImage(img, 0, 0, canvas.width, canvas.height);
  const blob = await new Promise<Blob | null>((done) => canvas.toBlob(done, "image/png"));
  if (!blob) throw new Error("the diagram couldn't be drawn");
  return { png: new Uint8Array(await blob.arrayBuffer()), width: Math.round(width), height: Math.round(height) };
}

/** A Mermaid SVG's size in CSS pixels: its viewBox's (Mermaid sizes the element by CSS). */
export function svgSize(svg: string): { width: number; height: number } {
  const box = /viewBox="([\d.\s,-]+)"/.exec(svg)?.[1].split(/[\s,]+/).map(Number);
  if (box && box.length === 4 && box[2] > 0 && box[3] > 0) return { width: box[2], height: box[3] };
  const w = Number(/\bwidth="([\d.]+)/.exec(svg)?.[1]);
  const h = Number(/\bheight="([\d.]+)/.exec(svg)?.[1]);
  return { width: w > 0 ? w : 600, height: h > 0 ? h : 400 };
}

// The SVG with its size written on it: an <img> of an SVG sized only by
// CSS (max-width) draws at no size.
function sized(svg: string, width: number, height: number): string {
  return svg.replace(/<svg\b([^>]*)>/, (_m, attrs: string) => `<svg${attrs.replace(/\s(width|height)="[^"]*"/g, "").replace(/\sstyle="[^"]*"/, "")} width="${width}" height="${height}">`);
}

/**
 * The ```mermaid blocks in Markdown, by their source as the Word writer
 * reads it (its lines, without the fences or a last newline).
 */
export function mermaidSources(markdown: string): string[] {
  const out: string[] = [];
  const fence = /^( {0,3})(`{3,}|~{3,})[ \t]*mermaid\b[^\n]*\n([\s\S]*?)^ {0,3}\2[`~]*[ \t]*$/gim;
  for (const m of markdown.matchAll(fence)) {
    const indent = m[1].length;
    const body = m[3]
      .split("\n")
      .map((l) => l.replace(new RegExp(`^ {0,${indent}}`), ""))
      .join("\n")
      .replace(/\n+$/, "");
    out.push(body);
  }
  return out;
}

/** A ```mermaid block: the diagram, or its source while it doesn't parse (a reply still coming in). */
export function MermaidBlock({ source }: { source: string }) {
  const { theme } = useApp();
  const [svg, setSvg] = useState("");
  const [error, setError] = useState("");
  const [showSource, setShowSource] = useState(false);
  const [copied, setCopied] = useState(false);
  const host = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let live = true;
    // After the text settles (it streams in, or is being typed), and after
    // a theme change has reached the CSS variables.
    const timer = setTimeout(() => {
      drawDiagram(source, theme).then(
        (s) => live && (setSvg(s), setError("")),
        (e: unknown) => live && (setSvg(""), setError(e instanceof Error ? e.message : String(e))),
      );
    }, 200);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [source, theme]);

  useEffect(() => {
    if (host.current) host.current.innerHTML = svg; // DOMPurify-cleaned by Mermaid (strict)
  }, [svg, showSource]);

  // Pan and zoom (panzoom.ts), its controls in the head.
  const controls = useRef<HTMLSpanElement>(null);
  const drawn = !!svg && !showSource;
  const svgRef = useRef(svg);
  svgRef.current = svg;
  useEffect(() => {
    if (!drawn || !host.current || !controls.current) return;
    const pz = panZoom(host.current);
    const box = diagramControls(pz, () => svgRef.current);
    controls.current.replaceChildren(box);
    return () => {
      pz.destroy();
      box.remove();
    };
  }, [drawn]);

  if (!svg) {
    return (
      <>
        <CodeBlock lang={lang}>{source}</CodeBlock>
        {error && <p className="mermaid-error t-body-small muted">{t("desktop.mermaid.error", { error: error.split("\n")[0] })}</p>}
      </>
    );
  }
  return (
    <div className="code-block mermaid-block" data-source={source}>
      <div className="code-head">
        <span className="t-label muted">{lang}</span>
        <span className="code-head-actions">
          <span ref={controls} />
          <IconButton
            icon={showSource ? mdiGraphOutline : mdiCodeTags}
            label={showSource ? t("desktop.mermaid.show_diagram") : t("desktop.mermaid.source")}
            small
            onClick={() => setShowSource((s) => !s)}
          />
          <IconButton
            icon={copied ? mdiCheck : mdiContentCopy}
            label={copied ? t("desktop.code.copied") : t("desktop.code.copy")}
            small
            onClick={() =>
              navigator.clipboard?.writeText(source).then(() => {
                setCopied(true);
                setTimeout(() => setCopied(false), 1500);
              })
            }
          />
        </span>
      </div>
      {showSource ? (
        <pre>
          <code>{source}</code>
        </pre>
      ) : (
        <div className="mermaid-diagram" ref={host} role="img" aria-label={t("desktop.mermaid.diagram")} />
      )}
    </div>
  );
}
