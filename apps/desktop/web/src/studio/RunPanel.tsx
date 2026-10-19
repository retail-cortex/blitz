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

import {
  mdiAlertCircleOutline,
  mdiCheckCircleOutline,
  mdiChevronDown,
  mdiChevronUp,
  mdiClose,
  mdiCloseCircleOutline,
  mdiContentCopy,
  mdiLoading,
  mdiPauseCircleOutline,
  mdiSkipNextCircleOutline,
  mdiStop,
  mdiStopCircleOutline,
} from "@mdi/js";
import { useEffect, useRef, useState, type ReactNode } from "react";
import type { WorkflowRun, WorkflowRunStep } from "../gen/blitz/v1/workspace_pb";
import { t, tn } from "../i18n";
import { Button, Icon, IconButton, Tabs, useSnackbar } from "../ui/controls";
import { modeOf } from "../options";
import { cost, ms, progress, tokens, took } from "../workflowRuns";

/** A run's or a step's status as an icon (and whether it spins). */
export function statusIcon(status: string): { path: string; spin?: boolean; className?: string } {
  switch (status) {
    case "running":
      return { path: mdiLoading, spin: true };
    case "done":
      return { path: mdiCheckCircleOutline, className: "ok-text" };
    case "waiting":
      return { path: mdiPauseCircleOutline, className: "warn-text" };
    case "skipped":
      return { path: mdiSkipNextCircleOutline, className: "muted" };
    case "stopped":
      return { path: mdiStopCircleOutline, className: "muted" };
    default:
      return { path: status === "failed" ? mdiCloseCircleOutline : mdiAlertCircleOutline, className: "error-text" };
  }
}

