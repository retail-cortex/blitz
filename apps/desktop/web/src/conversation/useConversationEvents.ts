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

import { useEffect, useRef, type Dispatch, type MutableRefObject, type RefObject, type SetStateAction } from "react";
import { sessions } from "../api";
import { focusComposerEvent } from "../composerDock";
import { parseCommand } from "../commands";
import {
  addToContextEvent,
  addWebEvent,
  composeEvent,
  loadSessionEvent,
  showPendingEvent,
  takePendingCompose,
  type AddToContextDetail,
  type AddWebDetail,
  type ComposeDetail,
  type LoadSessionDetail,
} from "../events";
import type { SessionInfo } from "../gen/blitz/v1/session_pb";
import { t } from "../i18n";
import { appendMention, isAttachablePath } from "../mentions";

/**
 * Binds window-level conversation events (focus composer, show pending request,
 * load session, add to context, add web block/screenshot, compose/run fresh).
 */
export function useConversationEvents({
  dir,
  visible,
  session,
  running,
  dockRef,
  showWaiting,
  load,
  newSession,
  show,
  refreshList,
  addImagePath,
  addWebDetail,
  setDraft,
  say,
  fail,
  executeRef,
  submitRef,
}: {
  dir: string;
  visible: boolean;
  session?: SessionInfo;
  running: boolean;
  dockRef: RefObject<HTMLDivElement | null>;
  showWaiting: () => void;
  load: (id: string) => Promise<void>;
  newSession: () => Promise<void>;
  show: (s: SessionInfo) => void;
  refreshList: () => Promise<void>;
  addImagePath: (path: string) => void;
  addWebDetail: (d: AddWebDetail) => void;
  setDraft: Dispatch<SetStateAction<string>>;
  say: (text: string, tone?: "info" | "error", markdown?: boolean) => void;
  fail: (e: unknown) => void;
  executeRef: MutableRefObject<(line: string) => Promise<boolean>>;
  submitRef: MutableRefObject<(text: string) => Promise<void>>;
}) {
  useEffect(() => {
    if (!visible) return;
    const focus = () => dockRef.current?.querySelector("textarea")?.focus();
    window.addEventListener(focusComposerEvent, focus);
    return () => window.removeEventListener(focusComposerEvent, focus);
  }, [visible, dockRef]);

  useEffect(() => {
    const f = (e: Event) => (e as CustomEvent<{ dir: string }>).detail.dir === dir && showWaiting();
    window.addEventListener(showPendingEvent, f);
    return () => window.removeEventListener(showPendingEvent, f);
  }, [dir, showWaiting]);

  useEffect(() => {
    const f = (e: Event) => {
      const d = (e as CustomEvent<LoadSessionDetail>).detail;
      if (d.dir !== dir) return;
      e.preventDefault(); // taken
      if (!running) void load(d.id);
    };
    window.addEventListener(loadSessionEvent, f);
    return () => window.removeEventListener(loadSessionEvent, f);
  });

  // The Files view adds a file or folder to the next prompt, or starts a
  // conversation about it.
  const addToContextRef = useRef<(d: AddToContextDetail) => void>(() => {});
  addToContextRef.current = async (d) => {
    if (d.fresh && !running) await newSession();
    setDraft((draft) => appendMention(d.fresh ? "" : draft, d.path));
    if (isAttachablePath(d.path)) addImagePath(d.path);
  };
  useEffect(() => {
    const f = (e: Event) => {
      const d = (e as CustomEvent<AddToContextDetail>).detail;
      if (d.dir === dir) addToContextRef.current(d);
    };
    window.addEventListener(addToContextEvent, f);
    return () => window.removeEventListener(addToContextEvent, f);
  }, [dir]);

  const addWebRef = useRef<(d: AddWebDetail) => void>(() => {});
  addWebRef.current = addWebDetail;
  useEffect(() => {
    const f = (e: Event) => {
      const d = (e as CustomEvent<AddWebDetail>).detail;
      if (d.dir === dir) addWebRef.current(d);
    };
    window.addEventListener(addWebEvent, f);
    return () => window.removeEventListener(addWebEvent, f);
  }, [dir]);

  // A command to run in a new chat (the setup), once that chat is shown.
  const runFresh = useRef<string | null>(null);
  const startFreshRef = useRef<(text: string) => void>(() => {});
  startFreshRef.current = async (text) => {
    if (running) return say(t("desktop.cmd.wait", { name: parseCommand(text)?.name ?? text }), "error");
    try {
      const s = (await sessions.newSession({ workspace: dir })).session!;
      runFresh.current = text;
      show(s);
      await refreshList();
    } catch (e) {
      fail(e);
    }
  };
  useEffect(() => {
    const text = runFresh.current;
    if (!session || text === null) return;
    runFresh.current = null;
    void executeRef.current(text); // the new chat's own execute, by now
  }, [session, executeRef]);

  // The command palette and other windows parts send text here.
  useEffect(() => {
    const f = (e: Event) => {
      const d = (e as CustomEvent<ComposeDetail>).detail;
      if (d.dir !== dir) return;
      e.preventDefault(); // taken
      if (d.run && d.fresh) startFreshRef.current(d.text);
      // A prompt rather than a command is sent as typed (Ask the agent to resolve).
      else if (d.run)
        void executeRef.current(d.text).then(async (done) => {
          if (!done) await submitRef.current(d.text);
        });
      else if (d.append) setDraft((draft) => (draft.trim() ? `${draft.trimEnd()}\n\n${d.text}` : d.text));
      else setDraft(d.text);
    };
    const waiting = takePendingCompose(dir);
    if (waiting) setDraft(waiting.text);
    window.addEventListener(composeEvent, f);
    return () => window.removeEventListener(composeEvent, f);
  }, [dir, executeRef, submitRef, setDraft]);

  return { addToContextRef };
}
