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

import { useState } from "react";
import { mdiChevronDoubleLeft, mdiChevronDoubleRight, mdiClose, mdiContentCopy, mdiMessageArrowLeftOutline } from "@mdi/js";
import { Button, Icon, IconButton, ResizeHandle, useSnackbar } from "@blitz/webui";
import { copyText } from "../clipboard";
import { CodeBlock } from "../CodeBlock";
import type { ReasoningNode } from "../gen/blitz/v1/session_pb";
import { t } from "../i18n";
import { Markdown } from "../Markdown";
import { costOf, duration, kindIcons, tokenLine } from "./ReasoningCards";
import type { CardKind } from "./reasoningGraph";

const widthKey = "blitz.reasoning.side";
const openKey = "blitz.reasoning.side_open";
/** The detail panel's width by default, in pixels. */
export const sideDefault = 380;

const stored = (k: string) => {
  try {
    return localStorage.getItem(k) ?? "";
  } catch {
    return "";
  }
};
const store = (k: string, v: string) => {
  try {
    localStorage.setItem(k, v);
  } catch {
    // a private window: the default next time
  }
};

/** A node's facts as label and value pairs, those it has. */
export function facts(n: ReasoningNode): [string, string][] {
  const rows: [string, string][] = [];
  if (n.kind === "call") {
    if (n.model) rows.push([t("desktop.reasoning.model"), n.model]);
    if (n.provider) rows.push([t("desktop.reasoning.provider"), n.provider]);
  }
  if (n.author && n.kind !== "turn") rows.push([t("desktop.reasoning.agent"), n.author]);
  if (n.kind === "tool" || n.kind === "agent" || n.kind === "turn") rows.push([t("desktop.reasoning.status"), t(`desktop.reasoning.status.${n.status || "ok"}`)]);
  const tok = tokenLine(n.usage);
  if (tok) rows.push([t("desktop.reasoning.tokens_label"), tok]);
  const c = costOf(n.usage);
  if (c) rows.push([t("desktop.reasoning.cost"), c]);
  const d = duration(n.durationMs);
  if (d) rows.push([t("desktop.reasoning.took"), d]);
  return rows;
}

/** A step's text without the heading its title already shows (**Title** on its first line). */
export function withoutHeading(text: string, title: string): string {
  const first = text.trimStart().split("\n", 1)[0].trim();
  return first === `**${title}**` ? text.trimStart().slice(first.length).trimStart() : text;
}

/** What a node holds, in full: the copy button's text. */
export function fullText(n: ReasoningNode): string {
  const json = (v: unknown) => JSON.stringify(v, null, 2);
  if (n.kind === "tool" || n.kind === "agent") return [n.title, n.args && json(n.args), n.result && json(n.result)].filter(Boolean).join("\n\n");
  return n.text || n.title;
}

/** The selected card in full, beside the graph: folds to a rail, and resizes. */
export function ReasoningDetail({ node, turn, onShowInChat, onClose }: { node?: ReasoningNode; turn?: number; onShowInChat: (turn: number) => void; onClose: () => void }) {
  const snack = useSnackbar();
  const [open, setOpen] = useState(() => stored(openKey) !== "closed");
  const [width, setWidth] = useState(() => Number(stored(widthKey)) || sideDefault);
  const show = (v: boolean) => {
    setOpen(v);
    store(openKey, v ? "open" : "closed");
  };
  const kind = (node?.kind ?? "turn") as CardKind;
  const label = node ? t(`desktop.reasoning.kind.${kind}`) : t("desktop.reasoning.detail");
  if (!open)
    return (
      <aside className="flow-side rsn-side is-closed" aria-label={t("desktop.reasoning.detail")}>
        <button type="button" className="flow-side-rail" onClick={() => show(true)} title={t("desktop.reasoning.side_show")} aria-label={t("desktop.reasoning.side_show")}>
          <Icon path={mdiChevronDoubleLeft} size="sm" />
          <span>{node?.title || label}</span>
        </button>
      </aside>
    );
  return (
    <aside className="flow-side rsn-side" style={{ width }} aria-label={t("desktop.reasoning.detail")}>
      <ResizeHandle
        width={width}
        edge="left"
        min={280}
        max={() => window.innerWidth * 0.5}
        label={t("desktop.reasoning.side_resize")}
        onResize={(w) => {
          setWidth(w || sideDefault);
          store(widthKey, String(w));
        }}
      />
      <div className="row flow-side-head">
        <span className={`rsn-card-icon is-${kind}`}>
          <Icon path={kindIcons[kind]} size="sm" />
        </span>
        <span className="t-title-sm spacer ellipsis">{label}</span>
        {node && <IconButton icon={mdiContentCopy} small label={t("desktop.reasoning.copy")} onClick={() => void copyText(fullText(node)).then(() => snack(t("desktop.copied")))} />}
        {node && <IconButton icon={mdiClose} small label={t("desktop.reasoning.deselect")} onClick={onClose} />}
        <IconButton icon={mdiChevronDoubleRight} small label={t("desktop.reasoning.side_hide")} onClick={() => show(false)} />
      </div>
      <div className="flow-side-body stack rsn-detail">
        {!node ? (
          <p className="muted">{t("desktop.reasoning.pick")}</p>
        ) : (
          <>
            {node.kind !== "text" && node.kind !== "thought" && <h3 className="t-title-sm rsn-detail-title">{node.kind === "call" ? node.model || label : node.title}</h3>}
            {node.kind === "thought" && <h3 className="t-title-sm rsn-detail-title">{node.title}</h3>}
            {facts(node).length > 0 && (
              <dl className="rsn-facts">
                {facts(node).map(([k, v]) => (
                  <div key={k}>
                    <dt>{k}</dt>
                    <dd>{v}</dd>
                  </div>
                ))}
              </dl>
            )}
            {node.hidden && <div className="card row rsn-note">{t("desktop.reasoning.hidden")}</div>}
            {node.kind === "call" && !node.hidden && <p className="muted t-body-sm">{t("desktop.reasoning.call_hint")}</p>}
            {(node.kind === "thought" || node.kind === "text" || node.kind === "turn") && node.text && (
              <div className="rsn-text markdown">
                <Markdown text={node.kind === "thought" ? withoutHeading(node.text, node.title) : node.text} />
              </div>
            )}
            {node.args && Object.keys(node.args).length > 0 && (
              <section className="stack rsn-json">
                <span className="t-label">{t("desktop.reasoning.args")}</span>
                <CodeBlock lang="json">{JSON.stringify(node.args, null, 2)}</CodeBlock>
              </section>
            )}
            {node.result && (
              <section className="stack rsn-json">
                <span className="t-label">{t("desktop.reasoning.result")}</span>
                <CodeBlock lang="json">{JSON.stringify(node.result, null, 2)}</CodeBlock>
              </section>
            )}
          </>
        )}
        {turn !== undefined && (
          <Button small variant="tonal" icon={mdiMessageArrowLeftOutline} onClick={() => onShowInChat(turn)}>
            {t("desktop.reasoning.show_in_chat")}
          </Button>
        )}
      </div>
    </aside>
  );
}
