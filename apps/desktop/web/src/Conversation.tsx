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

import { useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { mdiAlertCircleOutline, mdiClose, mdiImagePlusOutline, mdiProgressClock } from "@mdi/js";
import { Button, Icon, IconButton, useSnackbar } from "@blitz/webui";
import { serviceLost, sessions, workspaces } from "./api";
import { RepoCodeNotice } from "./RepoCodeNotice";
import { acceptAttribute, webBlocks } from "./attachments";
import { allCommands, listedCommands, parseCommand, type CommandSpec } from "./commands";
import { ComposerDock } from "./composerDock";
import { ApprovalCard, QuestionCard } from "./conversation/ApprovalCards";
import { Composer } from "./conversation/Composer";
import { executeSlashCommand } from "./conversation/executeSlashCommand";
import { DeleteSessionsDialog, EmptyState, ForceRewindDialog, SessionBar } from "./conversation/SessionChrome";
import { TaskList } from "./conversation/StatusCards";
import { useAttachments } from "./conversation/useAttachments";
import { useBackgroundTasks } from "./conversation/useBackgroundTasks";
import { useConversationEvents } from "./conversation/useConversationEvents";
import { liveThought, showPrompt } from "./reasoning/chatLink";
import { ReasoningDrawer } from "./reasoning/ReasoningDrawer";
import { ConversationStream } from "./conversation/ConversationStream";
import { useConversationRun } from "./conversation/useConversationRun";
import { useConversationShell } from "./conversation/useConversationShell";
import { useConversationVoice } from "./conversation/useConversationVoice";
import { useSessionOps } from "./conversation/useSessionOps";
import { isUnavailable, message, reason } from "./errors";
import { openSettings, takePendingLoad } from "./events";
import { dragType, treeDrag } from "./files/tree";
import type { SessionInfo } from "./gen/blitz/v1/session_pb";
import { Decision, type Task, type Usage } from "./gen/blitz/v1/turn_pb";
import type { GetSettingsResponse } from "./gen/blitz/v1/workspace_pb";
import { t, tn } from "./i18n";
import { useLoopsEndWith } from "./loops";
import { notify, shouldNotify, type NotifyKind } from "./notify";
import { plainReason } from "./providerStatus";
import { ShellRunContext, shellLine } from "./shellRun";
import type { CommandContext } from "./slashCommands";
import { useApp } from "./state";
import { publishStatus } from "./status";
import { fromMessages, latestTurn, turnAnswers, withTools, type Entry } from "./turns";
import { VoiceBar, VoiceButton } from "./voice/VoiceControls";
import { dropWait, type Pending } from "./waits";

export { ApprovalCard };

/** One workspace's conversation: its sessions, the chat and the composer. */
export function Conversation({
  dir,
  name,
  visible,
  settings,
  modelProblem,
  projectWaiting = 0,
  onReviewProject,
  onSettingsChanged,
  onOpenView,
  expand,
}: {
  dir: string;
  name: string;
  /** The user can see this conversation (its workspace and view are shown). */
  visible: boolean;
  settings?: GetSettingsResponse;
  modelProblem: string;
  /** Project settings off for want of trust. */
  projectWaiting?: number;
  onReviewProject?: () => void;
  onSettingsChanged: () => void;
  /** Opens a view (/diff, /processes, …); unset where there are none (the VS Code panel), which says so. */
  onOpenView?: CommandContext["openView"];
  /** The chat expanded over the editor, or not, while files are open (its header's button). */
  expand?: { expanded: boolean; toggle: () => void };
}) {
  // The pinned bar the composer renders into (composerDock.ts), and ⌘L.
  const dockEl = useContext(ComposerDock);
  const dockRef = useRef<HTMLDivElement>(null);
  const { prefs, update, setActivity, registerStop } = useApp();
  const snack = useSnackbar();
  const [list, setList] = useState<SessionInfo[]>([]);
  const [session, setSession] = useState<SessionInfo>();
  const [entries, setEntries] = useState<Entry[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [running, setRunning] = useState(false);
  // The turn's requests waiting for an answer, oldest first, shown one at
  // a time (waits.ts).
  const [waits, setWaits] = useState<Pending[]>([]);
  const pending = waits[0] ?? null;
  const [turnUsage, setTurnUsage] = useState("");
  const [total, setTotal] = useState<Usage>();
  // The status bar shows the session's usage, and the last turn's.
  useEffect(() => publishStatus(dir, { total, turn: turnUsage }), [dir, total, turnUsage]);
  useEffect(() => publishStatus(dir, { conversationModel: session?.model || undefined, sessionId: session?.id }), [dir, session?.model, session?.id]);
  const [error, setError] = useState("");
  const [draft, setDraft] = useState("");
  const [customCommands, setCustomCommands] = useState<CommandSpec[]>([]);
  useEffect(() => {
    workspaces
      .listCommands({ workspace: dir })
      .then((r) => setCustomCommands(listedCommands(r.commands)))
      .catch(() => {});
  }, [dir]);
  const commands = useMemo(() => allCommands(customCommands), [customCommands]);
  const [dragging, setDragging] = useState(false);
  const [deleting, setDeleting] = useState<SessionInfo[] | null>(null);
  // The Reasoning view (spec_066_reasoning), asked to look again as the conversation changes.
  const [reasoningOpen, setReasoningOpen] = useState(false);
  const [changes, setChanges] = useState(0);
  useEffect(() => setChanges((n) => n + 1), [entries]);
  const [forceRewind, setForceRewind] = useState<{ index: number; mode: string; error: string } | null>(null);
  const scroller = useRef<HTMLDivElement>(null);
  const stick = useRef(true);

  const fail = useCallback((e: unknown) => {
    if (isUnavailable(e)) serviceLost();
    setError(message(e));
  }, []);

  const refreshList = useCallback(async () => {
    try {
      setList((await sessions.listSessions({ workspace: dir })).sessions);
    } catch (e) {
      fail(e);
    }
  }, [dir, fail]);

  const refreshTotal = useCallback(async () => {
    try {
      const r = await sessions.getUsage({ workspace: dir });
      setTotal(r.usage);
      publishStatus(dir, { threshold: r.threshold, autoCompact: r.autoCompact, byModel: r.byModel });
    } catch {
      // the total is a nicety
    }
  }, [dir]);

  // The session shown, for answers that arrive after another is shown.
  const shownId = useRef("");
  const show = useCallback(
    (s: SessionInfo) => {
      setSession(s);
      setEntries(fromMessages(s.messages));
      shownId.current = s.id;
      // Its tool calls, back where they ran (an older service has none to give).
      if (s.messages.length)
        sessions.listSessionTools({ workspace: dir, sessionId: s.id }).then(
          (r) => shownId.current === s.id && r.tools.length && setEntries((cur) => withTools(cur, r.tools)),
          () => {},
        );
      setTasks([]);
      setTurnUsage("");
      setError("");
      stick.current = true;
      refreshTotal();
    },
    [dir, refreshTotal],
  );

  // Reloads the active session's messages (after a rewind, say).
  const reload = useCallback(async () => {
    const s = (await sessions.getActiveSession({ workspace: dir })).session;
    if (s) show(s);
    await refreshList();
  }, [dir, show, refreshList]);

  useEffect(() => {
    (async () => {
      try {
        // A session asked for before the workspace opened (the inbox).
        const waiting = takePendingLoad(dir);
        const active = waiting ? (await sessions.loadSession({ workspace: dir, ref: waiting })).session : (await sessions.getActiveSession({ workspace: dir })).session;
        show(active ?? (await sessions.newSession({ workspace: dir })).session!);
        await refreshList();
      } catch (e) {
        fail(e);
      }
    })();
  }, [dir, show, refreshList, fail]);

  useEffect(() => setActivity(dir, { running, waiting: !!pending }), [dir, running, pending, setActivity]);
  // A closed (or remounted) workspace's activity goes with it.
  useEffect(() => () => setActivity(dir, null), [dir, setActivity]);
  useLoopsEndWith(dir);

  // Tell the user, when they're looking elsewhere, that the agent waits or is done.
  const visibleRef = useRef(visible);
  visibleRef.current = visible;
  const tell = useCallback(
    (kind: NotifyKind, title: string, body: string) => {
      const s = { enabled: prefs.notifications === "on", focused: document.hasFocus(), shown: visibleRef.current, elapsedMs: Date.now() - started.current };
      if (shouldNotify(kind, s)) notify(title, body, dir);
    },
    [prefs.notifications, dir],
  );
  useEffect(() => {
    if (pending?.kind === "approval") tell("waiting", t("desktop.notify.approval", { name }), t("desktop.notify.approval_body", { tool: pending.req.tool, detail: pending.req.detail }));
    if (pending?.kind === "question") tell("waiting", t("desktop.notify.asks", { name }), pending.q.question.replace(/[#*`_>]/g, "").trim());
  }, [pending, name, tell]);

  // What waits for the user is brought into view as it comes, and when the
  // status bar's "Waiting for you" is clicked.
  const pendingRef = useRef<HTMLDivElement>(null);
  const showWaiting = useCallback(() => {
    const el = pendingRef.current?.firstElementChild as HTMLElement | null;
    el?.scrollIntoView({ block: "nearest", behavior: "smooth" });
    el?.querySelector<HTMLElement>("button, textarea, input")?.focus({ preventScroll: true });
  }, []);

  // Follow new output, unless the user scrolled up to read.
  useEffect(() => {
    const el = scroller.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  }, [entries, pending, tasks]);
  const onScroll = () => {
    const el = scroller.current;
    if (el) stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
  };

  const { run, stop, latestRunning, late, started } = useConversationRun({
    dir,
    name,
    session,
    running,
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
  });

  const { attachments, setAttachments, imagesOn, accepted, addFiles, addImagePath, removeAttachment, addData, addWebDetail, clearAttachments } = useAttachments({ dir, settings, snack });

  // A line of notice text in the conversation (command results).
  const say = useCallback((text: string, tone: "info" | "error" = "info", markdown = false) => {
    stick.current = true;
    setEntries((e) => [...e, { kind: "notice", text, tone, markdown }]);
  }, []);

  // ! commands run as the user's own (WorkspaceService.RunShell), each a
  // card in the chat with Stop while it runs; the agent never sees them.
  const { runShell, stopShell, putShell } = useConversationShell({ dir, say, setEntries, setDraft });

  const submit = async (text: string) => {
    const sh = shellLine(text);
    if (sh.shell) return void runShell(sh.command);
    if (parseCommand(text) && (await execute(text))) return;
    if (!running) {
      const images = attachments.filter((a) => a.id && !a.error);
      const context = webBlocks(attachments);
      setAttachments([]);
      return run(text, { images, context });
    }
    // While a turn runs, a message steers it.
    try {
      await sessions.steer({ workspace: dir, sessionId: session!.id, text });
      setEntries((e) => [...e, { kind: "user", text, sub: "steer" }]);
    } catch (e) {
      // The turn ended as it was sent: it's recorded, and goes as the next
      // turn, with the unread ones (BL-SVC-10).
      if (reason(e) !== "STEER_TOO_LATE") return fail(e);
      setEntries((x) => [...x, { kind: "user", text, sub: "steer" }]);
      if (latestRunning.current) late.current.push(text);
      else run(text, { accepted: true });
    }
  };

  const { taskRequests, answerTask } = useBackgroundTasks({
    dir,
    sessionId: session?.id,
    visible,
    running,
    continueOn: prefs.task_continue,
    latestRunning,
    run,
    setEntries,
    scroller,
    fail,
  });

  useEffect(() => {
    if (pending || taskRequests.length) requestAnimationFrame(() => pendingRef.current?.firstElementChild?.scrollIntoView({ block: "nearest" }));
  }, [pending, taskRequests.length]);

  const decide = async (decision: Decision) => {
    if (pending?.kind !== "approval") return;
    const requestId = pending.req.requestId;
    setWaits((w) => dropWait(w, requestId));
    await sessions.approve({ workspace: dir, requestId, decision }).catch(fail);
  };

  const answer = async (text: string) => {
    if (pending?.kind !== "question") return;
    const requestId = pending.q.requestId;
    setWaits((w) => dropWait(w, requestId));
    await sessions.answer({ workspace: dir, requestId, answer: text }).catch(fail);
  };

  const { newSession, load, remove, rename, rewind } = useSessionOps({
    dir,
    show,
    reload,
    refreshList,
    refreshTotal,
    fail,
    snack,
    setDeleting,
    setForceRewind,
    setSession,
    setDraft,
  });

  // A /loop's firing: a command runs as typed, anything else is sent (the
  // loop skips a firing while a turn runs).
  const sendRef = useRef<(text: string) => void>(() => {});
  const execute = (line: string) =>
    executeSlashCommand(line, {
      dir,
      commands,
      running,
      latestRunning,
      settings,
      sessionId: session?.id,
      sessionTitle: session?.title,
      projectWaiting,
      entries,
      prefs,
      updatePrefs: update,
      say,
      run,
      sendRef,
      snack,
      onSettingsChanged,
      onOpenView,
      onReviewProject,
      reload,
      refreshTotal,
      refreshList,
      newSession,
      rename,
      rewind,
      addImagePath,
      clearAttachments,
    });
  sendRef.current = (text: string) => {
    if (text.startsWith("/")) void execute(text);
    else void run(text);
  };

  const executeRef = useRef(execute);
  executeRef.current = execute;
  const submitRef = useRef(submit);
  submitRef.current = submit;

  const { myVoice, voice, voiceState, toggleVoice } = useConversationVoice({
    dir,
    sessionId: session?.id ?? "",
    visible,
    running,
    micId: prefs.voice_mic,
    pending,
    taskRequests,
    submitRef,
    stop,
    decide,
    answer,
    answerTask,
    setEntries,
    snack,
  });

  const { addToContextRef } = useConversationEvents({
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
  });

  const shown = prefs.show_thoughts ? entries : entries.filter((e) => e.kind !== "thought");
  const answers = useMemo(() => turnAnswers(shown), [shown]);
  const empty = shown.length === 0 && !running;
  // The task list and composer: under the conversation, or in the window's
  // pinned bar (composerDock.ts) while this workspace is shown.
  const dock = (
    <div className="chat-column dock" ref={dockRef}>
      {tasks.length > 0 && <TaskList tasks={tasks} onDismiss={running ? undefined : () => setTasks([])} />}
      {myVoice && <VoiceBar conv={myVoice} onMute={voice.mute} onEnd={voice.end} />}
      <Composer
        commands={commands}
        attachments={attachments}
        onAddFiles={addFiles}
        onAddImagePath={addImagePath}
        onRemoveAttachment={removeAttachment}
        onAddData={addData}
        imagesOn={imagesOn}
        accept={acceptAttribute(accepted)}
        draft={draft}
        setDraft={setDraft}
        running={running}
        dir={dir}
        onSubmit={submit}
        onStop={stop}
        voice={<VoiceButton state={voiceState} hold={myVoice?.listen === "hold"} onToggle={toggleVoice} onHold={voice.hold} />}
      />
    </div>
  );

  // Where the latest turn starts: its tool groups stay open while it runs.
  const turnStart = latestTurn(shown);

  return (
    <div
      className={`chat ${dragging ? "dragging" : ""}`}
      onDragOver={(e) => {
        const types = Array.from(e.dataTransfer.types);
        const fromTree = types.includes(dragType) || treeDrag.current?.dir === dir;
        if (!types.includes("Files") && !fromTree) return;
        e.preventDefault();
        // Rows from the Files tree become context, not attachments (FIL-76).
        if (fromTree) e.dataTransfer.dropEffect = "link";
        else setDragging(true);
      }}
      onDragLeave={(e) => {
        if (e.currentTarget === e.target || !e.currentTarget.contains(e.relatedTarget as Node)) setDragging(false);
      }}
      onDrop={(e) => {
        e.preventDefault();
        setDragging(false);
        const rows = treeDrag.current?.dir === dir ? treeDrag.current.paths : null;
        const typed = e.dataTransfer.getData(dragType);
        treeDrag.current = null;
        if (rows || typed) {
          for (const path of rows ?? typed.split("\n").filter(Boolean)) addToContextRef.current({ dir, path });
          return;
        }
        addFiles(Array.from(e.dataTransfer.files));
      }}
    >
      {dragging && (
        <div className="drop-overlay">
          <Icon path={mdiImagePlusOutline} size="lg" />
          <span className="t-title">{t("desktop.drop")}</span>
        </div>
      )}
      <SessionBar
        session={session}
        list={list}
        running={running}
        onNew={newSession}
        onLoad={load}
        onRename={rename}
        onDelete={setDeleting}
        expand={expand}
        reasoning={{ open: reasoningOpen, toggle: () => setReasoningOpen((o) => !o) }}
      />
      <div className="chat-scroll" ref={scroller} onScroll={onScroll}>
        <ShellRunContext.Provider value={putShell}>
          <div className="chat-column">
            {modelProblem && (
              <div className="card warn row">
                <Icon path={mdiAlertCircleOutline} />
                <span style={{ flex: 1 }}>{t("desktop.model_unavailable", { reason: plainReason(modelProblem) })}</span>
                <Button small onClick={onSettingsChanged}>
                  {t("desktop.model_check_again")}
                </Button>
                <Button small variant="tonal" onClick={() => openSettings({ section: "providers" })}>
                  {t("desktop.model_set_up")}
                </Button>
              </div>
            )}
            <RepoCodeNotice dir={dir} />
            {projectWaiting > 0 && (
              <div className="card warn row">
                <Icon path={mdiAlertCircleOutline} />
                <span style={{ flex: 1 }}>{tn("desktop.project.bar", projectWaiting)}</span>
                <Button small onClick={onReviewProject}>
                  {t("desktop.project.review")}
                </Button>
              </div>
            )}
            {empty && <EmptyState dir={dir} name={name} onDraft={setDraft} onSend={(text) => void submit(text)} onOpen={(id) => void load(id)} />}
            <ConversationStream
              shown={shown}
              running={running}
              turnStart={turnStart}
              dir={dir}
              sessionId={session?.id ?? ""}
              answers={answers}
              stopShell={stopShell}
              rewind={rewind}
              setDraft={setDraft}
            />
            {running && !pending && (
              <div className="working">
                <Icon path={mdiProgressClock} className="pulse" />
                <span className="muted">{t("desktop.working")}</span>
              </div>
            )}
            <div className="pending" ref={pendingRef}>
              {pending?.kind === "approval" && <ApprovalCard req={pending.req} onDecide={decide} />}
              {pending?.kind === "question" && <QuestionCard q={pending.q} onAnswer={answer} />}
              {taskRequests.map((r) =>
                r.approval ? (
                  <ApprovalCard key={r.id} who={r.who} req={r.approval} autoFocus={false} onDecide={(decision) => answerTask(r, { decision })} />
                ) : (
                  <QuestionCard key={r.id} who={r.who} q={r.question!} autoFocus={false} onAnswer={(text) => answerTask(r, { text })} />
                ),
              )}
            </div>
            {error && (
              <div className="card error row">
                <Icon path={mdiAlertCircleOutline} />
                <span className="spacer">{error}</span>
                <IconButton icon={mdiClose} label={t("desktop.dismiss")} small onClick={() => setError("")} />
              </div>
            )}
          </div>
        </ShellRunContext.Provider>
      </div>
      {dockEl && visible ? createPortal(dock, dockEl) : dock}
      {reasoningOpen && visible && (
        <ReasoningDrawer
          dir={dir}
          sessionId={session?.id ?? ""}
          running={running}
          activity={changes}
          live={liveThought(entries, running)}
          onShowInChat={(turn) => {
            setReasoningOpen(false);
            requestAnimationFrame(() => showPrompt(scroller.current, turn));
          }}
          onClose={() => setReasoningOpen(false)}
        />
      )}
      {deleting && <DeleteSessionsDialog deleting={deleting} onCancel={() => setDeleting(null)} onConfirm={(l) => void remove(l)} />}
      {forceRewind && (
        <ForceRewindDialog
          conflict={forceRewind}
          onCancel={() => setForceRewind(null)}
          onForce={(index, mode) => {
            setForceRewind(null);
            void rewind(index, mode, true);
          }}
        />
      )}
    </div>
  );
}
