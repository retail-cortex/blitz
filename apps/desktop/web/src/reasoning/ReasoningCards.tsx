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

import { memo } from "react";
import { Handle, Position, type Node, type NodeProps } from "@xyflow/react";
import {
  mdiAccountOutline,
  mdiAlertCircleOutline,
  mdiCheckCircleOutline,
  mdiChevronDown,
  mdiChevronUp,
  mdiCreationOutline,
  mdiEyeOffOutline,
  mdiLightbulbOnOutline,
  mdiMessageTextOutline,
  mdiProgressClock,
  mdiRobotOutline,
  mdiWrenchOutline,
} from "@mdi/js";
import { Icon } from "@blitz/webui";
import type { Usage } from "../gen/blitz/v1/turn_pb";
import { t, tn } from "../i18n";
import { cost, tokens } from "../workflowRuns";
import { argSummary, cardSize, liveTail, type CardData, type CardKind } from "./reasoningGraph";

/** Each kind's icon. */
export const kindIcons: Record<CardKind, string> = {
  turn: mdiAccountOutline,
  call: mdiCreationOutline,
  thought: mdiLightbulbOnOutline,
  tool: mdiWrenchOutline,
  agent: mdiRobotOutline,
  text: mdiMessageTextOutline,
  live: mdiCreationOutline,
};

/** A status's icon (ok, failed, running). */
export const statusIcons: Record<string, string> = { ok: mdiCheckCircleOutline, failed: mdiAlertCircleOutline, running: mdiProgressClock };

/** A duration in milliseconds, short: 820 ms, 4.2 s, 3m 10s. */
export function duration(ms: bigint | number): string {
  const v = Number(ms);
  if (v <= 0) return "";
  if (v < 1000) return `${v} ms`;
  const s = v / 1000;
  if (s < 60) return `${s < 10 ? s.toFixed(1) : Math.round(s)} s`;
  return `${Math.floor(s / 60)}m ${Math.round(s % 60)}s`;
}

/** A call's tokens in and out, with its reasoning's. */
export function tokenLine(u: Usage | undefined): string {
  if (!u || (!u.input && !u.output)) return "";
  const parts = [t("desktop.reasoning.tokens", { input: tokens(u.input), output: tokens(u.output) })];
  if (u.reasoning) parts.push(t("desktop.reasoning.reasoning_tokens", { n: tokens(u.reasoning) }));
  return parts.join(" · ");
}

/**
 * A card's preview of text: plain (no Markdown marks), going on from its
 * title when the text starts with it.
 */
export function preview(text: string, title: string): string {
  const plain = text.replace(/\*\*|__|`|^#+\s*/gm, "").trim();
  return plain.startsWith(title) ? plain.slice(title.length).replace(/^[\s:;,.\u2014-]+/, "") : plain;
}

/** A usage's estimated cost, or "" for none. */
export const costOf = (u: Usage | undefined) => (u && u.calls ? cost(u.costUsd, u.priced) : "");

const Handles = () => (
  <>
    <Handle type="target" position={Position.Top} id="t" isConnectable={false} />
    <Handle type="source" position={Position.Bottom} id="b" isConnectable={false} />
    <Handle type="source" position={Position.Left} id="l-out" isConnectable={false} />
    <Handle type="target" position={Position.Left} id="l-in" isConnectable={false} />
    <Handle type="source" position={Position.Right} id="r-out" isConnectable={false} />
    <Handle type="target" position={Position.Right} id="r-in" isConnectable={false} />
  </>
);

/** One card of the graph; open/fold is the card's own button (onToggle). */
export const ReasoningCard = memo(function ReasoningCard({ data, selected }: NodeProps<Node<CardData>>) {
  const { kind, node, inside, open } = data;
  const status = node?.status ?? "ok";
  const size = cardSize[kind];
  const meta: string[] = [];
  let title = node?.title ?? "";
  let body = "";
  switch (kind) {
    case "turn":
      meta.push(tn("desktop.reasoning.calls", node?.usage?.calls ?? 0), duration(node?.durationMs ?? 0), costOf(node?.usage));
      break;
    case "call":
      title = node?.model || t("desktop.reasoning.call");
      // In and out on the card; the reasoning's tokens are in its details.
      if (node?.usage && (node.usage.input || node.usage.output)) meta.push(t("desktop.reasoning.tokens", { input: tokens(node.usage.input), output: tokens(node.usage.output) }));
      meta.push(duration(node?.durationMs ?? 0), costOf(node?.usage));
      break;
    case "thought":
    case "text":
      body = preview(node?.text ?? "", title);
      break;
    case "tool":
      body = argSummary(node?.args);
      break;
    case "agent":
      meta.push(tn("desktop.reasoning.calls", node?.usage?.calls ?? 0), duration(node?.durationMs ?? 0), costOf(node?.usage));
      break;
    case "live":
      title = t("desktop.reasoning.thinking");
      body = liveTail(data.live ?? "") || t("desktop.reasoning.working");
      break;
  }
  const line = meta.filter(Boolean).join(" · ");
  const classes = ["rsn-card", `is-${kind}`, `is-${status}`, selected ? "selected" : "", node?.hidden ? "is-hidden" : ""].filter(Boolean).join(" ");
  const toggles = (kind === "call" && inside > 0) || (kind === "agent" && inside > 0);
  return (
    <div className={classes} style={{ width: size.w, height: size.h }} data-kind={kind}>
      <Handles />
      <div className="rsn-card-head">
        <span className="rsn-card-icon">
          <Icon path={kindIcons[kind]} size="sm" className={kind === "live" ? "pulse" : undefined} />
        </span>
        {kind === "turn" && <span className="rsn-card-kicker">{t("desktop.reasoning.turn_n", { n: data.turn ?? 1 })}</span>}
        {kind !== "turn" && <span className="rsn-card-title ellipsis">{title}</span>}
        {kind === "tool" && node?.durationMs ? <span className="rsn-card-took">{duration(node.durationMs)}</span> : null}
        {(kind === "tool" || kind === "agent" || kind === "turn") && status !== "ok" && (
          <span className={`rsn-status is-${status}`} title={t(`desktop.reasoning.status.${status}`)}>
            <Icon path={statusIcons[status] ?? mdiCheckCircleOutline} size="sm" className={status === "running" ? "pulse" : undefined} />
          </span>
        )}
        {node?.hidden && (
          <span className="rsn-status is-hidden" title={t("desktop.reasoning.hidden")}>
            <Icon path={mdiEyeOffOutline} size="sm" />
          </span>
        )}
        {toggles && (
          <button
            type="button"
            className="rsn-toggle nodrag"
            data-toggle={node?.id}
            aria-expanded={open}
            title={t(kind === "agent" ? (open ? "desktop.reasoning.fold_agent" : "desktop.reasoning.open_agent") : open ? "desktop.reasoning.fold_steps" : "desktop.reasoning.open_steps")}
          >
            <span>{kind === "agent" ? tn("desktop.reasoning.calls_short", inside) : tn("desktop.reasoning.steps", inside)}</span>
            <Icon path={open ? mdiChevronUp : mdiChevronDown} size="sm" />
          </button>
        )}
      </div>
      {kind === "turn" && <div className="rsn-card-prompt">{title}</div>}
      {body && <div className={`rsn-card-body ${kind === "tool" ? "mono" : ""}`}>{body}</div>}
      {line && <div className="rsn-card-meta ellipsis">{line}</div>}
    </div>
  );
});

/** The graph's one node type. */
export const nodeTypes = { card: ReasoningCard };
