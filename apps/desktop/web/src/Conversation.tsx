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

import { memo, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import type { JsonObject } from "@bufbuild/protobuf";
import {
  mdiAlertCircleOutline,
  mdiArrowUp,
  mdiCheck,
  mdiCheckboxBlankOutline,
  mdiCheckboxMarked,
  mdiDeleteOutline,
  mdiChevronDown,
  mdiChevronRight,
  mdiClipboardCheckOutline,
  mdiClose,
  mdiConsole,
  mdiContentCopy,
  mdiLanguageMarkdownOutline,
  mdiTextBoxMultipleOutline,
  mdiDotsHorizontal,
  mdiFileDocumentOutline,
  mdiFileEditOutline,
  mdiFormatListChecks,
  mdiHelpCircleOutline,
  mdiHistory,
  mdiFolderOutline,
  mdiImageOutline,
  mdiImagePlusOutline,
  mdiMagnify,
  mdiMessageReplyTextOutline,
  mdiPencilOutline,
  mdiPlaylistEdit,
  mdiPlus,
  mdiProgressClock,
  mdiRobotOutline,
  mdiShieldAlertOutline,
  mdiStop,
  mdiUndoVariant,
  mdiWeb,
  mdiWrenchOutline,
  mdiFileCompare,
  mdiKeyboardOutline,
} from "@mdi/js";
import { files, serviceLost, sessions, workspaces } from "./api";
import { editorConfig, toEditor } from "./host";
import { DiffView } from "./Changes";
import { isUnavailable, message, reason } from "./errors";
import type { SessionInfo } from "./gen/blitz/v1/session_pb";
import { Decision, type ApprovalRequest, type Question, type Task, type Usage } from "./gen/blitz/v1/turn_pb";
import type { BackgroundTask, GetSettingsResponse, GetSuggestionsResponse } from "./gen/blitz/v1/workspace_pb";
import { allCommands, helpText, matchCommands, parseCommand, type CommandSpec } from "./commands";
import {
  addToContextEvent,
  composeEvent,
  setupCommand,
  configChanged,
  filesTouched,
  loadSessionEvent,
  showLicense,
  showPendingEvent,
  takePendingCompose,
  takePendingLoad,
  type AddToContextDetail,
  type ComposeDetail,
  type LoadSessionDetail,
} from "./events";
import { appendMention, insertMention, isImagePath, mentionAt } from "./mentions";
import { pollMs, shouldPoll, welcomeTiles, type TileAction } from "./suggestions";
import { ComposerDock, focusComposerEvent } from "./composerDock";
import { refreshRuns, runInSession } from "./backgroundRuns";
import { fileIcon } from "./files/icons";
import { describeImage, imageFiles, readyIds, rejectReason, uploading, type Attachment } from "./attachments";
import { Markdown } from "./Markdown";
import { language, t, tn, useLanguage } from "./i18n";
import { notify, shouldNotify, type NotifyKind } from "./notify";
import { efforts, effortIcon, modeOf } from "./options";
import { useAdvanced, useApp } from "./state";
import { publishStatus } from "./status";
import { useOpenPath } from "./files/links";
import { applyEvent, assignPromptIndices, failed, fromMessages, parseDiff, summarizeArgs, tasksOf, turnAnswers, type Entry, type UserEntry, groupTools, latestTurn, type ToolEntry } from "./turns";
import { copyRendered, copyText } from "./clipboard";
import { Button, Dialog, Icon, IconButton, Menu, useSnackbar } from "./ui/controls";

/** A background task's approval request or question, waiting for an answer. */
type TaskRequest = { id: string; who: string; approval?: ApprovalRequest; question?: Question };

type Pending = { kind: "approval"; req: ApprovalRequest } | { kind: "question"; q: Question };

/** How a turn is sent (see Turn in the API). */
interface TurnOptions {
  accepted?: boolean;
  plan?: boolean;
  aside?: boolean;
  command?: boolean;
  readOnly?: string;
  prompt?: string;
  fetchGrants?: string[];
  images?: Attachment[];
  /** The prompt as shown, when it differs from the text sent. */
  shown?: string;
  /** Follow this background run instead of sending text (blitz --bg). */
  follow?: string;
}

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
  onOpenView: (v: "changes" | "workers") => void;
}) {
  // The pinned bar the composer renders into (composerDock.ts), and ⌘L.
  const dockEl = useContext(ComposerDock);
  const dockRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!visible) return;
    const focus = () => dockRef.current?.querySelector("textarea")?.focus();
    window.addEventListener(focusComposerEvent, focus);
    return () => window.removeEventListener(focusComposerEvent, focus);
  }, [visible]);
  const { prefs, setActivity, registerStop } = useApp();
  const snack = useSnackbar();
  const [list, setList] = useState<SessionInfo[]>([]);
  const [session, setSession] = useState<SessionInfo>();
  const [entries, setEntries] = useState<Entry[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [running, setRunning] = useState(false);
  const [pending, setPending] = useState<Pending | null>(null);
  // Background tasks' approval requests and questions, waiting.
  const [taskRequests, setTaskRequests] = useState<TaskRequest[]>([]);
  const [turnUsage, setTurnUsage] = useState("");
  const [total, setTotal] = useState<Usage>();
  // The status bar shows the session's usage, and the last turn's.
  useEffect(() => publishStatus(dir, { total, turn: turnUsage }), [dir, total, turnUsage]);
  const [error, setError] = useState("");
  const [draft, setDraft] = useState("");
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [customCommands, setCustomCommands] = useState<CommandSpec[]>([]);
  useEffect(() => {
    workspaces
      .listCommands({ workspace: dir })
      .then((r) =>
        setCustomCommands(
          r.commands.map((c) => ({ name: c.name, args: c.argumentHint || undefined, description: c.description || `Run /${c.name}`, source: (c.source || "project") as CommandSpec["source"] })),
        ),
      )
      .catch(() => {});
  }, [dir]);
  const commands = useMemo(() => allCommands(customCommands), [customCommands]);
  const [dragging, setDragging] = useState(false);
  const [deleting, setDeleting] = useState<SessionInfo | null>(null);
  const [forceRewind, setForceRewind] = useState<{ index: number; mode: string; error: string } | null>(null);
  const abort = useRef<AbortController | null>(null);
  const following = useRef(""); // the background run the turn view follows
  const late = useRef<string[]>([]); // steer messages the turn was too late for
  const latestRunning = useRef(false);
  const finished = useRef<Promise<void>>(Promise.resolve());
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
      publishStatus(dir, { threshold: r.threshold, autoCompact: r.autoCompact });
    } catch {
      // the total is a nicety
    }
  }, [dir]);

  const show = useCallback(
    (s: SessionInfo) => {
      setSession(s);
      setEntries(fromMessages(s.messages));
      setTasks([]);
      setTurnUsage("");
      setError("");
      stick.current = true;
      refreshTotal();
    },
    [refreshTotal],
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

  // Tell the user, when they're looking elsewhere, that the agent waits or is done.
  const started = useRef(0);
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
  useEffect(() => {
    if (pending || taskRequests.length) requestAnimationFrame(() => pendingRef.current?.firstElementChild?.scrollIntoView({ block: "nearest" }));
  }, [pending, taskRequests.length]);
  useEffect(() => {
    const f = (e: Event) => (e as CustomEvent<{ dir: string }>).detail.dir === dir && showWaiting();
    window.addEventListener(showPendingEvent, f);
    return () => window.removeEventListener(showPendingEvent, f);
  }, [dir, showWaiting]);

  // Follow new output, unless the user scrolled up to read.
  useEffect(() => {
    const el = scroller.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  }, [entries, pending, tasks]);
  const onScroll = () => {
    const el = scroller.current;
    if (el) stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
  };

  const run = useCallback(
    async (text: string, opts: TurnOptions = {}) => {
      if (!session) return;
      const images = opts.images ?? [];
      if (!opts.accepted && !opts.follow)
        setEntries((e) => [
          ...e,
          {
            kind: "user",
            text: opts.shown ?? (opts.plan ? `/plan ${text}` : text),
            sub: opts.aside ? "aside" : undefined,
            images: images.length ? images.map((a) => ({ url: a.url, name: a.name })) : undefined,
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
            },
          },
          { signal: ctl.signal },
        );
        for await (const res of stream) {
          // The desktop app keeps a quiet stream moving with empty messages
          // (proxy.go): WebKitGTK may hold back what came before a pause.
          const ev = res.event;
          if (!ev) continue;
          const taskList = tasksOf(ev);
          if (taskList) setTasks(taskList);
          else if (ev.kind.case === "approvalRequest") setPending({ kind: "approval", req: ev.kind.value });
          else if (ev.kind.case === "question") setPending({ kind: "question", q: ev.kind.value });
          else setEntries((e) => applyEvent(e, ev));
          if (ev.kind.case === "toolResult" || ev.kind.case === "finished") filesTouched({ dir });
          if (ev.kind.case === "finished") {
            setTurnUsage(usageLine(ev.kind.value.before, ev.kind.value.after));
            setTotal(ev.kind.value.after);
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
        setPending(null);
        abort.current = null;
        following.current = "";
        done();
        if (opts.follow) refreshRuns();
      }
      // Prompts learn their place in the transcript, for rewinding.
      try {
        const s = (await sessions.getActiveSession({ workspace: dir })).session;
        if (s?.id === session.id) setEntries((e) => assignPromptIndices(e, s.messages));
      } catch {
        // only the rewind actions need it
      }
      refreshList();
      onSettingsChanged(); // an approved plan may have left plan mode
      // Steer messages sent after the agent's last tool call were never
      // read: they are the next turn (unless the turn was stopped).
      leftover = [...leftover, ...late.current.splice(0)];
      latestRunning.current = false;
      if (leftover.length > 0 && !ctl.signal.aborted) run(leftover.join("\n\n"), { accepted: true });
    },
    [dir, session, refreshList, fail, onSettingsChanged, tell, name],
  );

  // Stopping a followed background run stops the run, whose stream then
  // ends; a turn's stream is cut.
  const stop = useCallback(async () => {
    if (following.current) await sessions.stopBackground({ id: following.current }).catch(() => abort.current?.abort());
    else abort.current?.abort();
    await finished.current;
  }, []);
  useEffect(() => {
    registerStop(dir, stop);
    return () => registerStop(dir, null);
  }, [dir, stop, registerStop]);

  // Images for the next prompt: each uploads as soon as it's added.
  const imagesOn = settings?.imagesEnabled ?? true;
  const addFiles = useCallback(
    (files: File[]) => {
      if (!imagesOn) {
        snack(t("desktop.images.off"), { error: true });
        return;
      }
      for (const f of files) {
        const why = rejectReason(f);
        if (why) {
          snack(why, { error: true });
          continue;
        }
        const a: Attachment = { key: `${Date.now()}-${Math.random()}`, name: f.name || "pasted image", url: URL.createObjectURL(f) };
        setAttachments((list) => [...list, a]);
        (async () => {
          try {
            const res = await workspaces.addImage({ workspace: dir, name: a.name, data: new Uint8Array(await f.arrayBuffer()) });
            const img = res.image!;
            setAttachments((list) => list.map((x) => (x.key === a.key ? { ...x, id: img.id, detail: describeImage(img) } : x)));
          } catch (e) {
            setAttachments((list) => list.map((x) => (x.key === a.key ? { ...x, error: message(e) } : x)));
          }
        })();
      }
    },
    [dir, imagesOn, snack],
  );
  // An image in the workspace (an @mention of one, or Add to context).
  const addImagePath = useCallback(
    (path: string) => {
      if (!imagesOn) return;
      const a: Attachment = { key: `${Date.now()}-${Math.random()}`, name: path.slice(path.lastIndexOf("/") + 1), url: "" };
      setAttachments((list) => [...list, a]);
      workspaces.loadImage({ workspace: dir, path }).then(
        (res) => setAttachments((list) => list.map((x) => (x.key === a.key ? { ...x, id: res.image!.id, detail: describeImage(res.image!) } : x))),
        (e) => setAttachments((list) => list.map((x) => (x.key === a.key ? { ...x, error: message(e) } : x))),
      );
    },
    [dir, imagesOn],
  );
  const removeAttachment = (key: string) =>
    setAttachments((list) => {
      const gone = list.find((a) => a.key === key);
      if (gone) URL.revokeObjectURL(gone.url);
      return list.filter((a) => a.key !== key);
    });

  const submit = async (text: string) => {
    if (parseCommand(text) && (await execute(text))) return;
    if (!running) {
      const images = attachments.filter((a) => a.id && !a.error);
      setAttachments([]);
      return run(text, { images });
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

  // Background tasks of this session, followed while it's open
  // (spec_background_agents_032 BGA-41): their requests wait here, and
  // one that ends while nothing runs says so, and may start a turn.
  const latest = useRef({ running, run, continueOn: prefs.task_continue });
  latest.current = { running, run, continueOn: prefs.task_continue };
  const sessionId = session?.id;
  // A background run going on in this session (blitz --bg, the inbox;
  // spec_parity_027 PAR-PAR-20) shows as a turn from its start: its
  // requests wait here, and Stop stops it.
  useEffect(() => {
    if (!sessionId) return;
    let gone = false;
    sessions
      .listBackground({})
      .then((r) => {
        const bg = runInSession(r.runs, sessionId);
        if (bg && !gone && !latest.current.running) latest.current.run("", { follow: bg.id });
      })
      .catch(() => {});
    return () => {
      gone = true;
    };
  }, [sessionId]);
  useEffect(() => {
    if (!sessionId) return;
    setTaskRequests([]);
    const ctl = new AbortController();
    (async () => {
      while (!ctl.signal.aborted) {
        try {
          for await (const res of workspaces.watchTasks({ workspace: dir, sessionIds: [sessionId] }, { signal: ctl.signal })) {
            const k = res.event?.kind;
            if (k?.case === "approvalRequest" || k?.case === "question") {
              const r: TaskRequest = k.case === "approvalRequest" ? { id: k.value.requestId, who: `${k.value.agent} · ${k.value.taskId}`, approval: k.value } : { id: k.value.requestId, who: `${k.value.agent} · ${k.value.taskId}`, question: k.value };
              setTaskRequests((list) => (list.some((x) => x.id === r.id) ? list : [...list, r]));
            } else if (k?.case === "settingsChanged") {
              configChanged({ dir }); // rules or approvals changed on disk: views refresh
            } else if (k?.case === "resolved") {
              setTaskRequests((list) => list.filter((x) => x.id !== k.value));
            } else if (k?.case === "task" && ["done", "failed", "stopped"].includes(k.value.state)) {
              const task = k.value;
              setEntries((e) => [...e, { kind: "notice", text: t("desktop.task.ended", { id: task.id, agent: task.agent, state: t(`tasks.state.${task.state}`) }), tone: task.state === "failed" ? "error" : "info" }]);
              const { running: busy, run: go, continueOn } = latest.current;
              if (continueOn && !busy) go(t("desktop.task.continue_prompt", { id: task.id }));
            }
          }
        } catch {
          // The service went away, or the stream broke: follow again soon.
        }
        if (!ctl.signal.aborted) await new Promise((r) => setTimeout(r, 3000));
      }
    })();
    return () => ctl.abort();
  }, [dir, sessionId]);

  const answerTask = async (r: TaskRequest, reply: { decision?: Decision; text?: string }) => {
    setTaskRequests((list) => list.filter((x) => x.id !== r.id));
    const call = reply.text !== undefined ? sessions.answer({ workspace: dir, requestId: r.id, answer: reply.text }) : sessions.approve({ workspace: dir, requestId: r.id, decision: reply.decision! });
    await call.catch(fail);
  };

  // Ctrl+J: the next background task's request.
  useEffect(() => {
    if (!visible) return;
    const key = (e: KeyboardEvent) => {
      if (!e.ctrlKey || e.metaKey || e.key.toLowerCase() !== "j") return;
      const cards = [...(scroller.current?.querySelectorAll<HTMLElement>(".task-request") ?? [])];
      if (!cards.length) return;
      e.preventDefault();
      const at = cards.findIndex((c) => c.contains(document.activeElement));
      const next = cards[(at + 1) % cards.length];
      next.scrollIntoView({ block: "center" });
      next.querySelector<HTMLElement>("button, input")?.focus();
    };
    document.addEventListener("keydown", key);
    return () => document.removeEventListener("keydown", key);
  }, [visible]);

  const decide = async (decision: Decision) => {
    if (pending?.kind !== "approval") return;
    const requestId = pending.req.requestId;
    setPending(null);
    await sessions.approve({ workspace: dir, requestId, decision }).catch(fail);
  };

  const answer = async (text: string) => {
    if (pending?.kind !== "question") return;
    const requestId = pending.q.requestId;
    setPending(null);
    await sessions.answer({ workspace: dir, requestId, answer: text }).catch(fail);
  };

  const newSession = async () => {
    try {
      show((await sessions.newSession({ workspace: dir })).session!);
      await refreshList();
    } catch (e) {
      fail(e);
    }
  };
  const load = async (id: string) => {
    try {
      show((await sessions.loadSession({ workspace: dir, ref: id })).session!);
      await refreshList();
    } catch (e) {
      fail(e);
    }
  };
  const remove = async (s: SessionInfo) => {
    setDeleting(null);
    try {
      await sessions.deleteSession({ workspace: dir, sessionId: s.id });
      snack(t("desktop.chat.deleted", { title: chatTitle(s) }));
      await refreshList();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const rename = async (title: string) => {
    try {
      setSession((await sessions.renameSession({ workspace: dir, title })).session);
      await refreshList();
    } catch (e) {
      fail(e);
    }
  };

  const rewind = async (index: number, mode: string, force = false) => {
    try {
      const res = await sessions.rewind({ workspace: dir, index, mode, force });
      if (mode === "both" || mode === "conversation") {
        await reload();
        setDraft(res.prompt); // to change and send again
      }
      const files = res.restored.length ? tn("desktop.rewind.restored", res.restored.length) : "";
      if (mode.startsWith("summarize")) {
        const c = res.compacted;
        snack(t("desktop.compacted", { events: c?.eventsCompacted ?? 0, chars: c?.summaryChars ?? 0 }));
        refreshTotal();
      } else snack(mode === "code" ? t("desktop.rewind.code_done", { files }) : t("desktop.rewind.done", { files }));
    } catch (e) {
      if (reason(e) === "UNDO_CONFLICT") setForceRewind({ index, mode, error: message(e) });
      else snack(message(e), { error: true });
    }
  };

  // A line of notice text in the conversation (command results).
  const say = useCallback((text: string, tone: "info" | "error" = "info", markdown = false) => {
    stick.current = true;
    setEntries((e) => [...e, { kind: "notice", text, tone, markdown }]);
  }, []);

  /**
   * Runs a slash command; false when the line isn't one (a path, say) and
   * should go to the agent. Unknown commands are refused, never sent.
   */
  const execute = async (line: string): Promise<boolean> => {
    const c = parseCommand(line);
    if (!c) return false;
    const spec = commands.find((x) => x.name === c.name);
    if (!spec) {
      say(t("desktop.cmd.unknown", { name: c.name }), "error");
      return true;
    }
    const startsTurn = spec.source !== "builtin" || ["plan", "btw", "search", "new", "undo", "compact", "rename", "session", "agent", "model"].includes(c.name);
    if (running && startsTurn) {
      say(t("desktop.cmd.wait", { name: c.name }), "error");
      return true;
    }
    if (spec.source !== "builtin") {
      run(line.trim(), { command: true });
      return true;
    }
    const usage = () => say(t("desktop.cmd.usage", { usage: `/${spec.name}${spec.args ? " " + spec.args : ""}` }), "error");
    const k = (n: bigint) => Number(n).toLocaleString(language());
    try {
      switch (c.name) {
        case "plan":
          return c.args ? (run(c.args, { plan: true }), true) : (usage(), true);
        case "btw":
          return c.args ? (run(c.args, { aside: true, shown: `/btw ${c.args}` }), true) : (usage(), true);
        case "search": {
          const [sub, ...rest] = c.args.split(/\s+/);
          const terms = rest.join(" ").trim();
          if (!terms || (sub !== "web" && sub !== "session")) return usage(), true;
          const recorded = `/search ${sub} ${terms}`;
          if (sub === "session") {
            const r = await sessions.searchSession({ workspace: dir, terms });
            if (r.found === 0) say(t("desktop.search.session_none", { terms }));
            else run(recorded, { prompt: r.prompt, readOnly: "search", shown: recorded });
            return true;
          }
          say(t("desktop.search.searching", { terms }));
          const r = await workspaces.searchWeb({ workspace: dir, terms });
          if (r.links.length === 0) {
            say(t("desktop.search.none", { terms }));
            return true;
          }
          say(`${t("desktop.search.found", { provider: r.provider })}\n\n${r.links.map((l, i) => `${i + 1}. [${l.title.replace(/[[\]]/g, "")}](${l.url})`).join("\n")}\n\n${t("desktop.search.reading")}`, "info", true);
          run(recorded, { prompt: r.prompt, readOnly: "search", fetchGrants: r.links.map((l) => l.url), shown: recorded });
          return true;
        }
        case "undo": {
          const res = await workspaces.undo({ workspace: dir, force: c.args === "--force" });
          say(res.restored.length ? t("desktop.changes.undid", { label: res.label, files: res.restored.join(", ") }) : t("desktop.undo.nothing"));
          if (res.error) say(res.error.message, "error");
          return true;
        }
        case "checkpoints": {
          const r = await workspaces.listCheckpoints({ workspace: dir });
          say(
            r.checkpoints.length
              ? t("desktop.checkpoints.title") + "\n\n" + r.checkpoints.map((cp) => `- ${cp.label} — ${cp.files.map((f) => `\`${f}\``).join(", ")}`).join("\n")
              : t("desktop.checkpoints.none"),
            "info",
            true,
          );
          return true;
        }
        case "diff":
          onOpenView("changes");
          return true;
        case "cost":
        case "context": {
          const r = await sessions.getUsage({ workspace: dir });
          const u = r.usage;
          if (!u || u.calls === 0) return say(t("desktop.usage.none")), true;
          say(
            c.name === "cost"
              ? `| | |\n|---|---|\n| ${t("desktop.usage.calls")} | ${u.calls} |\n| ${t("desktop.usage.sent")} | ${k(u.input)} (${t("desktop.usage.cached", { count: k(u.cached) })}) |\n| ${t("desktop.usage.received")} | ${k(u.output)} |\n| ${t("desktop.usage.cost")} | ${u.priced ? "$" + u.costUsd.toFixed(4) : t("desktop.usage.unpriced")} |`
              : t("desktop.usage.context", { count: k(u.lastPrompt) }) + (r.autoCompact ? t("desktop.usage.auto", { threshold: r.threshold.toLocaleString(language()) }) : t("desktop.usage.manual")),
            "info",
            true,
          );
          return true;
        }
        case "compact": {
          say(t("desktop.compacting"));
          const r = await sessions.compact({ workspace: dir, focus: c.args });
          say(t("desktop.compacted", { events: r.eventsCompacted, chars: r.summaryChars }));
          refreshTotal();
          return true;
        }
        case "agent": {
          if (!c.args) {
            const r = await workspaces.listAgents({ workspace: dir });
            say(t("desktop.agents.title") + "\n\n" + r.agents.map((a) => `- ${a.active ? "**" : ""}${a.displayName}${a.active ? `** ${t("desktop.agents.active")}` : ""} — \`${a.name}\`: ${a.description}`).join("\n"), "info", true);
            return true;
          }
          const r = await workspaces.setAgent({ workspace: dir, name: c.args });
          say(t("desktop.agents.switched", { name: r.agent?.displayName ?? c.args }));
          onSettingsChanged();
          return true;
        }
        case "model": {
          if (!c.args) {
            const m = await workspaces.getModel({ workspace: dir });
            const ref = `${m.provider ? m.provider + "/" : ""}${m.name}`;
            say(m.unavailable ? t("desktop.model.unavailable", { model: ref, reason: m.unavailable }) : t("desktop.model.is", { model: ref }), "info", true);
            return true;
          }
          await workspaces.setModel({ workspace: dir, ref: c.args });
          say(t("desktop.model.now", { model: c.args }));
          onSettingsChanged();
          return true;
        }
        case "mode": {
          if (!c.args) return say(t("desktop.mode.is", { mode: modeOf(settings?.permissionMode ?? "default").label }), "info", true), true;
          const r = await workspaces.setPermissionMode({ workspace: dir, mode: c.args });
          say(t("desktop.mode.now", { mode: modeOf(r.mode).label }));
          onSettingsChanged();
          return true;
        }
        case "effort": {
          if (!c.args) return say(t("desktop.effort.is", { effort: efforts().find((e) => e.value === (settings?.effort ?? ""))?.label ?? t("desktop.effort.auto") }), "info", true), true;
          await workspaces.setSetting({ workspace: dir, key: "effort", value: c.args });
          say(t("desktop.effort.now", { effort: c.args }));
          onSettingsChanged();
          return true;
        }
        case "session": {
          const m = /^save\s+(\S+)(\s+--force)?$/.exec(c.args);
          if (!m) return usage(), true;
          const r = await sessions.saveSnapshot({ workspace: dir, name: m[1], force: !!m[2] });
          say(t("desktop.snapshot.saved", { name: r.snapshot?.snapshot ?? m[1] }));
          refreshList();
          return true;
        }
        case "rename":
          return c.args ? (await rename(c.args), say(t("desktop.renamed", { title: c.args })), true) : (usage(), true);
        case "new":
          await newSession();
          return true;
        case "license": {
          const which = c.args === "full" || c.args === "third-party" ? c.args : c.args ? null : "notice";
          if (!which) return usage(), true;
          showLicense({ which });
          return true;
        }
        case "help":
          say(helpText(commands), "info", true);
          return true;
      }
    } catch (e) {
      if (isUnavailable(e)) serviceLost();
      say(message(e), "error");
      return true;
    }
    return false;
  };
  useEffect(() => {
    const f = (e: Event) => {
      const d = (e as CustomEvent<LoadSessionDetail>).detail;
      if (d.dir !== dir) return;
      e.preventDefault(); // taken
      if (!running) load(d.id);
    };
    window.addEventListener(loadSessionEvent, f);
    return () => window.removeEventListener(loadSessionEvent, f);
  });
  const executeRef = useRef(execute);
  executeRef.current = execute;

  // The Files view adds a file or folder to the next prompt, or starts a
  // conversation about it.
  const addToContextRef = useRef<(d: AddToContextDetail) => void>(() => {});
  addToContextRef.current = async (d) => {
    if (d.fresh && !running) await newSession();
    setDraft((draft) => appendMention(d.fresh ? "" : draft, d.path));
    if (isImagePath(d.path)) addImagePath(d.path);
  };
  useEffect(() => {
    const f = (e: Event) => {
      const d = (e as CustomEvent<AddToContextDetail>).detail;
      if (d.dir === dir) addToContextRef.current(d);
    };
    window.addEventListener(addToContextEvent, f);
    return () => window.removeEventListener(addToContextEvent, f);
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
  }, [session]);

  // The command palette and other windows parts send text here.
  useEffect(() => {
    const f = (e: Event) => {
      const d = (e as CustomEvent<ComposeDetail>).detail;
      if (d.dir !== dir) return;
      e.preventDefault(); // taken
      if (d.run && d.fresh) startFreshRef.current(d.text);
      else if (d.run) executeRef.current(d.text);
      else if (d.append) setDraft((draft) => (draft.trim() ? `${draft.trimEnd()}\n\n${d.text}` : d.text));
      else setDraft(d.text);
    };
    const waiting = takePendingCompose(dir);
    if (waiting) setDraft(waiting.text);
    window.addEventListener(composeEvent, f);
    return () => window.removeEventListener(composeEvent, f);
  }, [dir]);

  const shown = prefs.show_thoughts ? entries : entries.filter((e) => e.kind !== "thought");
  const answers = useMemo(() => turnAnswers(shown), [shown]);
  const empty = shown.length === 0 && !running;
  // The task list and composer: under the conversation, or in the window's
  // pinned bar (composerDock.ts) while this workspace is shown.
  const dock = (
    <div className="chat-column dock" ref={dockRef}>
      {tasks.length > 0 && <TaskList tasks={tasks} onDismiss={running ? undefined : () => setTasks([])} />}
      <Composer
        commands={commands}
        attachments={attachments}
        onAddFiles={addFiles}
        onAddImagePath={addImagePath}
        onRemoveAttachment={removeAttachment}
        imagesOn={imagesOn}
        draft={draft}
        setDraft={setDraft}
        running={running}
        dir={dir}
        onSubmit={submit}
        onStop={stop}
      />
    </div>
  );

  // Where the latest turn starts: its tool groups stay open while it runs.
  const turnStart = latestTurn(shown);

  return (
    <div
      className={`chat ${dragging ? "dragging" : ""}`}
      onDragOver={(e) => {
        if (!Array.from(e.dataTransfer.types).includes("Files")) return;
        e.preventDefault();
        setDragging(true);
      }}
      onDragLeave={(e) => {
        if (e.currentTarget === e.target || !e.currentTarget.contains(e.relatedTarget as Node)) setDragging(false);
      }}
      onDrop={(e) => {
        e.preventDefault();
        setDragging(false);
        addFiles(imageFiles(e.dataTransfer.files));
      }}
    >
      {dragging && (
        <div className="drop-overlay">
          <Icon path={mdiImagePlusOutline} size="lg" />
          <span className="t-title">{t("desktop.drop")}</span>
        </div>
      )}
      <SessionBar session={session} list={list} running={running} onNew={newSession} onLoad={load} onRename={rename} onDelete={setDeleting} />
      <div className="chat-scroll" ref={scroller} onScroll={onScroll}>
        <div className="chat-column">
          {modelProblem && (
            <div className="card warn row">
              <Icon path={mdiAlertCircleOutline} />
              <span style={{ flex: 1 }}>{t("desktop.model_unavailable", { reason: modelProblem })}</span>
              <Button small onClick={onSettingsChanged}>
                {t("desktop.model_check_again")}
              </Button>
            </div>
          )}
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
          {groupTools(shown).map((g) =>
            g.kind === "tools" ? (
              <div key={g.at}>
                <ToolGroup tools={g.tools} live={running && g.at >= turnStart} />
                {g.tools.map((x) =>
                  x.name === "invoke_agent" && typeof x.result?.task_id === "string" ? <TaskCard key={x.result.task_id} dir={dir} sessionId={session?.id ?? ""} id={x.result.task_id} /> : null,
                )}
              </div>
            ) : (
              <EntryView key={g.at} entry={g.entry} turn={answers.get(g.at)} running={running} onRewind={rewind} onEdit={setDraft} />
            ),
          )}
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
      </div>
      {dockEl && visible ? createPortal(dock, dockEl) : dock}
      {deleting && (
        <Dialog
          title={t("desktop.chat.delete_title", { title: chatTitle(deleting) })}
          icon={mdiDeleteOutline}
          onClose={() => setDeleting(null)}
          footer={
            <>
              <Button onClick={() => setDeleting(null)}>{t("desktop.cancel")}</Button>
              <Button variant="filled" danger onClick={() => void remove(deleting)}>
                {t("desktop.chat.delete")}
              </Button>
            </>
          }
        >
          <p>{t("desktop.chat.delete_body")}</p>
        </Dialog>
      )}
      {forceRewind && (
        <Dialog
          title={t("desktop.conflict.title")}
          icon={mdiAlertCircleOutline}
          onClose={() => setForceRewind(null)}
          footer={
            <>
              <Button onClick={() => setForceRewind(null)}>{t("desktop.conflict.keep")}</Button>
              <Button
                variant="filled"
                danger
                onClick={() => {
                  const f = forceRewind;
                  setForceRewind(null);
                  rewind(f.index, f.mode, true);
                }}
              >
                {t("desktop.overwrite")}
              </Button>
            </>
          }
        >
          <p className="muted">{forceRewind.error}</p>
          <p className="muted">{t("desktop.rewind.overwrite_body")}</p>
        </Dialog>
      )}
    </div>
  );
}

/** How a chat is named in History: a snapshot by its name, else its title. */
function chatTitle(s: SessionInfo): string {
  return s.snapshot ? `📸 ${s.snapshot}` : s.title || t("desktop.untitled");
}

function SessionBar({
  session,
  list,
  running,
  onNew,
  onLoad,
  onRename,
  onDelete,
}: {
  session?: SessionInfo;
  list: SessionInfo[];
  running: boolean;
  onNew: () => void;
  onLoad: (id: string) => void;
  onRename: (t: string) => void;
  onDelete: (s: SessionInfo) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [title, setTitle] = useState("");
  const untitled = session?.title ? "" : t("desktop.chat.new");
  const commit = () => {
    setEditing(false);
    const next = title.trim();
    if (next && next !== session?.title) onRename(next);
  };
  return (
    <div className="session-bar chat-column">
      {editing ? (
        <input
          className="input session-title-input"
          autoFocus
          value={title}
          maxLength={80}
          onChange={(e) => setTitle(e.target.value)}
          onBlur={commit}
          onKeyDown={(e) => {
            if (e.key === "Enter") commit();
            if (e.key === "Escape") setEditing(false);
          }}
        />
      ) : (
        <button
          className="session-title"
          title={t("desktop.chat.rename")}
          onClick={() => {
            setTitle(session?.title ?? "");
            setEditing(true);
          }}
        >
          <span className="t-title ellipsis">{session?.title || untitled}</span>
          <Icon path={mdiPencilOutline} size="sm" className="title-edit" />
        </button>
      )}
      <span className="spacer" />
      <Menu
        placement="down end"
        className="wide-menu"
        trigger={(p) => <IconButton icon={mdiHistory} label={t("desktop.chat.history")} disabled={running} {...p} />}
        items={
          list.length === 0
            ? [{ heading: t("desktop.chat.none") }]
            : [
                { heading: t("desktop.chat.list") },
                ...list.map((s) => ({
                  label: chatTitle(s),
                  detail: tn("desktop.messages", s.messageCount) + (s.updated ? ` · ${timestampDate(s.updated).toLocaleString(language(), { dateStyle: "medium", timeStyle: "short" })}` : ""),
                  on: s.id === session?.id,
                  onSelect: () => onLoad(s.id),
                  // The chat in view can't be deleted: start or load another first.
                  actions: s.id === session?.id ? undefined : [{ icon: mdiDeleteOutline, label: t("desktop.chat.delete"), removes: true, onSelect: () => onDelete(s) }],
                })),
              ]
        }
      />
      <IconButton icon={mdiPlus} label={t("desktop.chat.new")} variant="tonal" onClick={onNew} disabled={running} />
    </div>
  );
}

/**
 * The welcome screen: the workspace's suggestions (GetSuggestions), asked
 * for again while a model writes ideas from the recent conversations, or
 * the canned tiles when there are none.
 */
function EmptyState({ dir, name, onDraft, onSend, onOpen }: { dir: string; name: string; onDraft: (t: string) => void; onSend: (t: string) => void; onOpen: (id: string) => void }) {
  const [res, setRes] = useState<{ r?: GetSuggestionsResponse; learning: boolean } | null>(null);
  useEffect(() => {
    let attempts = 0;
    let timer = 0;
    let gone = false;
    const ask = async () => {
      let r: GetSuggestionsResponse | undefined;
      try {
        r = await workspaces.getSuggestions({ workspace: dir });
      } catch {
        // the canned tiles, or what came before, will do
        if (!gone) setRes((was) => ({ r: was?.r, learning: false }));
        return;
      }
      if (gone) return;
      const again = shouldPoll(r, attempts);
      setRes({ r, learning: again });
      if (again) {
        attempts++;
        timer = window.setTimeout(ask, pollMs);
      }
    };
    setRes(null);
    void ask();
    return () => {
      gone = true;
      window.clearTimeout(timer);
    };
  }, [dir]);
  const choose = (a: TileAction) => {
    if (a.type === "setup") onSend(setupCommand);
    else if (a.type === "session") onOpen(a.id);
    else if (a.type === "send") onSend(a.prompt);
    else onDraft(a.text);
  };
  return (
    <div className="empty">
      <h2 className="t-display gradient-text">{t("desktop.empty.title", { name })}</h2>
      {res && (
        <div className="suggestions">
          {welcomeTiles(res.r).map((s) => (
            <button key={s.key} className={`suggestion ${s.highlight ? "highlight" : ""}`} title={s.detail} onClick={() => choose(s.action)}>
              <Icon path={s.icon} />
              <span>{s.text}</span>
              {s.highlight && s.detail && <span className="t-body-sm suggestion-detail">{s.detail}</span>}
            </button>
          ))}
        </div>
      )}
      {res?.learning && <p className="t-body-sm muted suggestions-learning">{t("desktop.suggest.learning")}</p>}
    </div>
  );
}

const EntryView = memo(function EntryView({
  entry,
  turn,
  running,
  onRewind,
  onEdit,
}: {
  entry: Entry;
  /** The whole turn's answer, when it has more than one part. */
  turn?: string;
  running: boolean;
  onRewind: (index: number, mode: string) => void;
  onEdit: (text: string) => void;
}) {
  useLanguage();
  switch (entry.kind) {
    case "user":
      return <UserBubble entry={entry} running={running} onRewind={onRewind} onEdit={onEdit} />;
    case "model":
      return <ModelAnswer entry={entry} turn={turn} />;
    case "thought":
      return <Thought text={entry.text} open={entry.open} />;
    case "tool":
      return <ToolRow name={entry.name} args={entry.args} result={entry.result} />;
    case "notice":
      return entry.markdown ? (
        <div className={`notice card ${entry.tone}`}>
          <Markdown text={entry.text} />
        </div>
      ) : (
        <div className={`notice ${entry.tone}`}>{entry.text}</div>
      );
  }
});

/**
 * Model text, with actions to copy it as shown or as its Markdown, and the
 * whole turn's answer when it came in parts. They show once it's complete.
 */
function ModelAnswer({ entry, turn }: { entry: Extract<Entry, { kind: "model" }>; turn?: string }) {
  const advanced = useAdvanced(); // copy as Markdown and the whole turn: advanced
  const snack = useSnackbar();
  const body = useRef<HTMLDivElement>(null);
  const copy = (f: () => Promise<void>, done: string) =>
    f().then(
      () => snack(done),
      (e) => snack(t("desktop.copy_failed", { error: message(e) }), { error: true }),
    );
  return (
    <div className="turn-model">
      {entry.author && entry.author !== "blitz" && (
        <span className="author">
          <Icon path={mdiRobotOutline} size="sm" /> {entry.author}
        </span>
      )}
      <div ref={body}>
        <Markdown text={entry.text} />
      </div>
      {!entry.open && entry.text.trim() && (
        <div className="bubble-actions answer-actions">
          <IconButton icon={mdiContentCopy} label={t("desktop.copy")} small onClick={() => body.current && copy(() => copyRendered(body.current!), t("desktop.copied"))} />
          {advanced && <IconButton icon={mdiLanguageMarkdownOutline} label={t("desktop.copy_markdown")} small onClick={() => copy(() => copyText(entry.text), t("desktop.copied_markdown"))} />}
          {advanced && turn && <IconButton icon={mdiTextBoxMultipleOutline} label={t("desktop.copy_turn")} small onClick={() => copy(() => copyText(turn), t("desktop.copied_turn"))} />}
        </div>
      )}
    </div>
  );
}

function UserBubble({ entry, running, onRewind, onEdit }: { entry: UserEntry; running: boolean; onRewind: (index: number, mode: string) => void; onEdit: (text: string) => void }) {
  const snack = useSnackbar();
  // The rewind menu's finer choices are advanced; Edit goes back as it says.
  const advanced = useAdvanced();
  if (entry.sub === "hook" || entry.sub === "plan") {
    return (
      <div className="notice info row">
        <Icon path={entry.sub === "plan" ? mdiClipboardCheckOutline : mdiMessageReplyTextOutline} size="sm" />
        <span>{entry.sub === "plan" ? t("desktop.plan_approved") : t("desktop.stop_hook", { text: entry.text.replace(/^\(stop hook\) /, "") })}</span>
      </div>
    );
  }
  const canRewind = entry.index !== undefined && !running;
  return (
    <div className={`turn-user ${entry.sub === "steer" ? "steer" : ""}`}>
      <div className="bubble-actions">
        <IconButton icon={mdiContentCopy} label={t("desktop.copy")} small onClick={() => navigator.clipboard?.writeText(entry.text).then(() => snack(t("desktop.copied")))} />
        {canRewind && (
          <>
            <IconButton icon={mdiPencilOutline} label={t("desktop.prompt.edit")} small onClick={() => onRewind(entry.index!, "both")} />
            {advanced && (
            <Menu
              placement="down end"
              trigger={(p) => <IconButton icon={mdiDotsHorizontal} label={t("desktop.prompt.rewind")} small {...p} />}
              items={[
                { heading: t("desktop.rewind.heading") },
                { label: t("desktop.rewind.both"), detail: t("desktop.rewind.both.detail"), icon: mdiUndoVariant, onSelect: () => onRewind(entry.index!, "both") },
                { label: t("desktop.rewind.conversation"), detail: t("desktop.rewind.conversation.detail"), icon: mdiMessageReplyTextOutline, onSelect: () => onRewind(entry.index!, "conversation") },
                { label: t("desktop.rewind.code"), detail: t("desktop.rewind.code.detail"), icon: mdiFileEditOutline, onSelect: () => onRewind(entry.index!, "code") },
                "divider",
                { label: t("desktop.rewind.from"), detail: t("desktop.rewind.from.detail"), icon: mdiPlaylistEdit, onSelect: () => onRewind(entry.index!, "summarize_from") },
                { label: t("desktop.rewind.upto"), detail: t("desktop.rewind.upto.detail"), icon: mdiPlaylistEdit, onSelect: () => onRewind(entry.index!, "summarize_up_to") },
                "divider",
                { label: t("desktop.rewind.copy"), icon: mdiContentCopy, onSelect: () => onEdit(entry.text) },
              ]}
            />
            )}
          </>
        )}
      </div>
      <div className="bubble">
        {entry.sub === "steer" && <span className="t-label muted">{t("desktop.bubble.steer")}</span>}
        {entry.sub === "aside" && <span className="t-label muted">{t("desktop.bubble.aside")}</span>}
        {entry.images && (
          <div className="bubble-images">
            {entry.images.map((img) => (
              <img key={img.url} src={img.url} alt={img.name} title={img.name} />
            ))}
          </div>
        )}
        <div className="bubble-text">{entry.text}</div>
      </div>
    </div>
  );
}

function Thought({ text, open }: { text: string; open: boolean }) {
  const [expanded, setExpanded] = useState(false);
  return (
    <div className="thought">
      <button className="thought-head" onClick={() => setExpanded((x) => !x)} aria-expanded={expanded}>
        <Icon path={effortIcon} size="sm" className={open ? "pulse" : ""} />
        <span>{open ? t("desktop.thinking") : t("desktop.thoughts")}</span>
        <Icon path={expanded ? mdiChevronDown : mdiChevronRight} size="sm" />
      </button>
      {expanded && (
        <div className="thought-body">
          <Markdown text={text} />
        </div>
      )}
    </div>
  );
}

function toolIcon(name: string): string {
  if (/^(read_file|list_files|view_image)$/.test(name)) return mdiFileDocumentOutline;
  if (/^(glob|grep|list_or_search_skills)$/.test(name)) return mdiMagnify;
  if (/(create|replace|delete|edit|patch|notebook)/.test(name)) return mdiFileEditOutline;
  if (/shell|process/.test(name)) return mdiConsole;
  if (/^web_/.test(name)) return mdiWeb;
  if (/agent/.test(name)) return mdiRobotOutline;
  if (name === "todo") return mdiFormatListChecks;
  if (name === "ask_user_question") return mdiHelpCircleOutline;
  if (/plan_mode/.test(name)) return mdiClipboardCheckOutline;
  return mdiWrenchOutline;
}

/**
 * A run of tool calls. One call is just its row; from two, a head says how
 * many, open while the turn runs (or when one failed) and folded after.
 * Either way the rows stay mounted as calls arrive, so nothing redraws.
 */
function ToolGroup({ tools, live }: { tools: ToolEntry[]; live: boolean }) {
  const busy = tools.some((x) => x.result === undefined);
  const failures = tools.filter((x) => failed(x.result)).length;
  const [open, setOpen] = useState<boolean | null>(null);
  const single = tools.length === 1;
  const shown = single || (open ?? (live || busy || failures > 0));
  return (
    <div className={`tool-group ${shown ? "open" : ""} ${single ? "single" : ""}`}>
      {!single && (
        <button className="tool-group-head" onClick={() => setOpen(!shown)} aria-expanded={shown}>
          <span className="tool-icons">
            {[...new Set(tools.map((x) => toolIcon(x.name)))].slice(0, 4).map((p) => (
              <Icon key={p} path={p} size="sm" />
            ))}
          </span>
          <span>
            {tn("desktop.tools.used", tools.length)}
            {failures > 0 ? t("desktop.tools.failed", { count: failures }) : ""}
          </span>
          {busy && <Icon path={mdiProgressClock} size="sm" className="pulse" />}
          <span className="spacer" />
          <Icon path={shown ? mdiChevronDown : mdiChevronRight} size="sm" />
        </button>
      )}
      {shown && (
        <div className="tool-group-body">
          {tools.map((x, i) => (
            <ToolRow key={x.id || i} name={x.name} args={x.args} result={x.result} />
          ))}
        </div>
      )}
    </div>
  );
}

function ToolRow({ name, args, result }: { name: string; args?: JsonObject; result?: JsonObject }) {
  // A tool's arguments and result open with advanced settings; simple mode
  // shows what it did and on which file.
  const advanced = useAdvanced();
  const [opened, setOpen] = useState(false);
  const open = advanced && opened;
  const openPath = useOpenPath();
  const bad = failed(result);
  const status = result === undefined ? <Icon path={mdiProgressClock} size="sm" className="pulse" /> : bad ? <Icon path={mdiClose} size="sm" /> : <Icon path={mdiCheck} size="sm" />;
  return (
    <div className={`tool ${bad ? "failed" : ""} ${open ? "open" : ""}`}>
      <button className="tool-head" onClick={() => advanced && setOpen((o) => !o)} aria-expanded={advanced ? open : undefined}>
        <Icon path={toolIcon(name)} size="sm" />
        <code>{name}</code>
        {openPath && typeof args?.path === "string" && args.path ? (
          <span
            role="link"
            tabIndex={0}
            className="ellipsis muted file-link"
            title={t("desktop.files.open_path", { path: args.path })}
            onClick={(e) => {
              e.stopPropagation();
              openPath(args.path as string);
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.stopPropagation();
                openPath(args.path as string);
              }
            }}
          >
            {summarizeArgs(args)}
          </span>
        ) : (
          <span className="ellipsis muted">{summarizeArgs(args)}</span>
        )}
        <span className="spacer" />
        {bad && <span className="ellipsis tool-error">{String(result!.error)}</span>}
        {status}
      </button>
      {open && (
        <div className="tool-body">
          {args && <JsonBlock label={t("desktop.tool.args")} value={args} />}
          {result && <JsonBlock label={t("desktop.tool.result")} value={result} />}
        </div>
      )}
    </div>
  );
}

/**
 * A background task (invoke_agent with background: true): its agent,
 * state, runtime and cost, followed until it ends, with Show (its latest
 * events and result) and Stop (spec_background_agents_032 BGA-33).
 */
function TaskCard({ dir, sessionId, id }: { dir: string; sessionId: string; id: string }) {
  const snack = useSnackbar();
  const [task, setTask] = useState<BackgroundTask>();
  const [events, setEvents] = useState<string[]>([]);
  const [open, setOpen] = useState(false);
  const [gone, setGone] = useState(false);
  const load = useCallback(async () => {
    try {
      const r = await workspaces.getTask({ workspace: dir, sessionIds: [sessionId], id });
      setTask(r.task);
      setEvents(r.events);
    } catch {
      setGone(true); // the service restarted, or it's another client's
    }
  }, [dir, sessionId, id]);
  useEffect(() => {
    load();
  }, [load]);
  const runningNow = !task || task.state === "running" || task.state === "waiting";
  useEffect(() => {
    if (!runningNow || gone) return;
    const timer = setInterval(load, 2000);
    return () => clearInterval(timer);
  }, [runningNow, gone, load]);
  if (gone) return null;
  const stop = async () => {
    try {
      const r = await workspaces.stopTask({ workspace: dir, sessionIds: [sessionId], id });
      setTask(r.task);
    } catch (e) {
      snack(message(e));
    }
  };
  const started = task?.started ? timestampDate(task.started).getTime() : Date.now();
  const ended = task?.ended ? timestampDate(task.ended).getTime() : Date.now();
  const seconds = Math.max(0, Math.round((ended - started) / 1000));
  return (
    <div className={`card task-card ${task?.state ?? "running"}`}>
      <div className="row">
        <Icon path={mdiRobotOutline} size="sm" />
        <span className="ellipsis" style={{ flex: 1 }}>
          <b>{task?.agent}</b> · {id} · {t(`tasks.state.${task?.state ?? "running"}`)} · {t("desktop.task.runtime", { seconds })}
          {task?.usage?.costUsd ? ` · $${task.usage.costUsd.toFixed(2)}` : ""}
        </span>
        {runningNow && <Icon path={mdiProgressClock} size="sm" className="pulse" />}
        <Button small onClick={() => setOpen((o) => !o)}>
          {open ? t("desktop.task.hide") : t("desktop.task.show")}
        </Button>
        {runningNow && (
          <Button small onClick={stop}>
            {t("desktop.task.stop")}
          </Button>
        )}
      </div>
      {task?.prompt && <div className="muted ellipsis">{task.prompt}</div>}
      {task?.branch && <div className="muted ellipsis mono">{t("tasks.worktree", { path: task.worktree, branch: task.branch })}</div>}
      {open && (
        <div className="task-body">
          {events.length > 0 && <pre className="json">{events.join("\n")}</pre>}
          {task?.result && <Markdown text={task.result} />}
          {task?.error && <p className="error-text">{task.error}</p>}
        </div>
      )}
    </div>
  );
}

function JsonBlock({ label, value }: { label: string; value: JsonObject }) {
  let text = JSON.stringify(value, null, 2);
  if (text.length > 20000) text = text.slice(0, 20000) + "\n…";
  return (
    <div>
      <div className="t-label muted">{label}</div>
      <pre className="json">{text}</pre>
    </div>
  );
}

function ApprovalCard({ req, onDecide, who, autoFocus = true }: { req: ApprovalRequest; onDecide: (d: Decision) => void; who?: string; autoFocus?: boolean }) {
  const files = req.diff ? parseDiff(req.diff) : [];
  const advanced = useAdvanced(); // Always allow: advanced
  return (
    <div className={`card approval ${who ? "task-request" : ""}`} role="alertdialog" aria-label={t("desktop.approval.label")}>
      <div className="row">
        <Icon path={mdiShieldAlertOutline} size="lg" />
        <div className="stack" style={{ gap: 2 }}>
          {who && <span className="t-label muted">{t("desktop.task.asks", { who })}</span>}
          <span className="t-title">{t("desktop.approval.title")}</span>
          <span className="muted">
            {t("desktop.approval.wants", { tool: req.tool, detail: req.detail })}
          </span>
        </div>
      </div>
      {files.length > 0 && <DiffView files={files} compact />}
      <div className="row wrap">
        {files.length > 0 && editorConfig() && (
          <Button variant="outlined" icon={mdiFileCompare} onClick={() => toEditor({ type: "diff", diff: req.diff, title: req.detail })}>
            {t("desktop.approval.in_editor")}
          </Button>
        )}
        <Button variant="filled" onClick={() => onDecide(Decision.ONCE)} autoFocus={autoFocus}>
          {t("desktop.approval.once")}
        </Button>
        {req.scopeLabel && (
          <Button variant="tonal" onClick={() => onDecide(Decision.SESSION)}>
            {t("desktop.approval.session", { scope: req.scopeLabel })}
          </Button>
        )}
        {advanced && req.scopeLabel && <Button onClick={() => onDecide(Decision.ALWAYS)}>{t("desktop.approval.always", { scope: req.scopeLabel })}</Button>}
        <Button variant="outlined" danger onClick={() => onDecide(Decision.DENY)}>
          {t("desktop.approval.deny")}
        </Button>
      </div>
    </div>
  );
}

function QuestionCard({ q, onAnswer, who, autoFocus = true }: { q: Question; onAnswer: (a: string) => void; who?: string; autoFocus?: boolean }) {
  const [text, setText] = useState("");
  // A multi-select question: the options chosen, sent together (one per line).
  const [chosen, setChosen] = useState<string[]>([]);
  const toggle = (o: string) => setChosen((c) => (c.includes(o) ? c.filter((x) => x !== o) : [...c, o]));
  // A plan review lists "carry it out" first: make that the primary choice.
  return (
    <div className={`card question ${who ? "task-request" : ""}`} role="alertdialog" aria-label={t("desktop.question.title")}>
      <div className="row">
        <Icon path={mdiHelpCircleOutline} size="lg" />
        <div className="stack" style={{ gap: 2 }}>
          {who && <span className="t-label muted">{t("desktop.task.asks", { who })}</span>}
          <span className="t-title">{t("desktop.question.title")}</span>
        </div>
      </div>
      <div className="question-text">
        <Markdown text={q.question} />
      </div>
      {q.options.length > 0 && q.multiSelect && (
        <div className="stack" role="group" aria-label={t("desktop.question.choose_several")}>
          <span className="t-label muted">{t("desktop.question.choose_several")}</span>
          {q.options.map((o) => (
            <label key={o} className="row check">
              <input type="checkbox" checked={chosen.includes(o)} onChange={() => toggle(o)} />
              <span>{o}</span>
            </label>
          ))}
          <div className="row">
            <Button variant="filled" disabled={chosen.length === 0} onClick={() => onAnswer(q.options.filter((o) => chosen.includes(o)).join("\n"))}>
              {t("desktop.send")}
            </Button>
          </div>
        </div>
      )}
      {q.options.length > 0 && !q.multiSelect && (
        <div className="row wrap">
          {q.options.map((o, i) => (
            <Button key={o} variant={i === 0 ? "filled" : "tonal"} onClick={() => onAnswer(o)}>
              {o}
            </Button>
          ))}
        </div>
      )}
      <form
        className="row"
        onSubmit={(e) => {
          e.preventDefault();
          if (text.trim()) onAnswer(text.trim());
        }}
      >
        <input className="input" value={text} onChange={(e) => setText(e.target.value)} placeholder={q.options.length ? t("desktop.question.other") : t("desktop.question.answer")} autoFocus={autoFocus && q.options.length === 0} />
        <Button variant="text" type="submit" disabled={!text.trim()}>
          {t("desktop.send")}
        </Button>
      </form>
    </div>
  );
}

function TaskList({ tasks, onDismiss }: { tasks: Task[]; onDismiss?: () => void }) {
  const done = tasks.filter((x) => x.status === "done").length;
  const allDone = done === tasks.length;
  const [open, setOpen] = useState(true);
  useEffect(() => setOpen(!allDone), [allDone]); // folds itself once every task is done
  return (
    <div className="tasks card outlined">
      <div className="row">
        <button className="tasks-head" onClick={() => setOpen((o) => !o)} aria-expanded={open}>
          <Icon path={mdiFormatListChecks} size="sm" />
          <span className="t-title-sm">{t("desktop.tasks.title")}</span>
          <span className="muted t-body-sm">
            {t("desktop.tasks.progress", { done, total: tasks.length })}
          </span>
          <Icon path={open ? mdiChevronDown : mdiChevronRight} size="sm" />
        </button>
        <span className="spacer" />
        {onDismiss && <IconButton icon={mdiClose} label={t("desktop.tasks.hide")} small onClick={onDismiss} />}
      </div>
      <div className="progress-bar">
        <span style={{ width: `${(done / tasks.length) * 100}%` }} />
      </div>
      {open && (
        <ul className="task-items">
          {tasks.map((task, i) => (
            <li key={i} className={task.status}>
              <Icon path={task.status === "done" ? mdiCheckboxMarked : task.status === "in_progress" ? mdiProgressClock : mdiCheckboxBlankOutline} size="sm" className={task.status === "in_progress" ? "pulse" : ""} />
              <span>{task.content}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

// The composer's keys, as a keyboard icon whose tooltip lists them (on
// hover, and on focus for the keyboard).
function KeyHint() {
  const text = `${t("desktop.keys.enter")} ${t("desktop.keys.to_send")} · ${t("desktop.keys.shift")}+${t("desktop.keys.enter")} ${t("desktop.keys.new_line")} · ${t("desktop.composer.plan_hint")}`;
  return (
    <span className="key-hint" tabIndex={0} role="note" aria-label={text} data-tip={text}>
      <Icon path={mdiKeyboardOutline} size="sm" />
    </span>
  );
}

function Composer({
  commands,
  attachments,
  onAddFiles,
  onAddImagePath,
  onRemoveAttachment,
  imagesOn,
  draft,
  setDraft,
  running,
  dir,
  onSubmit,
  onStop,
}: {
  commands: CommandSpec[];
  attachments: Attachment[];
  onAddFiles: (files: File[]) => void;
  onAddImagePath: (path: string) => void;
  onRemoveAttachment: (key: string) => void;
  imagesOn: boolean;
  draft: string;
  setDraft: (t: string) => void;
  running: boolean;
  dir: string;
  onSubmit: (t: string) => void;
  onStop: () => void;
}) {
  const ref = useRef<HTMLTextAreaElement>(null);
  const picker = useRef<HTMLInputElement>(null);
  const busy = uploading(attachments);
  // Completing a command's name as it's typed.
  const matches = matchCommands(draft, commands);
  const [pick, setPick] = useState(0);
  // The draft Escape closed the menus on: forgotten once the draft
  // changes, so the same text typed again (a "/" in an emptied box) opens
  // them again.
  const [dismissed, setDismissed] = useState("");
  const menuOpen = matches.length > 0 && dismissed !== draft;
  useEffect(() => {
    setPick(0);
    setDismissed((d) => (d === draft ? d : ""));
  }, [draft]);
  const complete = (c: CommandSpec) => setDraft(`/${c.name}${c.args ? " " : ""}`);
  // Completing an @mention: the workspace's files and folders.
  const [caret, setCaret] = useState(0);
  const mention = menuOpen ? null : mentionAt(draft, caret);
  const [found, setFound] = useState<{ query: string; paths: string[] }>({ query: "", paths: [] });
  useEffect(() => {
    if (!mention) return;
    const query = mention.query;
    let live = true;
    const timer = setTimeout(() => {
      files.findFiles({ workspace: dir, query, limit: 12, folders: true }).then(
        (r) => live && setFound({ query, paths: r.paths }),
        () => live && setFound({ query, paths: [] }),
      );
    }, 120);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [mention?.query, dir]); // eslint-disable-line react-hooks/exhaustive-deps
  const mentionOpen = !!mention && found.query === mention.query && found.paths.length > 0 && dismissed !== draft;
  const choosePath = (path: string) => {
    if (!mention) return;
    const r = insertMention(draft, mention, path);
    setDraft(r.text);
    setCaret(r.caret);
    requestAnimationFrame(() => ref.current?.setSelectionRange(r.caret, r.caret));
    if (isImagePath(path)) onAddImagePath(path);
  };
  const trackCaret = (e: { currentTarget: HTMLTextAreaElement }) => setCaret(e.currentTarget.selectionStart ?? 0);
  // Grow with the text, up to a limit.
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 280)}px`;
  }, [draft]);
  useEffect(() => {
    if (draft) ref.current?.focus();
  }, [draft]);
  const send = () => {
    const text = draft.trim();
    if (!text || busy) return;
    setDraft("");
    onSubmit(text);
  };
  return (
    <div className={`composer ${running ? "running" : ""}`}>
      {menuOpen && (
        <div className="command-menu" role="listbox" aria-label={t("desktop.commands")}>
          {matches.map((c, i) => (
            <button
              key={c.name}
              role="option"
              aria-selected={i === pick}
              className={`command-option ${i === pick ? "on" : ""}`}
              onMouseDown={(e) => {
                e.preventDefault(); // keep the focus in the field
                complete(c);
              }}
            >
              <code>/{c.name}</code>
              {c.args && <span className="muted mono t-body-sm">{c.args}</span>}
              <span className="spacer ellipsis muted t-body-sm">{c.description}</span>
              {c.source !== "builtin" && <span className="chip static command-source">{c.source}</span>}
            </button>
          ))}
          <div className="command-hint t-body-sm muted">
            <kbd>↑</kbd>
            <kbd>↓</kbd> {t("desktop.keys.choose")} · <kbd>{t("desktop.keys.tab")}</kbd> {t("desktop.keys.complete")} · <kbd>{t("desktop.keys.esc")}</kbd> {t("desktop.keys.close")}
          </div>
        </div>
      )}
      {mentionOpen && (
        <div className="command-menu" role="listbox" aria-label={t("desktop.mention.files")}>
          {found.paths.map((p, i) => (
            <button
              key={p}
              role="option"
              aria-selected={i === pick}
              className={`command-option ${i === pick ? "on" : ""}`}
              onMouseDown={(e) => {
                e.preventDefault(); // keep the focus in the field
                choosePath(p);
              }}
            >
              <Icon path={p.endsWith("/") ? mdiFolderOutline : fileIcon(p.slice(p.lastIndexOf("/", p.length - 2) + 1))} size="sm" />
              <span className="mono ellipsis">{p}</span>
            </button>
          ))}
          <div className="command-hint t-body-sm muted">
            <kbd>↑</kbd>
            <kbd>↓</kbd> {t("desktop.keys.choose")} · <kbd>{t("desktop.keys.tab")}</kbd> {t("desktop.mention.add")} · <kbd>{t("desktop.keys.esc")}</kbd> {t("desktop.keys.close")}
          </div>
        </div>
      )}
      {attachments.length > 0 && (
        <div className="attachments">
          {attachments.map((a) => (
            <div key={a.key} className={`attachment ${a.error ? "failed" : ""}`} title={a.error || `${a.name}${a.detail ? ` · ${a.detail}` : ""}`}>
              {a.url ? <img src={a.url} alt="" /> : <Icon path={mdiImageOutline} />}
              <span className="attachment-text">
                <span className="ellipsis">{a.name}</span>
                <small className={a.error ? "error-text ellipsis" : "muted ellipsis"}>{a.error || a.detail || t("desktop.attach.uploading")}</small>
              </span>
              <IconButton icon={mdiClose} label={t("desktop.attach.remove", { name: a.name })} small onClick={() => onRemoveAttachment(a.key)} />
            </div>
          ))}
        </div>
      )}
      {/* The prompt mark: shown only when pinned (composerDock.ts). */}
      <span className="composer-mark" aria-hidden="true">
        ›
      </span>
      <textarea
        ref={ref}
        value={draft}
        rows={1}
        onChange={(e) => {
          setDraft(e.target.value);
          trackCaret(e);
        }}
        onSelect={trackCaret}
        onPaste={(e) => {
          const files = imageFiles(e.clipboardData.files);
          if (files.length) {
            e.preventDefault();
            onAddFiles(files);
          }
        }}
        onKeyDown={(e) => {
          if (mentionOpen && !e.nativeEvent.isComposing) {
            const n = found.paths.length;
            if (e.key === "ArrowDown" || e.key === "ArrowUp") {
              e.preventDefault();
              setPick((p) => (p + (e.key === "ArrowDown" ? 1 : n - 1)) % n);
              return;
            }
            if (e.key === "Escape") {
              e.preventDefault();
              setDismissed(draft);
              return;
            }
            if (e.key === "Tab" || (e.key === "Enter" && !e.shiftKey)) {
              e.preventDefault();
              choosePath(found.paths[Math.min(pick, n - 1)]);
              return;
            }
          }
          if (menuOpen && !e.nativeEvent.isComposing) {
            const chosen = matches[Math.min(pick, matches.length - 1)];
            if (e.key === "ArrowDown" || e.key === "ArrowUp") {
              e.preventDefault();
              setPick((p) => (p + (e.key === "ArrowDown" ? 1 : matches.length - 1)) % matches.length);
              return;
            }
            if (e.key === "Escape") {
              e.preventDefault();
              setDismissed(draft);
              return;
            }
            // Tab completes; Enter completes too, unless the name is already whole.
            if (e.key === "Tab" || (e.key === "Enter" && !e.shiftKey && draft.trim() !== `/${chosen.name}`)) {
              e.preventDefault();
              complete(chosen);
              return;
            }
          }
          if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
            e.preventDefault();
            send();
          }
        }}
        placeholder={running ? t("desktop.composer.steer") : t("desktop.composer.ask")}
        aria-label={t("desktop.composer.message")}
      />
      <div className="composer-bar">
        {!running && imagesOn && (
          <>
            <IconButton icon={mdiImagePlusOutline} label={t("desktop.attach")} small onClick={() => picker.current?.click()} />
            <input
              ref={picker}
              type="file"
              accept="image/*"
              multiple
              hidden
              onChange={(e) => {
                onAddFiles(imageFiles(e.target.files));
                e.target.value = "";
              }}
            />
          </>
        )}
        <span className="spacer" />
        <KeyHint />
        {running && <IconButton icon={mdiStop} label={t("desktop.stop")} variant="tonal" onClick={onStop} />}
        <IconButton icon={mdiArrowUp} label={busy ? t("desktop.uploading") : running ? t("desktop.steer") : t("desktop.send")} variant="filled" disabled={!draft.trim() || busy} onClick={send} />
      </div>
    </div>
  );
}

const k = (n: bigint) => (n >= 1_000_000n ? `${(Number(n) / 1e6).toFixed(1)}M` : n >= 1000n ? `${(Number(n) / 1000).toFixed(1)}k` : String(n));

function usageLine(before?: Usage, after?: Usage): string {
  if (!after || !before || after.calls === before.calls) return "";
  let s = t("desktop.usage.turn", { in: k(after.input - before.input), out: k(after.output - before.output), context: k(after.lastPrompt) });
  if (after.priced) s += ` · $${(after.costUsd - before.costUsd).toFixed(4)}`;
  return s;
}