/** A time of day, to the second. */
const clock = (at: number) => (at ? new Date(at).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" }) : "");
/** A date and time, as a run's receipt is dated. */
export const dated = (at: number) => (at ? new Date(at).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" }) : "");

/** A run as text: its receipt's envelope and routing slip, to keep or paste. */
export function receiptText(run: WorkflowRun): string {
  const lines = [
    `/${run.workflow} · ${run.id}`,
    `${t(`desktop.flows.run_status.${run.status}`)} · ${dated(ms(run.started))} · ${took(ms(run.started), ms(run.ended) || Date.now())} · ${cost(run.usage?.costUsd, run.usage?.priced)}`,
    `${t("desktop.flows.receipt.origin")}: ${t(`desktop.flows.origin.${run.origin || "chat"}`)}${run.mode ? ` · ${t("desktop.flows.mode")}: ${modeOf(run.mode).label}` : ""}${run.input ? ` · ${t("desktop.flows.input")}: ${run.input}` : ""}`,
    "",
  ];
  for (const s of run.steps) {
    lines.push(
      [s.title || s.id, t(`desktop.flows.run_status.${s.status}`), clock(ms(s.started)), took(ms(s.started), ms(s.ended) || Date.now()), s.models.join(", "), s.usage?.calls ? `${tokens(s.usage.input)} in, ${tokens(s.usage.output)} out` : "", s.usage?.calls ? cost(s.usage.costUsd, s.usage.priced) : "", s.error]
        .filter(Boolean)
        .join(" · "),
    );
  }
  if (run.error) lines.push("", run.error);
  return lines.join("\n");
}

type Tab = "steps" | "events" | "stream";

/** Where the panel's height is kept. */
const heightKey = "blitz.flows.run_height";

/**
 * A workflow run's receipt under its canvas (spec_workflows_043 WF-19):
 * how it's going or how it ended, when, how long and what it's spent; its
 * steps' runs (when each started, how long it took, its tries, models,
 * tokens and cost), its events, and the text a step's turns streamed. It
 * shrinks to its header line to give the canvas the room.
 */
export function RunPanel({
  run,
  titleOf,
  step,
  onStep,
  onStop,
  onClose,
  children,
}: {
  run: WorkflowRun;
  /** A step's title on the canvas (the receipt's may be older). */
  titleOf: (id: string) => string;
  /** The step whose stream shows (the canvas's selection). */
  step: string;
  onStep: (id: string) => void;
  onStop: () => void;
  onClose: () => void;
  /** What the run waits on here (an approval, a person's form): shown however far it's folded. */
  children?: ReactNode;
}) {
  const snack = useSnackbar();
  const [tab, setTab] = useState<Tab>("steps");
  const [open, setOpen] = useState(true);
  // Its height as the user dragged its top edge (0: the default).
  const [height, setHeight] = useState(() => {
    try {
      return Number(localStorage.getItem(heightKey)) || 0;
    } catch {
      return 0;
    }
  });
  const drag = (e: React.PointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    const panel = e.currentTarget.parentElement!;
    const box = panel.getBoundingClientRect();
    const room = (panel.parentElement?.getBoundingClientRect().height ?? 600) - 160;
    let h = box.height;
    const move = (ev: PointerEvent) => {
      h = Math.round(Math.min(Math.max(box.bottom - ev.clientY, 140), room));
      panel.style.height = `${h}px`;
    };
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      document.body.classList.remove("resizing");
      setHeight(h);
      try {
        localStorage.setItem(heightKey, String(h));
      } catch {
        // the default next time
      }
    };
    document.body.classList.add("resizing");
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };
  const [, tick] = useState(0);
  const running = run.status === "running";
  useEffect(() => {
    if (!running) return;
    const id = setInterval(() => tick((n) => n + 1), 1000);
    return () => clearInterval(id);
  }, [running]);
  const now = Date.now();
  const icon = statusIcon(run.status);
  const { done, started } = progress(run);
  const summary = [
    running ? t("desktop.flows.run_progress", { done, started }) : t(`desktop.flows.run_status.${run.status}`),
    dated(ms(run.started)),
    took(ms(run.started), ms(run.ended) || now),
    cost(run.usage?.costUsd, run.usage?.priced),
    run.usage?.calls ? t("desktop.flows.run_tokens", { input: tokens(run.usage.input), output: tokens(run.usage.output) }) : "",
    run.mode ? modeOf(run.mode).label : "",
  ]
    .filter(Boolean)
    .join(" · ");
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(receiptText(run));
      snack(t("desktop.flows.receipt.copied"));
    } catch {
      snack(t("desktop.flows.receipt.copy_failed"), { error: true });
    }
  };
  const streamed = [...run.steps].reverse().find((s) => s.id === step) ?? [...run.steps].reverse().find((s) => s.stream) ?? run.steps[run.steps.length - 1];
  return (
    <section className={`run-panel ${open ? "is-open" : ""} ${run.status === "failed" ? "is-failed" : ""}`} style={open && height ? { height } : undefined} aria-label={t("desktop.flows.run_panel")}>
      {open && (
        <div
          className="run-panel-resize"
          role="separator"
          aria-orientation="horizontal"
          aria-label={t("desktop.flows.run_resize")}
          title={t("desktop.resize_hint")}
          onPointerDown={drag}
          onDoubleClick={() => {
            setHeight(0);
            try {
              localStorage.removeItem(heightKey);
            } catch {
              // nothing kept
            }
          }}
        />
      )}
      <div className="row run-panel-head">
        <Icon path={icon.path} spin={icon.spin} className={icon.className} size="sm" />
        <span className="t-label ellipsis spacer" title={summary}>
          {summary}
          {run.approvals > 0 && <span className="warn-text"> · {tn("desktop.flow_runs.approvals", run.approvals)}</span>}
        </span>
        {running && (
          <Button small icon={mdiStop} onClick={onStop}>
            {t("desktop.evals.stop")}
          </Button>
        )}
        <IconButton icon={mdiContentCopy} label={t("desktop.flows.receipt.copy")} small onClick={() => void copy()} />
        <IconButton icon={open ? mdiChevronDown : mdiChevronUp} label={t(open ? "desktop.flows.run_hide" : "desktop.flows.run_show")} small onClick={() => setOpen(!open)} />
        <IconButton icon={mdiClose} label={t("desktop.flows.run_close")} small onClick={onClose} />
      </div>
      {children && <div className="run-panel-attention stack">{children}</div>}
      {open && (
        <div className="run-panel-body">
          {run.error && <div className="t-body-sm error-text run-error">{run.error}</div>}
          <Tabs
            label={t("desktop.flows.run_panel")}
            value={tab}
            onChange={setTab}
            tabs={[
              { value: "steps", label: t("desktop.flows.run_tab.steps"), count: run.steps.length },
              { value: "events", label: t("desktop.flows.run_tab.events"), count: run.events.length },
              { value: "stream", label: t("desktop.flows.run_tab.stream") },
            ]}
          />
          <div className="run-panel-content">
            {tab === "steps" && <StepsTable run={run} titleOf={titleOf} now={now} selected={step} onSelect={(id) => (onStep(id), setTab("stream"))} />}
            {tab === "events" && <EventLog run={run} titleOf={titleOf} />}
            {tab === "stream" && <Stream run={run} step={streamed} titleOf={titleOf} onStep={onStep} />}
          </div>
        </div>
      )}
    </section>
  );
}

