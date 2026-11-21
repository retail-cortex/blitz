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

import { useCallback, useEffect, useRef } from "react";
import { sessions } from "../api";
import { readyIds } from "../attachments";
import { refreshRuns } from "../backgroundRuns";
import { usageLine, type TurnOptions } from "./executeSlashCommand";
import { filesTouched } from "../events";
import type { SessionInfo } from "../gen/blitz/v1/session_pb";
import type { Task, Usage } from "../gen/blitz/v1/turn_pb";
import { t } from "../i18n";
import type { NotifyKind } from "../notify";
import { applyEvent, assignPromptIndices, tasksOf, type Entry } from "../turns";
import { addWait, type Pending } from "../waits";

/** Executes conversation turns and watches background runs. */
export function useConversationRun({
  dir,
  name,
  session,
  setRunning,
  setEntries,
  setTasks,
  setWaits,
  setTurnUsage,
  setTotal,
  setError,
  refreshList,
  refreshTotal,
  fail,
  onSettingsChanged,
  tell,
  snack,
  stick,
  registerStop,
}: {
  dir: string;
  name: string;
  session?: SessionInfo;
  running: boolean;
  setRunning: React.Dispatch<React.SetStateAction<boolean>>;
  setEntries: React.Dispatch<React.SetStateAction<Entry[]>>;
  setTasks: React.Dispatch<React.SetStateAction<Task[]>>;
  setWaits: React.Dispatch<React.SetStateAction<Pending[]>>;
  setTurnUsage: (u: string) => void;
  setTotal: (u: Usage | undefined) => void;
  setError: (e: string) => void;
  refreshList: () => Promise<void>;
  refreshTotal: () => Promise<void>;
  fail: (e: unknown) => void;
  onSettingsChanged: () => void;
  tell: (kind: NotifyKind, title: string, body: string) => void;
  snack: (msg: string) => void;
  stick: React.MutableRefObject<boolean>;
  registerStop: (dir: string, stop: (() => Promise<void>) | null) => void;
}) {
  const abort = useRef<AbortController | null>(null);
  const following = useRef("");
  const late = useRef<string[]>([]);
  const latestRunning = useRef(false);
  const finished = useRef<Promise<void>>(Promise.resolve());
  const started = useRef(0);

  const run = useCallback(
    async (text: string, opts: TurnOptions = {}) => {
      if (!session) return;
      const images = opts.images ?? [];
      const context = opts.context ?? [];
      if (!opts.accepted && !opts.follow)
        setEntries((e) => [
          ...e,
          {
            kind: "user",
            text: opts.shown ?? (opts.plan ? `/plan ${text}` : text),
            sub: opts.aside ? "aside" : undefined,
            images: images.length || context.length ? [...images.map((a) => ({ url: a.url, name: a.name })), ...context.map((b) => ({ url: "", name: b.title || b.url, web: b.kind }))] : undefined,
          },
        ]);
      setRunning(true);
      latestRunning.current = true;
      started.current = Date.now();
      setError("");
      setTurnUsage("");
      stick.current = true;
      const ctl = new AbortController();
      abort.current = ctl;
      following.current = opts.follow ?? "";
      let done!: () => void;
      finished.current = new Promise((r) => (done = r));
      let leftover: string[] = [];
      try {
        const stream = opts.follow
          ? sessions.watchBackground({ id: opts.follow, follow: true }, { signal: ctl.signal })
          : sessions.runTurn(
              {
                workspace: dir,
                sessionId: session.id,
                turn: {
                  text,
                  accepted: !!opts.accepted,
                  plan: !!opts.plan,
                  imageIds: readyIds(images),
                  aside: !!opts.aside,
                  readOnly: opts.readOnly ?? "",
                  prompt: opts.prompt ?? "",
                  fetchGrants: opts.fetchGrants ?? [],
                  command: !!opts.command,
                  context,
                },
              },
              { signal: ctl.signal },
            );
        for await (const res of stream) {
          const ev = res.event;
          if (!ev) continue;
          const taskList = tasksOf(ev);
          if (taskList) setTasks(taskList);
          else if (ev.kind.case === "approvalRequest") {
            const req = ev.kind.value;
            setWaits((w) => addWait(w, { kind: "approval", req }));
          } else if (ev.kind.case === "question") {
            const q = ev.kind.value;
            setWaits((w) => addWait(w, { kind: "question", q }));
          } else if (ev.kind.case === "notice" && ev.kind.value.transient) snack(ev.kind.value.text);
          else setEntries((e) => applyEvent(e, ev));
          if (ev.kind.case === "toolResult" || ev.kind.case === "finished") filesTouched({ dir });
          if (ev.kind.case === "finished") {
            setTurnUsage(usageLine(ev.kind.value.before, ev.kind.value.after));
            setTotal(ev.kind.value.after);
            void refreshTotal();
            leftover = ev.kind.value.leftover;
            const f = ev.kind.value;
            tell("finished", f.error ? t("desktop.notify.failed", { name }) : t("desktop.notify.done", { name }), f.error ? f.error.message : f.output || t("desktop.notify.finished"));
          }
        }
      } catch (e) {
        if (!ctl.signal.aborted) fail(e);
        else setEntries((e) => [...e, { kind: "notice", text: t("desktop.stopped"), tone: "info" }]);
      } finally {
        setRunning(false);
        setWaits([]);
        abort.current = null;
        following.current = "";
        done();
        if (opts.follow) refreshRuns();
      }
      try {
        const s = (await sessions.getActiveSession({ workspace: dir })).session;
        if (s?.id === session.id) setEntries((e) => assignPromptIndices(e, s.messages));
      } catch {
        // ignore
      }
      refreshList();
      onSettingsChanged();
      leftover = [...leftover, ...late.current.splice(0)];
      latestRunning.current = false;
      if (leftover.length > 0 && !ctl.signal.aborted) run(leftover.join("\n\n"), { accepted: true });
    },
    [dir, session, refreshList, refreshTotal, fail, onSettingsChanged, tell, name, snack, setEntries, setRunning, setError, setTurnUsage, stick, setTasks, setWaits, setTotal],
  );

  const stop = useCallback(async () => {
    if (following.current) await sessions.stopBackground({ id: following.current }).catch(() => abort.current?.abort());
    else abort.current?.abort();
    await finished.current;
  }, []);

  useEffect(() => {
    registerStop(dir, stop);
    return () => registerStop(dir, null);
  }, [dir, stop, registerStop]);

  return { run, stop, latestRunning, late, started };
}
