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

import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { mdiChevronDown, mdiChevronRight, mdiConsole, mdiRefresh, mdiStopCircleOutline } from "@mdi/js";
import { message } from "./errors";
import type { WorkspaceProcess } from "./gen/blitz/v1/workspace_pb";
import { t, tn } from "./i18n";
import { displayName } from "./prefs";
import { byWorkspace, flaggedOf, flagLabel, outputLines, processDetail, processKey, processOutput, stopProcess, useProcesses } from "./processes";
import { useApp } from "./state";
import { Button, Dialog, Icon, IconButton, useSnackbar } from "./ui/controls";
import { Drawer } from "./ui/Drawer";

/** A row's output, once asked for: the text, or why it couldn't be read. */
type Output = { text?: string; error?: string; loading: boolean };

/**
 * The agents' background processes' output and details
 * (spec_processes_052), in the drawer from the status bar's Processes
 * menu: every open workspace's, under each workspace (dir's first), each
 * with its running time, usage and flags, Stop, and its last lines of
 * output when expanded. Stop flagged stops every flagged one, after
 * asking. Expand names the process (expandIn's, else dir's) whose output
 * shows as soon as it's listed: the one picked in the menu, or the tray's
 * Show in Blitz.
 */
export function ProcessesDrawer({ dir, expand, expandIn, onClose }: { dir: string; expand?: number; expandIn?: string; onClose: () => void }) {
  const { prefs } = useApp();
  const snack = useSnackbar();
  const { list, receivedAt } = useProcesses();
  const [outputs, setOutputs] = useState<Record<string, Output>>({});
  const [confirm, setConfirm] = useState(false);
  const [stopping, setStopping] = useState<Set<string>>(new Set());
  // Running times count up each second from what the service last sent.
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);

  const groups = byWorkspace(list, dir);
  const flagged = flaggedOf(list);
  const wsName = (d: string) => displayName(prefs.workspaces.find((w) => w.dir === d) ?? { dir: d });

  const load = async (p: WorkspaceProcess) => {
    const key = processKey(p);
    setOutputs((o) => ({ ...o, [key]: { ...o[key], loading: true } }));
    try {
      const text = await processOutput(p);
      setOutputs((o) => ({ ...o, [key]: { text, loading: false } }));
    } catch (e) {
      setOutputs((o) => ({ ...o, [key]: { error: message(e), loading: false } }));
    }
  };
  // The process asked for opens once, when the stream lists it.
  const expanded = useRef(false);
  useEffect(() => {
    if (expand === undefined || expanded.current) return;
    const p = list.find((x) => x.workspace === (expandIn || dir) && x.process?.id === expand);
    if (!p) return;
    expanded.current = true;
    void load(p);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once, when it's listed
  }, [list, dir, expand, expandIn]);
  const toggle = (p: WorkspaceProcess) => {
    const key = processKey(p);
    if (!outputs[key]) return void load(p);
    setOutputs((o) => {
      const rest = { ...o };
      delete rest[key];
      return rest;
    });
  };
  const stop = async (ps: WorkspaceProcess[]) => {
    const keys = ps.map(processKey);
    setStopping((s) => new Set([...s, ...keys]));
    const results = await Promise.allSettled(ps.map(stopProcess));
    const failed = results.find((r): r is PromiseRejectedResult => r.status === "rejected");
    if (failed) snack(message(failed.reason), { error: true });
    setStopping((s) => new Set([...s].filter((k) => !keys.includes(k))));
  };

  return (
    <Drawer
      title={t("desktop.processes.title")}
      icon={mdiConsole}
      className="processes-drawer"
      onClose={onClose}
    >
      {list.length === 0 ? (
        <p className="muted">{t("desktop.processes.empty_all")}</p>
      ) : (
        <div className="stack" style={{ gap: 8 }}>
          <div className="row">
            <span className="t-body-sm muted spacer">{flagged.length > 0 ? tn("desktop.processes.flagged", flagged.length) : t("desktop.processes.hint")}</span>
            {flagged.length > 0 && (
              <Button small danger icon={mdiStopCircleOutline} onClick={() => setConfirm(true)}>
                {t("desktop.processes.stop_flagged")}
              </Button>
            )}
          </div>
          {groups.map((g) => (
          <div key={g.workspace} className="stack" style={{ gap: 4 }}>
          <h3 className="t-title-sm" title={g.workspace}>
            {wsName(g.workspace)}
          </h3>
          <div className="list">
            {g.processes.map((p) => {
              const proc = p.process;
              if (!proc) return null;
              const key = processKey(p);
              const out = outputs[key];
              // Its flags have badges of their own: the line has the rest.
              const detail = processDetail(p, receivedAt, now, false);
              return (
                <div key={key} className="stack" style={{ gap: 0 }}>
                  <div className="list-item static-item">
                    <button type="button" className="row-open" aria-expanded={!!out} onClick={() => toggle(p)}>
                      <Icon path={out ? mdiChevronDown : mdiChevronRight} size="sm" />
                      <span className="lines spacer">
                        <span className="mono ellipsis" title={proc.command}>
                          {proc.command}
                        </span>
                        <small className="ellipsis">{detail}</small>
                      </span>
                    </button>
                    <span className="trailing">
                      {proc.flags.map((f) => (
                        <span key={f} className={`flag-chip flag-${f}`}>
                          {flagLabel(f, proc.idleMs)}
                        </span>
                      ))}
                      <Button small danger disabled={stopping.has(key)} onClick={() => void stop([p])}>
                        {t("desktop.processes.stop")}
                      </Button>
                    </span>
                  </div>
                  {out && (
                    <div className="stack processes-output" style={{ gap: 6 }}>
                      <div className="row">
                        <span className="t-label muted spacer">{t("desktop.processes.output", { lines: outputLines })}</span>
                        <IconButton icon={mdiRefresh} small label={t("desktop.processes.refresh")} disabled={out.loading} onClick={() => void load(p)} />
                      </div>
                      {out.error ? (
                        <p className="error-text t-body-sm">{out.error}</p>
                      ) : (
                        <OutputText text={out.text === undefined ? t("desktop.checking") : out.text || t("desktop.processes.no_output")} />
                      )}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
          </div>
          ))}
        </div>
      )}
      {confirm &&
        createPortal(
          <Dialog
            title={t("desktop.processes.stop_flagged_title")}
            icon={mdiStopCircleOutline}
            onClose={() => setConfirm(false)}
            footer={
              <>
                <Button onClick={() => setConfirm(false)}>{t("desktop.cancel")}</Button>
                <Button
                  variant="filled"
                  danger
                  onClick={() => {
                    setConfirm(false);
                    void stop(flagged);
                  }}
                >
                  {t("desktop.processes.stop")}
                </Button>
              </>
            }
          >
            <p>{tn("desktop.processes.stop_flagged_body", flagged.length)}</p>
          </Dialog>,
          // Over the window, not inside the drawer's column.
          document.body,
        )}
    </Drawer>
  );
}

// A process's last lines, scrolled to the newest.
function OutputText({ text }: { text: string }) {
  const ref = useRef<HTMLPreElement>(null);
  useLayoutEffect(() => {
    if (ref.current) ref.current.scrollTop = ref.current.scrollHeight;
  }, [text]);
  return (
    <pre ref={ref} className="eval-text">
      {text}
    </pre>
  );
}