/** The steps' runs as a table: one row a run (a step run again has a row each). */
function StepsTable({ run, titleOf, now, selected, onSelect }: { run: WorkflowRun; titleOf: (id: string) => string; now: number; selected: string; onSelect: (id: string) => void }) {
  if (!run.steps.length) return <p className="t-body-sm muted">{t("desktop.flows.run_no_steps")}</p>;
  return (
    <table className="run-table">
      <thead>
        <tr>
          <th>{t("desktop.flows.receipt.step")}</th>
          <th>{t("desktop.flows.receipt.started")}</th>
          <th className="num">{t("desktop.flows.receipt.took")}</th>
          <th>{t("desktop.flows.receipt.model")}</th>
          <th className="num">{t("desktop.flows.receipt.tokens_in")}</th>
          <th className="num">{t("desktop.flows.receipt.tokens_out")}</th>
          <th className="num">{t("desktop.flows.receipt.cost")}</th>
        </tr>
      </thead>
      <tbody>
        {run.steps.map((s, i) => {
          const icon = statusIcon(s.status);
          const metered = (s.usage?.calls ?? 0) > 0;
          return (
            <tr key={`${s.id}-${i}`} className={s.id === selected ? "is-selected" : ""}>
              <td>
                <button type="button" className="run-step-name" title={s.error || t(`desktop.flows.run_status.${s.status}`)} onClick={() => onSelect(s.id)}>
                  <Icon path={icon.path} spin={icon.spin} className={icon.className} size="sm" />
                  <span className="ellipsis">{titleOf(s.id) || s.title || s.id}</span>
                  {s.attempts > 1 && <small className="muted">{tn("desktop.flows.receipt.tries", s.attempts)}</small>}
                </button>
                {s.error && <div className="t-body-sm error-text run-step-error">{s.error}</div>}
              </td>
              <td className="mono">{clock(ms(s.started))}</td>
              <td className="num mono">{s.status === "skipped" ? "—" : took(ms(s.started), ms(s.ended) || (s.status === "running" ? now : ms(s.started)))}</td>
              <td className="ellipsis" title={[s.agent, ...s.models].filter(Boolean).join(" · ")}>
                {s.models.join(", ") || (s.agent ? s.agent : "—")}
              </td>
              <td className="num mono">{metered ? tokens(s.usage?.input) : "—"}</td>
              <td className="num mono">{metered ? tokens(s.usage?.output) : "—"}</td>
              <td className="num mono">{metered ? cost(s.usage?.costUsd, s.usage?.priced) : "—"}</td>
            </tr>
          );
        })}
      </tbody>
      <tfoot>
        <tr>
          <td>{t("desktop.flows.receipt.total")}</td>
          <td />
          <td className="num mono">{took(ms(run.started), ms(run.ended) || now)}</td>
          <td className="ellipsis">{run.models.join(", ")}</td>
          <td className="num mono">{tokens(run.usage?.input)}</td>
          <td className="num mono">{tokens(run.usage?.output)}</td>
          <td className="num mono">{cost(run.usage?.costUsd, run.usage?.priced)}</td>
        </tr>
      </tfoot>
    </table>
  );
}

/** The run's events, oldest first, following the newest while it runs. */
function EventLog({ run, titleOf }: { run: WorkflowRun; titleOf: (id: string) => string }) {
  const end = useRef<HTMLDivElement>(null);
  useEffect(() => end.current?.scrollIntoView({ block: "nearest" }), [run.events.length]);
  if (!run.events.length) return <p className="t-body-sm muted">{t("desktop.flows.run_no_events")}</p>;
  return (
    <div className="run-events" role="log">
      {run.events.map((e, i) => (
        <div key={i} className={`run-event ${e.error ? "error-text" : ""}`}>
          <span className="mono muted">{clock(ms(e.at))}</span>
          <span className="run-event-kind">{t(`desktop.flows.event_kind.${e.kind}`)}</span>
          <span className="run-event-text">
            {e.step && <strong>{titleOf(e.step) || e.step}: </strong>}
            {e.kind === "step" || e.kind === "run" ? eventText(e.text) : e.text}
          </span>
        </div>
      ))}
      <div ref={end} />
    </div>
  );
}

/** A step or run event's text (its status, then why), in the user's words. */
export function eventText(text: string): string {
  const [status, ...rest] = text.split(": ");
  if (status === "held" && rest.length === 2) return t("desktop.flows.mode_held_event", { want: modeOf(rest[0]).label, mode: modeOf(rest[1]).label });
  const known = ["running", "waiting", "done", "failed", "skipped", "stopped", "started", "resumed"];
  if (!known.includes(status)) return text;
  return [t(`desktop.flows.run_status.${status}`), ...rest].join(": ");
}

/** What a step's turns streamed (or its output), the step chosen from the run's. */
function Stream({ run, step, titleOf, onStep }: { run: WorkflowRun; step: WorkflowRunStep | undefined; titleOf: (id: string) => string; onStep: (id: string) => void }) {
  const end = useRef<HTMLPreElement>(null);
  const text = step ? step.stream || step.output : "";
  useEffect(() => {
    const el = end.current;
    if (el && step?.status === "running") el.scrollTop = el.scrollHeight;
  }, [text, step?.status]);
  const ids = [...new Set(run.steps.map((s) => s.id))];
  return (
    <div className="stack run-stream" style={{ gap: 6 }}>
      <select className="select" aria-label={t("desktop.flows.receipt.step")} value={step?.id ?? ""} onChange={(e) => onStep(e.target.value)}>
        {ids.map((id) => (
          <option key={id} value={id}>
            {titleOf(id) || id}
          </option>
        ))}
      </select>
      <pre className="eval-text run-stream-text" ref={end}>
        {text || (step?.status === "running" ? t("desktop.flows.run_stream_waiting") : t("desktop.flows.run_stream_none"))}
      </pre>
    </div>
  );
}
