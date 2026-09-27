import { useCallback, useEffect, useState } from "react";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import {
  mdiAlertCircleOutline,
  mdiCalendarClock,
  mdiCheckCircleOutline,
  mdiCloseCircleOutline,
  mdiPauseCircleOutline,
  mdiPlay,
  mdiProgressClock,
  mdiRefresh,
  mdiShieldCheckOutline,
} from "@mdi/js";
import { workers } from "./api";
import { message } from "./errors";
import { RunStatus, WorkerState, type Worker, type WorkerRun } from "./gen/blitz/v1/worker_pb";
import { applyEvent, failed, summarizeArgs, type Entry } from "./turns";
import { Button, Chip, Icon, IconButton } from "./ui/controls";

const stateLabel: Record<WorkerState, string> = {
  [WorkerState.UNSPECIFIED]: "?",
  [WorkerState.NEW]: "New",
  [WorkerState.ENABLED]: "Enabled",
  [WorkerState.DISABLED]: "Disabled",
  [WorkerState.CHANGED]: "Changed since enabled",
  [WorkerState.INVALID]: "Invalid",
};

const runLabel: Record<RunStatus, string> = {
  [RunStatus.UNSPECIFIED]: "?",
  [RunStatus.RUNNING]: "Running",
  [RunStatus.SUCCEEDED]: "Succeeded",
  [RunStatus.FAILED]: "Failed",
  [RunStatus.LIMITED]: "Stopped at a limit",
  [RunStatus.SKIPPED]: "Skipped",
};

const runIcon = (s: RunStatus) =>
  s === RunStatus.SUCCEEDED ? mdiCheckCircleOutline : s === RunStatus.RUNNING ? mdiProgressClock : s === RunStatus.SKIPPED ? mdiPauseCircleOutline : mdiCloseCircleOutline;

/** A workspace's workers: review and enable them, run them, see their runs. */
export function Workers({ dir }: { dir: string }) {
  const [list, setList] = useState<Worker[]>([]);
  const [selected, setSelected] = useState("");
  const [error, setError] = useState("");

  const refresh = useCallback(async () => {
    try {
      const ws = (await workers.listWorkers({ workspace: dir })).workers;
      setList(ws);
      setSelected((cur) => cur || ws[0]?.name || "");
    } catch (e) {
      setError(message(e));
    }
  }, [dir]);
  useEffect(() => {
    refresh();
  }, [refresh]);

  const worker = list.find((w) => w.name === selected);
  return (
    <div className="split">
      <aside className="split-side">
        <div className="row side-head">
          <span className="t-title-sm spacer">Workers</span>
          <IconButton icon={mdiRefresh} label="Refresh" small onClick={refresh} />
        </div>
        {list.length === 0 && (
          <p className="muted t-body-sm">
            No workers. Add <code>workers/&lt;name&gt;/WORKER.md</code> to this workspace to run a prompt on a schedule.
          </p>
        )}
        <div className="list">
          {list.map((w) => (
            <button key={w.name} className={`list-item ${w.name === selected ? "active" : ""}`} onClick={() => setSelected(w.name)}>
              <Icon path={mdiCalendarClock} />
              <span className="lines">
                <span className="ellipsis">{w.name}</span>
                <small className="ellipsis">
                  {stateLabel[w.state]} · {w.schedule}
                </small>
              </span>
            </button>
          ))}
        </div>
      </aside>
      <section className="split-main">
        {error && (
          <div className="card error row">
            <Icon path={mdiAlertCircleOutline} /> {error}
          </div>
        )}
        {worker ? <WorkerView dir={dir} worker={worker} onChange={refresh} key={worker.name + worker.hash} /> : list.length > 0 && <p className="muted">Choose a worker.</p>}
      </section>
    </div>
  );
}

