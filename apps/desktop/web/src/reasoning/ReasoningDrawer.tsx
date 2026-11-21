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

import { useCallback, useEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent } from "react";
import { Background, Controls, MiniMap, ReactFlow, type Node, type ReactFlowInstance } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  mdiAlertCircleOutline,
  mdiArrowCollapseAll,
  mdiArrowExpandAll,
  mdiChevronLeft,
  mdiChevronRight,
  mdiCrosshairsGps,
  mdiGraphOutline,
  mdiLightbulbOnOutline,
  mdiMessageTextOutline,
  mdiWrenchOutline,
} from "@mdi/js";
import { Drawer, Icon, IconButton } from "@blitz/webui";
import { sessions } from "../api";
import { message } from "../errors";
import type { GetReasoningResponse } from "../gen/blitz/v1/session_pb";
import { t, tn } from "../i18n";
import { cost } from "../workflowRuns";
import { ReasoningDetail } from "./ReasoningDetail";
import { nodeTypes } from "./ReasoningCards";
import { isOpen, layoutReasoning, liveId, readingOrder, type CardData, type GraphView } from "./reasoningGraph";
import "./reasoning.css";

/** How often a running turn's graph is asked for again, at most. */
export const refreshMs = 700;

const viewKey = "blitz.reasoning.view";

function storedView(): Pick<GraphView, "thoughts" | "tools" | "words"> {
  try {
    const v = JSON.parse(localStorage.getItem(viewKey) ?? "{}") as Partial<GraphView>;
    return { thoughts: v.thoughts ?? true, tools: v.tools ?? true, words: v.words ?? true };
  } catch {
    return { thoughts: true, tools: true, words: true };
  }
}

/**
 * The Reasoning view (spec_066_reasoning): a session's turns as graphs of
 * their model calls, the reasoning each shared, the tools it called and
 * the sub-agents it asked, drawn as the turn runs. It fills the drawer
 * under the top menu, which stays usable.
 */
