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
import { workspaces } from "../api";
import { message } from "../errors";
import { t } from "../i18n";
import { type Entry } from "../turns";

/** Runs user shell commands directly in the conversation. */
export function useConversationShell({
  dir,
  say,
  setEntries,
  setDraft,
}: {
  dir: string;
  say: (text: string, tone?: "info" | "error", markdown?: boolean) => void;
  setEntries: React.Dispatch<React.SetStateAction<Entry[]>>;
  setDraft: (d: string) => void;
}) {
  const shellRuns = useRef(new Map<string, AbortController>());
  useEffect(() => () => shellRuns.current.forEach((c) => c.abort()), []);

  const runShell = async (command: string) => {
    if (!command) return say(t("desktop.shell.usage"), "error");
    const id = `sh-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`;
    const ctl = new AbortController();
    shellRuns.current.set(id, ctl);
    const patch = (f: (e: Extract<Entry, { kind: "shell" }>) => Extract<Entry, { kind: "shell" }>) =>
      setEntries((all) => all.map((e) => (e.kind === "shell" && e.id === id ? f(e) : e)));
    setEntries((all) => [...all, { kind: "shell", id, command, output: "", running: true }]);
    try {
      for await (const m of workspaces.runShell({ workspace: dir, command }, { signal: ctl.signal })) {
        if (m.done) patch((e) => ({ ...e, running: false, code: m.exitCode, error: m.error, elapsedMs: Number(m.elapsedMs), truncated: m.truncated }));
        else if (m.output) patch((e) => ({ ...e, output: e.output + m.output }));
      }
    } catch (err) {
      patch((e) => ({ ...e, running: false, code: ctl.signal.aborted ? -1 : e.code, error: ctl.signal.aborted ? "" : message(err) }));
    } finally {
      shellRuns.current.delete(id);
      patch((e) => (e.running ? { ...e, running: false, code: -1 } : e));
    }
  };

  const stopShell = (id: string) => shellRuns.current.get(id)?.abort();
  const putShell = useCallback((command: string) => setDraft(`!${command}`), [setDraft]);

  return { runShell, stopShell, putShell };
}