function WorkerView({ dir, worker, onChange }: { dir: string; worker: Worker; onChange: () => void }) {
  const [runs, setRuns] = useState<WorkerRun[]>([]);
  const [live, setLive] = useState<Entry[] | null>(null);
  const [error, setError] = useState("");

  const refreshRuns = useCallback(async () => {
    setRuns((await workers.listWorkerRuns({ workspace: dir, name: worker.name, limit: 20 })).runs);
  }, [dir, worker.name]);
  useEffect(() => {
    refreshRuns().catch((e) => setError(message(e)));
  }, [refreshRuns]);

  const act = async (f: () => Promise<unknown>) => {
    setError("");
    try {
      await f();
      onChange();
    } catch (e) {
      setError(message(e));
    }
  };
  // The hash shown is the one enabled: an edit since then fails.
  const enable = () => act(() => workers.enableWorker({ workspace: dir, name: worker.name, hash: worker.hash }));
  const disable = () => act(() => workers.disableWorker({ workspace: dir, name: worker.name }));
  const runNow = () =>
    act(async () => {
      const started = (await workers.runWorker({ workspace: dir, name: worker.name })).run!;
      setLive([]);
      try {
        for await (const res of workers.watchWorkerRun({ runId: started.id })) {
          setLive((e) => applyEvent(e ?? [], res.event!));
        }
      } catch {
        // A run too quick to watch is simply recorded.
      }
      await refreshRuns();
    });

  const limits = worker.limits;
  const enabled = worker.state === WorkerState.ENABLED;
  return (
    <div className="worker">
      <div className="row">
        <h2 className="t-headline spacer">{worker.name}</h2>
        <Chip className="static" selected={enabled} tone={worker.state === WorkerState.INVALID ? "danger" : worker.state === WorkerState.CHANGED ? "warn" : undefined}>
          {stateLabel[worker.state]}
        </Chip>
      </div>
      {worker.description && <p className="muted">{worker.description}</p>}
      <div className="card outlined">
        <dl className="facts">
          <dt>Schedule</dt>
          <dd>
            {worker.schedule} · <code>{worker.cron}</code> ({worker.timezone})
            {worker.nextRun && enabled && <div className="t-body-sm muted">Next run {timestampDate(worker.nextRun).toLocaleString()}</div>}
          </dd>
          {worker.agent && (
            <>
              <dt>Agent</dt>
              <dd>{worker.agent}</dd>
            </>
          )}
          {worker.model && (
            <>
              <dt>Model</dt>
              <dd>{worker.model}</dd>
            </>
          )}
          <dt>May</dt>
          <dd className="row wrap">{worker.permissions.length ? worker.permissions.map((p) => <code key={p} className="chip static">{p}</code>) : "Read only"}</dd>
          <dt>Limits</dt>
          <dd>
            {limits?.maxTurns} model calls · ${limits?.maxCostUsd.toFixed(2)} · {limits?.timeout ? `${Number(limits.timeout.seconds) / 60} min` : "?"}
          </dd>
          <dt>Content</dt>
          <dd>
            <code className="t-body-sm muted">{worker.hash}</code>
          </dd>
        </dl>
      </div>
      {worker.problems.map((p) => (
        <div key={p} className="card error row">
          <Icon path={mdiAlertCircleOutline} size="sm" /> {p}
        </div>
      ))}
      <div className="row wrap">
        {!enabled && worker.state !== WorkerState.INVALID && (
          <Button variant="filled" icon={mdiShieldCheckOutline} onClick={enable} title={`Read ${worker.path} first: it runs unattended with these permissions`}>
            Enable as shown
          </Button>
        )}
        {enabled && (
          <Button variant="filled" icon={mdiPlay} onClick={runNow}>
            Run now
          </Button>
        )}
        {enabled && (
          <Button variant="outlined" onClick={disable}>
            Disable
          </Button>
        )}
      </div>
      {error && <p className="error-text">{error}</p>}
      {live && (
        <div className="card outlined live-run">
          <div className="t-title-sm">This run</div>
          {live.length === 0 && <p className="muted">Running…</p>}
          {live.map((e, i) =>
            e.kind === "tool" ? (
              <div key={i} className={`t-body-sm ${failed(e.result) ? "error-text" : "muted"}`}>
                <code>{e.name}</code> {summarizeArgs(e.args)} {e.result === undefined ? "…" : failed(e.result) ? "✗" : "✓"}
              </div>
            ) : "text" in e && e.kind !== "thought" ? (
              <div key={i} className="live-text">
                {e.text}
              </div>
            ) : null,
          )}
        </div>
      )}
      <h3 className="t-title">Runs</h3>
      {runs.length === 0 && <p className="muted t-body-sm">It hasn't run yet.</p>}
      <div className="runs">
        {runs.map((r) => (
          <div key={r.id} className="run">
            <Icon path={runIcon(r.status)} className={r.status === RunStatus.FAILED || r.status === RunStatus.LIMITED ? "error-text" : "muted"} />
            <div className="stack" style={{ gap: 2 }}>
              <span>
                {runLabel[r.status]} · {r.manual ? "manual" : "scheduled"}
                {r.usage?.priced ? ` · $${r.usage.costUsd.toFixed(4)}` : ""}
              </span>
              <span className="t-body-sm muted">
                {r.started && timestampDate(r.started).toLocaleString()} · session <code>{r.sessionId}</code>
              </span>
              {r.refusals.map((f, i) => (
                <span key={i} className="t-body-sm muted">
                  Refused: {f.detail}
                </span>
              ))}
              {r.error && <span className="t-body-sm error-text">{r.error.message}</span>}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