export function ReasoningDrawer({
  dir,
  sessionId,
  running,
  activity,
  live,
  onShowInChat,
  onClose,
}: {
  dir: string;
  sessionId: string;
  running: boolean;
  /** Changes as the conversation does (its entries), so a running turn's graph is asked for again. */
  activity: number;
  /** The live call's streamed reasoning while a turn runs ("" before any); undefined when idle. */
  live?: string;
  onShowInChat: (turn: number) => void;
  onClose: () => void;
}) {
  const [data, setData] = useState<GetReasoningResponse>();
  const [error, setError] = useState("");
  const [asked, setAsked] = useState<number>();
  const [follow, setFollow] = useState(true);
  const [kinds, setKinds] = useState(storedView);
  const [toggled, setToggled] = useState<ReadonlySet<string>>(new Set());
  const [selected, setSelected] = useState("");
  const flow = useRef<ReactFlowInstance<Node<CardData>> | null>(null);
  const shownTurn = data && data.turn >= 0 ? data.turn : undefined;

  // The graph, asked for again as the conversation changes; at most every refreshMs while it runs.
  const last = useRef(0);
  useEffect(() => {
    if (!sessionId) return;
    let gone = false;
    const wait = running ? Math.max(0, last.current + refreshMs - Date.now()) : 0;
    const timer = window.setTimeout(async () => {
      last.current = Date.now();
      try {
        const r = await sessions.getReasoning({ workspace: dir, sessionId, turn: follow ? undefined : asked });
        if (gone) return;
        setData(r);
        setError("");
      } catch (e) {
        if (!gone) setError(message(e));
      }
    }, wait);
    return () => {
      gone = true;
      window.clearTimeout(timer);
    };
  }, [dir, sessionId, running, activity, asked, follow]);

  // A new turn shown starts with its own folds and nothing selected.
  useEffect(() => {
    setToggled(new Set());
    setSelected("");
  }, [shownTurn, sessionId]);

  const following = follow && running;
  const view: GraphView = useMemo(() => ({ ...kinds, toggled, live: following && live !== undefined ? live : undefined }), [kinds, toggled, following, live]);
  const laid = useMemo(() => layoutReasoning(data?.nodes ?? [], view, (shownTurn ?? 0) + 1), [data, view, shownTurn]);
  const nodes = useMemo(() => laid.nodes.map((n) => (n.id === selected ? { ...n, selected: true } : n)), [laid, selected]);
  const order = useMemo(() => readingOrder(laid), [laid]);
  const node = data?.nodes.find((n) => n.id === selected);

  // Each turn's canvas fits itself when it's shown (its key); while
  // following, the view keeps the newest card in sight.
  useEffect(() => {
    const f = flow.current;
    if (!f || !following || !laid.nodes.length) return;
    const end = laid.nodes.reduce((a, b) => (b.position.y > a.position.y ? b : a));
    void f.setCenter(end.position.x + (end.width ?? 0) / 2, end.position.y + (end.height ?? 0) / 2, { zoom: Math.max(f.getZoom(), 0.75), duration: 240 });
  }, [laid, following]);

  const choose = (turn: number) => {
    setFollow(turn === (data?.turns.length ?? 1) - 1);
    setAsked(turn);
  };
  const setKind = (k: "thoughts" | "tools" | "words") =>
    setKinds((v) => {
      const next = { ...v, [k]: !v[k] };
      try {
        localStorage.setItem(viewKey, JSON.stringify(next));
      } catch {
        // a private window: the default next time
      }
      return next;
    });
  const foldAll = (open: boolean) => {
    const list = data?.nodes ?? [];
    setToggled(new Set(list.filter((n) => (n.kind === "call" || n.kind === "agent") && isOpen(n, new Set()) !== open).map((n) => n.id)));
  };
  // A sub-agent opened: the view moves to show it and its calls.
  const [focus, setFocus] = useState("");
  const toggle = useCallback(
    (id: string) => {
      const n = data?.nodes.find((x) => x.id === id);
      if (n?.kind === "agent" && !isOpen(n, toggled)) setFocus(id);
      setToggled((s) => (s.has(id) ? new Set([...s].filter((x) => x !== id)) : new Set([...s, id])));
    },
    [data, toggled],
  );
  useEffect(() => {
    if (!focus || !flow.current) return;
    const ids = new Set([focus]);
    for (const n of data?.nodes ?? []) if (ids.has(n.parent)) ids.add(n.id); // in order: children follow parents
    const shown = laid.nodes.filter((n) => ids.has(n.id)).map((n) => ({ id: n.id }));
    void flow.current.fitView({ nodes: shown, padding: 0.2, maxZoom: 1, duration: 300 });
    setFocus("");
  }, [focus, laid, data]);
  const center = (id: string) => {
    const n = laid.nodes.find((x) => x.id === id);
    if (n && flow.current) void flow.current.setCenter(n.position.x + (n.width ?? 0) / 2, n.position.y + (n.height ?? 0) / 2, { zoom: flow.current.getZoom(), duration: 200 });
  };
  const move = (step: number) => {
    if (!order.length) return;
    const i = order.indexOf(selected);
    const next = order[Math.min(order.length - 1, Math.max(0, i < 0 ? 0 : i + step))];
    if (next === liveId) return;
    setSelected(next);
    center(next);
  };

  const turns = data?.turns ?? [];
  const summary = shownTurn !== undefined ? turns[shownTurn] : undefined;
  const header = (
    <>
      <div className="rsn-turns row">
        <IconButton icon={mdiChevronLeft} small label={t("desktop.reasoning.prev")} disabled={!shownTurn} onClick={() => choose((shownTurn ?? 0) - 1)} />
        <select className="select rsn-turn-select" aria-label={t("desktop.reasoning.turn")} value={shownTurn ?? ""} disabled={!turns.length} onChange={(e) => choose(Number(e.target.value))}>
          {turns.map((x) => (
            <option key={x.index} value={x.index}>
              {t("desktop.reasoning.turn_option", { n: x.index + 1, prompt: x.prompt || "…" })}
            </option>
          ))}
        </select>
        <IconButton icon={mdiChevronRight} small label={t("desktop.reasoning.next")} disabled={shownTurn === undefined || shownTurn >= turns.length - 1} onClick={() => choose((shownTurn ?? 0) + 1)} />
      </div>
      <span className="rsn-head-line" aria-hidden="true" />
      <IconButton icon={mdiLightbulbOnOutline} small selected={kinds.thoughts} label={t("desktop.reasoning.show_thoughts")} onClick={() => setKind("thoughts")} />
      <IconButton icon={mdiWrenchOutline} small selected={kinds.tools} label={t("desktop.reasoning.show_tools")} onClick={() => setKind("tools")} />
      <IconButton icon={mdiMessageTextOutline} small selected={kinds.words} label={t("desktop.reasoning.show_words")} onClick={() => setKind("words")} />
      <span className="rsn-head-line" aria-hidden="true" />
      <IconButton icon={mdiArrowExpandAll} small label={t("desktop.reasoning.expand_all")} onClick={() => foldAll(true)} />
      <IconButton icon={mdiArrowCollapseAll} small label={t("desktop.reasoning.collapse_all")} onClick={() => foldAll(false)} />
      <IconButton icon={mdiCrosshairsGps} small selected={follow} label={t("desktop.reasoning.follow")} onClick={() => (follow ? setFollow(false) : choose(turns.length - 1))} disabled={!turns.length} />
    </>
  );

  const empty = !sessionId || (data && !turns.length);
  return (
    <Drawer title={t("desktop.reasoning")} icon={mdiGraphOutline} onClose={onClose} className="reasoning-drawer" headerActions={header}>
      {summary && (
        <div className="rsn-summary row">
          <span className="t-title-sm ellipsis spacer" title={summary.prompt}>
            {summary.prompt}
          </span>
          <span className="rsn-chip">{tn("desktop.reasoning.calls", summary.calls)}</span>
          {summary.thoughts > 0 && <span className="rsn-chip">{tn("desktop.reasoning.steps", summary.thoughts)}</span>}
          {summary.usage && summary.usage.calls > 0 && <span className="rsn-chip" title={t("desktop.reasoning.cost_estimated")}>{cost(summary.usage.costUsd, summary.usage.priced)}</span>}
        </div>
      )}
      {error && (
        <div className="card error row" role="alert">
          <Icon path={mdiAlertCircleOutline} />
          <span className="spacer">{error}</span>
        </div>
      )}
      {data?.cut && (
        <div className="card warn row">
          <Icon path={mdiAlertCircleOutline} />
          <span className="spacer">{t("desktop.reasoning.cut")}</span>
        </div>
      )}
      {empty ? (
        <div className="rsn-empty">
          <Icon path={mdiGraphOutline} size="lg" />
          <span className="t-title">{t("desktop.reasoning.empty_title")}</span>
          <span className="muted">{t("desktop.reasoning.empty")}</span>
        </div>
      ) : (
        <div className="rsn-body">
          <div
            className="rsn-canvas"
            tabIndex={0}
            aria-label={t("desktop.reasoning.canvas")}
            onKeyDown={(e) => {
              if (e.key === "ArrowDown" || e.key === "j") (e.preventDefault(), move(1));
              else if (e.key === "ArrowUp" || e.key === "k") (e.preventDefault(), move(-1));
              else if ((e.key === "Enter" || e.key === " ") && selected) {
                const n = data?.nodes.find((x) => x.id === selected);
                if (n && (n.kind === "call" || n.kind === "agent")) (e.preventDefault(), toggle(selected));
              }
            }}
          >
            <ReactFlow<Node<CardData>>
              key={`${sessionId}:${shownTurn}`}
              fitView
              fitViewOptions={{ padding: 0.12, maxZoom: 1, minZoom: 0.3 }}
              nodes={nodes}
              edges={laid.edges}
              nodeTypes={nodeTypes}
              nodesDraggable={false}
              nodesConnectable={false}
              edgesFocusable={false}
              onlyRenderVisibleElements
              minZoom={0.15}
              maxZoom={1.6}
              colorMode={document.documentElement.dataset.theme === "dark" ? "dark" : "light"}
              onInit={(f: ReactFlowInstance<Node<CardData>>) => (flow.current = f)}
              onNodeClick={(e: ReactMouseEvent, n: Node<CardData>) => {
                const fold = (e.target as HTMLElement).closest<HTMLElement>("[data-toggle]")?.dataset.toggle;
                if (fold) return toggle(fold);
                if (n.id !== liveId) setSelected(n.id);
              }}
              onPaneClick={() => setSelected("")}
              onMoveStart={(e: MouseEvent | TouchEvent | null) => e && following && setFollow(false)}
              proOptions={{ hideAttribution: true }}
            >
              <Background gap={24} />
              <MiniMap pannable zoomable position="top-right" className="rsn-minimap" style={{ width: 168, height: 112 }} nodeClassName={(n) => `rsn-mini is-${(n.data as CardData).kind}`} />
              <Controls showInteractive={false} position="bottom-left" />
            </ReactFlow>
          </div>
          <ReasoningDetail node={node} turn={shownTurn} onShowInChat={onShowInChat} onClose={() => setSelected("")} />
        </div>
      )}
    </Drawer>
  );
}
