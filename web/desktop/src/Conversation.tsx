import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { timestampDate } from "@bufbuild/protobuf/wkt";
import type { JsonObject } from "@bufbuild/protobuf";
import {
  mdiAlertCircleOutline,
  mdiArrowUp,
  mdiCheck,
  mdiCheckboxBlankOutline,
  mdiCheckboxMarked,
  mdiChevronDown,
  mdiChevronRight,
  mdiClipboardCheckOutline,
  mdiClose,
  mdiConsole,
  mdiContentCopy,
  mdiDotsHorizontal,
  mdiFileDocumentOutline,
  mdiFileEditOutline,
  mdiFormatListChecks,
  mdiHelpCircleOutline,
  mdiHistory,
  mdiImagePlusOutline,
  mdiLightbulbOutline,
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
} from "@mdi/js";
import { serviceLost, sessions, workspaces } from "./api";
import { DiffView } from "./Changes";
import { isUnavailable, message, reason } from "./errors";
import type { SessionInfo } from "./gen/blitz/v1/session_pb";
import { Decision, type ApprovalRequest, type Question, type Task, type Usage } from "./gen/blitz/v1/turn_pb";
import type { GetSettingsResponse } from "./gen/blitz/v1/workspace_pb";
import { allCommands, helpText, matchCommands, parseCommand, type CommandSpec } from "./commands";
import { composeEvent, loadSessionEvent, type ComposeDetail, type LoadSessionDetail } from "./events";
import { describeImage, imageFiles, readyIds, rejectReason, uploading, type Attachment } from "./attachments";
import { Markdown } from "./Markdown";
import { notify, shouldNotify, type NotifyKind } from "./notify";
import { efforts, effortIcon, modeOf, modes } from "./options";
import { useApp } from "./state";
import { applyEvent, assignPromptIndices, failed, fromMessages, parseDiff, summarizeArgs, tasksOf, type Entry, type UserEntry } from "./turns";
import { Button, Chip, Dialog, Icon, IconButton, Menu, useSnackbar } from "./ui/controls";

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
}

/** One workspace's conversation: its sessions, the chat and the composer. */
export function Conversation({
  dir,
  name,
  visible,
  settings,
  modelProblem,
  onSettingsChanged,
  onOpenView,
}: {
  dir: string;
  name: string;
  /** The user can see this conversation (its workspace and view are shown). */
  visible: boolean;
  settings?: GetSettingsResponse;
  modelProblem: string;
  onSettingsChanged: () => void;
  onOpenView: (v: "changes" | "workers") => void;
}) {
  const { prefs, setActivity, registerStop } = useApp();
  const snack = useSnackbar();
  const [list, setList] = useState<SessionInfo[]>([]);
  const [session, setSession] = useState<SessionInfo>();
  const [entries, setEntries] = useState<Entry[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [running, setRunning] = useState(false);
  const [pending, setPending] = useState<Pending | null>(null);
  const [turnUsage, setTurnUsage] = useState("");
  const [total, setTotal] = useState<Usage>();
  const [error, setError] = useState("");
  const [draft, setDraft] = useState("");
  const [plan, setPlan] = useState(false);
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
  const [forceRewind, setForceRewind] = useState<{ index: number; mode: string; error: string } | null>(null);
  const abort = useRef<AbortController | null>(null);
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
      setTotal((await sessions.getUsage({ workspace: dir })).usage);
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
        const active = (await sessions.getActiveSession({ workspace: dir })).session;
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
    if (pending?.kind === "approval") tell("waiting", `${name}: approval needed`, `${pending.req.tool} wants to: ${pending.req.detail}`);
    if (pending?.kind === "question") tell("waiting", `${name}: the agent asks`, pending.q.question.replace(/[#*`_>]/g, "").trim());
  }, [pending, name, tell]);

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
      if (!opts.accepted)
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
      started.current = Date.now();
      setError("");
      setTurnUsage("");
      stick.current = true;
      const ctl = new AbortController();
      abort.current = ctl;
      let done!: () => void;
      finished.current = new Promise((r) => (done = r));
      let leftover: string[] = [];
      try {
        const stream = sessions.runTurn(
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
          const ev = res.event!;
          const t = tasksOf(ev);
          if (t) setTasks(t);
          else if (ev.kind.case === "approvalRequest") setPending({ kind: "approval", req: ev.kind.value });
          else if (ev.kind.case === "question") setPending({ kind: "question", q: ev.kind.value });
          else setEntries((e) => applyEvent(e, ev));
          if (ev.kind.case === "finished") {
            setTurnUsage(usageLine(ev.kind.value.before, ev.kind.value.after));
            setTotal(ev.kind.value.after);
            leftover = ev.kind.value.leftover;
            const f = ev.kind.value;
            tell("finished", f.error ? `${name}: the turn failed` : `${name}: done`, f.error ? f.error.message : f.output || "The agent finished.");
          }
        }
      } catch (e) {
        if (!ctl.signal.aborted) fail(e);
        else setEntries((e) => [...e, { kind: "notice", text: "Stopped.", tone: "info" }]);
      } finally {
        setRunning(false);
        setPending(null);
        abort.current = null;
        done();
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
      if (leftover.length > 0 && !ctl.signal.aborted) run(leftover.join("\n\n"), { accepted: true });
    },
    [dir, session, refreshList, fail, onSettingsChanged, tell, name],
  );

  const stop = useCallback(async () => {
    abort.current?.abort();
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
        snack("Images are turned off for this workspace ([images] enabled in the configuration).", { error: true });
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
  const removeAttachment = (key: string) =>
    setAttachments((list) => {
      const gone = list.find((a) => a.key === key);
      if (gone) URL.revokeObjectURL(gone.url);
      return list.filter((a) => a.key !== key);
    });

  const submit = async (text: string) => {
    if (parseCommand(text) && (await execute(text))) return;
    if (!running) {
      const asPlan = plan;
      setPlan(false);
      const images = attachments.filter((a) => a.id && !a.error);
      setAttachments([]);
      return run(text, { plan: asPlan, images });
    }
    // While a turn runs, a message steers it.
    try {
      await sessions.steer({ workspace: dir, sessionId: session!.id, text });
      setEntries((e) => [...e, { kind: "user", text, sub: "steer" }]);
    } catch (e) {
      fail(e);
    }
  };

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
      const files = res.restored.length ? ` Restored ${res.restored.length} file${res.restored.length === 1 ? "" : "s"}.` : "";
      if (mode.startsWith("summarize")) {
        const c = res.compacted;
        snack(`Summarized ${c?.eventsCompacted ?? 0} events into ${c?.summaryChars ?? 0} characters.`);
        refreshTotal();
      } else snack(mode === "code" ? `Files restored to before that prompt.${files}` : `Rewound to before that prompt.${files}`);
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
      say(`Unknown command /${c.name}. Type / to see the commands.`, "error");
      return true;
    }
    const startsTurn = spec.source !== "builtin" || ["plan", "btw", "search", "new", "undo", "compact", "rename", "session", "agent", "model"].includes(c.name);
    if (running && startsTurn) {
      say(`Wait for the agent to finish (or stop it) before /${c.name}.`, "error");
      return true;
    }
    if (spec.source !== "builtin") {
      run(line.trim(), { command: true });
      return true;
    }
    const usage = () => say(`Usage: /${spec.name}${spec.args ? " " + spec.args : ""}`, "error");
    const k = (n: bigint) => Number(n).toLocaleString();
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
            if (r.found === 0) say(`Nothing in this conversation matches “${terms}”.`);
            else run(recorded, { prompt: r.prompt, readOnly: "search", shown: recorded });
            return true;
          }
          say(`Searching the web for “${terms}”…`);
          const r = await workspaces.searchWeb({ workspace: dir, terms });
          if (r.links.length === 0) {
            say(`The search found nothing worth reading for “${terms}”.`);
            return true;
          }
          say(`Found with ${r.provider}:\n\n${r.links.map((l, i) => `${i + 1}. [${l.title.replace(/[[\]]/g, "")}](${l.url})`).join("\n")}\n\nThe agent reads them now.`, "info", true);
          run(recorded, { prompt: r.prompt, readOnly: "search", fetchGrants: r.links.map((l) => l.url), shown: recorded });
          return true;
        }
        case "undo": {
          const res = await workspaces.undo({ workspace: dir, force: c.args === "--force" });
          say(res.restored.length ? `Undid “${res.label}”: ${res.restored.join(", ")}.` : "Nothing was restored.");
          if (res.error) say(res.error.message, "error");
          return true;
        }
        case "checkpoints": {
          const r = await workspaces.listCheckpoints({ workspace: dir });
          say(
            r.checkpoints.length
              ? "**Turns that changed files** (newest first)\n\n" + r.checkpoints.map((cp) => `- ${cp.label} — ${cp.files.map((f) => `\`${f}\``).join(", ")}`).join("\n")
              : "No turn has changed files yet.",
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
          if (!u || u.calls === 0) return say("Nothing sent in this conversation yet."), true;
          say(
            c.name === "cost"
              ? `| | |\n|---|---|\n| Model calls | ${u.calls} |\n| Tokens sent | ${k(u.input)} (${k(u.cached)} cached) |\n| Tokens received | ${k(u.output)} |\n| Cost | ${u.priced ? "$" + u.costUsd.toFixed(4) : "not priced for this model"} |`
              : `The context is **${k(u.lastPrompt)} tokens**` + (r.autoCompact ? `; older turns are summarized past ${r.threshold.toLocaleString()}.` : ". Automatic compaction is off: use /compact."),
            "info",
            true,
          );
          return true;
        }
        case "compact": {
          say("Summarizing older turns…");
          const r = await sessions.compact({ workspace: dir, focus: c.args });
          say(`Summarized ${r.eventsCompacted} events into ${r.summaryChars} characters.`);
          refreshTotal();
          return true;
        }
        case "agent": {
          if (!c.args) {
            const r = await workspaces.listAgents({ workspace: dir });
            say("**Agents**\n\n" + r.agents.map((a) => `- ${a.active ? "**" : ""}${a.displayName}${a.active ? "** (active)" : ""} — \`${a.name}\`: ${a.description}`).join("\n"), "info", true);
            return true;
          }
          const r = await workspaces.setAgent({ workspace: dir, name: c.args });
          say(`Switched to ${r.agent?.displayName ?? c.args}.`);
          onSettingsChanged();
          return true;
        }
        case "model": {
          if (!c.args) {
            const m = await workspaces.getModel({ workspace: dir });
            say(`The model is \`${m.provider ? m.provider + "/" : ""}${m.name}\`${m.unavailable ? ` (unavailable: ${m.unavailable})` : ""}.`, "info", true);
            return true;
          }
          await workspaces.setModel({ workspace: dir, ref: c.args });
          say(`The model is now ${c.args}.`);
          onSettingsChanged();
          return true;
        }
        case "mode": {
          if (!c.args) return say(`The permission mode is **${modeOf(settings?.permissionMode ?? "default").label}**. Change it with /mode <name> or the chip below.`, "info", true), true;
          const r = await workspaces.setPermissionMode({ workspace: dir, mode: c.args });
          say(`Permission mode: ${modeOf(r.mode).label}.`);
          onSettingsChanged();
          return true;
        }
        case "effort": {
          if (!c.args) return say(`Reasoning effort: **${efforts.find((e) => e.value === (settings?.effort ?? ""))?.label ?? "Auto"}**.`, "info", true), true;
          await workspaces.setSetting({ workspace: dir, key: "effort", value: c.args });
          say(`Reasoning effort: ${c.args}.`);
          onSettingsChanged();
          return true;
        }
        case "session": {
          const m = /^save\s+(\S+)(\s+--force)?$/.exec(c.args);
          if (!m) return usage(), true;
          const r = await sessions.saveSnapshot({ workspace: dir, name: m[1], force: !!m[2] });
          say(`Saved as the snapshot “${r.snapshot?.snapshot ?? m[1]}”. Open it from History to continue from here.`);
          refreshList();
          return true;
        }
        case "rename":
          return c.args ? (await rename(c.args), say(`Renamed to “${c.args}”.`), true) : (usage(), true);
        case "new":
          await newSession();
          return true;
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
      if (d.dir === dir && !running) load(d.id);
    };
    window.addEventListener(loadSessionEvent, f);
    return () => window.removeEventListener(loadSessionEvent, f);
  });
  const executeRef = useRef(execute);
  executeRef.current = execute;

  // The command palette and other windows parts send text here.
  useEffect(() => {
    const f = (e: Event) => {
      const d = (e as CustomEvent<ComposeDetail>).detail;
      if (d.dir !== dir) return;
      if (d.run) executeRef.current(d.text);
      else setDraft(d.text);
    };
    window.addEventListener(composeEvent, f);
    return () => window.removeEventListener(composeEvent, f);
  }, [dir]);

  const shown = prefs.show_thoughts ? entries : entries.filter((e) => e.kind !== "thought");
  const empty = shown.length === 0 && !running;
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
          <span className="t-title">Drop images to attach them</span>
        </div>
      )}
      <SessionBar session={session} list={list} running={running} onNew={newSession} onLoad={load} onRename={rename} />
      <div className="chat-scroll" ref={scroller} onScroll={onScroll}>
        <div className="chat-column">
          {modelProblem && (
            <div className="card warn row">
              <Icon path={mdiAlertCircleOutline} />
              <span>The model isn't available: {modelProblem}</span>
            </div>
          )}
          {empty && <EmptyState name={name} onPick={(t) => setDraft(t)} />}
          {groupTools(shown).map((g) =>
            g.kind === "tools" ? (
              <ToolGroup key={g.at} tools={g.tools} />
            ) : (
              <EntryView key={g.at} entry={g.entry} running={running} onRewind={rewind} onEdit={setDraft} />
            ),
          )}
          {running && !pending && (
            <div className="working">
              <Icon path={mdiProgressClock} className="pulse" />
              <span className="muted">Working…</span>
            </div>
          )}
          {pending?.kind === "approval" && <ApprovalCard req={pending.req} onDecide={decide} />}
          {pending?.kind === "question" && <QuestionCard q={pending.q} onAnswer={answer} />}
          {error && (
            <div className="card error row">
              <Icon path={mdiAlertCircleOutline} />
              <span className="spacer">{error}</span>
              <IconButton icon={mdiClose} label="Dismiss" small onClick={() => setError("")} />
            </div>
          )}
        </div>
      </div>
      <div className="chat-column dock">
        {tasks.length > 0 && <TaskList tasks={tasks} onDismiss={running ? undefined : () => setTasks([])} />}
        <Composer
          commands={commands}
          attachments={attachments}
          onAddFiles={addFiles}
          onRemoveAttachment={removeAttachment}
          imagesOn={imagesOn}
          draft={draft}
          setDraft={setDraft}
          running={running}
          settings={settings}
          plan={plan}
          setPlan={setPlan}
          dir={dir}
          onSubmit={submit}
          onStop={stop}
          onSettingsChanged={onSettingsChanged}
        />
        <UsageFooter turn={turnUsage} total={total} />
      </div>
      {forceRewind && (
        <Dialog
          title="Files changed since"
          icon={mdiAlertCircleOutline}
          onClose={() => setForceRewind(null)}
          footer={
            <>
              <Button onClick={() => setForceRewind(null)}>Keep them</Button>
              <Button
                variant="filled"
                danger
                onClick={() => {
                  const f = forceRewind;
                  setForceRewind(null);
                  rewind(f.index, f.mode, true);
                }}
              >
                Overwrite them
              </Button>
            </>
          }
        >
          <p className="muted">{forceRewind.error}</p>
          <p className="muted">Rewinding would overwrite changes made after the agent's edits (by you, or by a command).</p>
        </Dialog>
      )}
    </div>
  );
}

function SessionBar({
  session,
  list,
  running,
  onNew,
  onLoad,
  onRename,
}: {
  session?: SessionInfo;
  list: SessionInfo[];
  running: boolean;
  onNew: () => void;
  onLoad: (id: string) => void;
  onRename: (t: string) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [title, setTitle] = useState("");
  const untitled = session?.title ? "" : "New chat";
  const commit = () => {
    setEditing(false);
    const t = title.trim();
    if (t && t !== session?.title) onRename(t);
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
          title="Rename this chat"
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
        trigger={(p) => (
          <Button icon={mdiHistory} small disabled={running} {...p}>
            History
          </Button>
        )}
        items={
          list.length === 0
            ? [{ heading: "No other chats yet" }]
            : [
                { heading: "Chats in this workspace" },
                ...list.map((s) => ({
                  label: s.snapshot ? `📸 ${s.snapshot}` : s.title || "(untitled)",
                  detail: `${s.messageCount} messages${s.updated ? ` · ${timestampDate(s.updated).toLocaleString()}` : ""}`,
                  on: s.id === session?.id,
                  onSelect: () => onLoad(s.id),
                })),
              ]
        }
      />
      <Button icon={mdiPlus} small variant="tonal" onClick={onNew} disabled={running}>
        New chat
      </Button>
    </div>
  );
}

const suggestions = [
  { icon: mdiMagnify, text: "Explain how this project is organised, and how to build and test it." },
  { icon: mdiWrenchOutline, text: "Find the most likely bug in the code I changed last, and fix it." },
  { icon: mdiFormatListChecks, text: "Write tests for the least-tested part of this code." },
  { icon: mdiLightbulbOutline, text: "Suggest three improvements to this codebase, most valuable first." },
];

function EmptyState({ name, onPick }: { name: string; onPick: (t: string) => void }) {
  return (
    <div className="empty">
      <h2 className="t-display gradient-text">What are we working on in {name}?</h2>
      <div className="suggestions">
        {suggestions.map((s) => (
          <button key={s.text} className="suggestion" onClick={() => onPick(s.text)}>
            <Icon path={s.icon} />
            <span>{s.text}</span>
          </button>
        ))}
      </div>
    </div>
  );
}

const EntryView = memo(function EntryView({
  entry,
  running,
  onRewind,
  onEdit,
}: {
  entry: Entry;
  running: boolean;
  onRewind: (index: number, mode: string) => void;
  onEdit: (text: string) => void;
}) {
  switch (entry.kind) {
    case "user":
      return <UserBubble entry={entry} running={running} onRewind={onRewind} onEdit={onEdit} />;
    case "model":
      return (
        <div className="turn-model">
          {entry.author && entry.author !== "blitz" && (
            <span className="author">
              <Icon path={mdiRobotOutline} size="sm" /> {entry.author}
            </span>
          )}
          <Markdown text={entry.text} />
        </div>
      );
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

function UserBubble({ entry, running, onRewind, onEdit }: { entry: UserEntry; running: boolean; onRewind: (index: number, mode: string) => void; onEdit: (text: string) => void }) {
  const snack = useSnackbar();
  if (entry.sub === "hook" || entry.sub === "plan") {
    return (
      <div className="notice info row">
        <Icon path={entry.sub === "plan" ? mdiClipboardCheckOutline : mdiMessageReplyTextOutline} size="sm" />
        <span>{entry.sub === "plan" ? "Plan approved: carrying it out." : entry.text.replace(/^\(stop hook\) /, "Stop hook: ")}</span>
      </div>
    );
  }
  const canRewind = entry.index !== undefined && !running;
  return (
    <div className={`turn-user ${entry.sub === "steer" ? "steer" : ""}`}>
      <div className="bubble-actions">
        <IconButton icon={mdiContentCopy} label="Copy" small onClick={() => navigator.clipboard?.writeText(entry.text).then(() => snack("Copied."))} />
        {canRewind && (
          <>
            <IconButton icon={mdiPencilOutline} label="Edit (goes back to before this prompt, files included)" small onClick={() => onRewind(entry.index!, "both")} />
            <Menu
              placement="down end"
              trigger={(p) => <IconButton icon={mdiDotsHorizontal} label="Rewind" small {...p} />}
              items={[
                { heading: "Go back to before this prompt" },
                { label: "Code and conversation", detail: "Restore the files and forget from here", icon: mdiUndoVariant, onSelect: () => onRewind(entry.index!, "both") },
                { label: "Conversation only", detail: "Keep the files as they are", icon: mdiMessageReplyTextOutline, onSelect: () => onRewind(entry.index!, "conversation") },
                { label: "Code only", detail: "Restore the files; keep the conversation", icon: mdiFileEditOutline, onSelect: () => onRewind(entry.index!, "code") },
                "divider",
                { label: "Summarize from here", detail: "Shrink the context: summarize this and later turns", icon: mdiPlaylistEdit, onSelect: () => onRewind(entry.index!, "summarize_from") },
                { label: "Summarize up to here", detail: "Summarize the turns before this one", icon: mdiPlaylistEdit, onSelect: () => onRewind(entry.index!, "summarize_up_to") },
                "divider",
                { label: "Copy into the composer", icon: mdiContentCopy, onSelect: () => onEdit(entry.text) },
              ]}
            />
          </>
        )}
      </div>
      <div className="bubble">
        {entry.sub === "steer" && <span className="t-label muted">Sent while working</span>}
        {entry.sub === "aside" && <span className="t-label muted">Side question: not kept in the conversation</span>}
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
        <span>{open ? "Thinking…" : "Thoughts"}</span>
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

type ToolEntry = Extract<Entry, { kind: "tool" }>;
type Item = { kind: "entry"; at: number; entry: Entry } | { kind: "tools"; at: number; tools: ToolEntry[] };

/** Groups runs of two or more tool calls, so an answer isn't buried in them. */
function groupTools(entries: Entry[]): Item[] {
  const out: Item[] = [];
  entries.forEach((e, i) => {
    const last = out[out.length - 1];
    if (e.kind === "tool" && last?.kind === "tools") last.tools.push(e);
    else if (e.kind === "tool" && entries[i + 1]?.kind === "tool") out.push({ kind: "tools", at: i, tools: [e] });
    else out.push({ kind: "entry", at: i, entry: e });
  });
  return out;
}

/** A run of tool calls: open while they run (or when one failed), folded after. */
function ToolGroup({ tools }: { tools: ToolEntry[] }) {
  const busy = tools.some((t) => t.result === undefined);
  const failures = tools.filter((t) => failed(t.result)).length;
  const [open, setOpen] = useState<boolean | null>(null);
  const shown = open ?? (busy || failures > 0);
  return (
    <div className={`tool-group ${shown ? "open" : ""}`}>
      <button className="tool-group-head" onClick={() => setOpen(!shown)} aria-expanded={shown}>
        <span className="tool-icons">
          {[...new Set(tools.map((t) => toolIcon(t.name)))].slice(0, 4).map((p) => (
            <Icon key={p} path={p} size="sm" />
          ))}
        </span>
        <span>
          Used {tools.length} tools{failures > 0 ? ` · ${failures} failed` : ""}
        </span>
        {busy && <Icon path={mdiProgressClock} size="sm" className="pulse" />}
        <span className="spacer" />
        <Icon path={shown ? mdiChevronDown : mdiChevronRight} size="sm" />
      </button>
      {shown && (
        <div className="tool-group-body">
          {tools.map((t, i) => (
            <ToolRow key={i} name={t.name} args={t.args} result={t.result} />
          ))}
        </div>
      )}
    </div>
  );
}

function ToolRow({ name, args, result }: { name: string; args?: JsonObject; result?: JsonObject }) {
  const [open, setOpen] = useState(false);
  const bad = failed(result);
  const status = result === undefined ? <Icon path={mdiProgressClock} size="sm" className="pulse" /> : bad ? <Icon path={mdiClose} size="sm" /> : <Icon path={mdiCheck} size="sm" />;
  return (
    <div className={`tool ${bad ? "failed" : ""} ${open ? "open" : ""}`}>
      <button className="tool-head" onClick={() => setOpen((o) => !o)} aria-expanded={open}>
        <Icon path={toolIcon(name)} size="sm" />
        <code>{name}</code>
        <span className="ellipsis muted">{summarizeArgs(args)}</span>
        <span className="spacer" />
        {bad && <span className="ellipsis tool-error">{String(result!.error)}</span>}
        {status}
      </button>
      {open && (
        <div className="tool-body">
          {args && <JsonBlock label="Arguments" value={args} />}
          {result && <JsonBlock label="Result" value={result} />}
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

function ApprovalCard({ req, onDecide }: { req: ApprovalRequest; onDecide: (d: Decision) => void }) {
  const files = req.diff ? parseDiff(req.diff) : [];
  return (
    <div className="card approval" role="alertdialog" aria-label="Approval required">
      <div className="row">
        <Icon path={mdiShieldAlertOutline} size="lg" />
        <div className="stack" style={{ gap: 2 }}>
          <span className="t-title">Allow this?</span>
          <span className="muted">
            <code>{req.tool}</code> wants to: {req.detail}
          </span>
        </div>
      </div>
      {files.length > 0 && <DiffView files={files} compact />}
      <div className="row wrap">
        <Button variant="filled" onClick={() => onDecide(Decision.ONCE)} autoFocus>
          Allow once
        </Button>
        {req.scopeLabel && (
          <Button variant="tonal" onClick={() => onDecide(Decision.SESSION)}>
            Allow {req.scopeLabel} this session
          </Button>
        )}
        {req.scopeLabel && <Button onClick={() => onDecide(Decision.ALWAYS)}>Always allow {req.scopeLabel}</Button>}
        <Button variant="outlined" danger onClick={() => onDecide(Decision.DENY)}>
          Deny
        </Button>
      </div>
    </div>
  );
}

function QuestionCard({ q, onAnswer }: { q: Question; onAnswer: (a: string) => void }) {
  const [text, setText] = useState("");
  // A plan review lists "carry it out" first: make that the primary choice.
  return (
    <div className="card question" role="alertdialog" aria-label="The agent asks">
      <div className="row">
        <Icon path={mdiHelpCircleOutline} size="lg" />
        <span className="t-title">The agent asks</span>
      </div>
      <div className="question-text">
        <Markdown text={q.question} />
      </div>
      {q.options.length > 0 && (
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
        <input className="input" value={text} onChange={(e) => setText(e.target.value)} placeholder={q.options.length ? "Or answer in your own words (feedback on a plan, say)" : "Your answer"} autoFocus={q.options.length === 0} />
        <Button variant="text" type="submit" disabled={!text.trim()}>
          Send
        </Button>
      </form>
    </div>
  );
}

function TaskList({ tasks, onDismiss }: { tasks: Task[]; onDismiss?: () => void }) {
  const done = tasks.filter((t) => t.status === "done").length;
  const allDone = done === tasks.length;
  const [open, setOpen] = useState(true);
  useEffect(() => setOpen(!allDone), [allDone]); // folds itself once every task is done
  return (
    <div className="tasks card outlined">
      <div className="row">
        <button className="tasks-head" onClick={() => setOpen((o) => !o)} aria-expanded={open}>
          <Icon path={mdiFormatListChecks} size="sm" />
          <span className="t-title-sm">Tasks</span>
          <span className="muted t-body-sm">
            {done} of {tasks.length} done
          </span>
          <Icon path={open ? mdiChevronDown : mdiChevronRight} size="sm" />
        </button>
        <span className="spacer" />
        {onDismiss && <IconButton icon={mdiClose} label="Hide the task list" small onClick={onDismiss} />}
      </div>
      <div className="progress-bar">
        <span style={{ width: `${(done / tasks.length) * 100}%` }} />
      </div>
      {open && (
        <ul className="task-items">
          {tasks.map((t, i) => (
            <li key={i} className={t.status}>
              <Icon path={t.status === "done" ? mdiCheckboxMarked : t.status === "in_progress" ? mdiProgressClock : mdiCheckboxBlankOutline} size="sm" className={t.status === "in_progress" ? "pulse" : ""} />
              <span>{t.content}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function Composer({
  commands,
  attachments,
  onAddFiles,
  onRemoveAttachment,
  imagesOn,
  draft,
  setDraft,
  running,
  settings,
  plan,
  setPlan,
  dir,
  onSubmit,
  onStop,
  onSettingsChanged,
}: {
  commands: CommandSpec[];
  attachments: Attachment[];
  onAddFiles: (files: File[]) => void;
  onRemoveAttachment: (key: string) => void;
  imagesOn: boolean;
  draft: string;
  setDraft: (t: string) => void;
  running: boolean;
  settings?: GetSettingsResponse;
  plan: boolean;
  setPlan: (p: boolean) => void;
  dir: string;
  onSubmit: (t: string) => void;
  onStop: () => void;
  onSettingsChanged: () => void;
}) {
  const snack = useSnackbar();
  const ref = useRef<HTMLTextAreaElement>(null);
  const picker = useRef<HTMLInputElement>(null);
  const busy = uploading(attachments);
  // Completing a command's name as it's typed.
  const matches = matchCommands(draft, commands);
  const [pick, setPick] = useState(0);
  const [dismissed, setDismissed] = useState("");
  const menuOpen = matches.length > 0 && dismissed !== draft;
  useEffect(() => setPick(0), [draft]);
  const complete = (c: CommandSpec) => setDraft(`/${c.name}${c.args ? " " : ""}`);
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
    const t = draft.trim();
    if (!t || busy) return;
    setDraft("");
    onSubmit(t);
  };
  const setMode = async (mode: string) => {
    try {
      await workspaces.setPermissionMode({ workspace: dir, mode });
      onSettingsChanged();
    } catch (e) {
      snack(reason(e) === "BYPASS_NEEDS_SANDBOX" ? "Bypass needs the OS sandbox, which isn't active." : message(e), { error: true });
    }
  };
  const setEffort = async (value: string) => {
    try {
      await workspaces.setSetting({ workspace: dir, key: "effort", value: value || "auto" });
      onSettingsChanged();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const mode = modeOf(settings?.permissionMode ?? "default");
  const effort = efforts.find((e) => e.value === (settings?.effort ?? "")) ?? efforts[0];
  return (
    <div className={`composer ${running ? "running" : ""}`}>
      {menuOpen && (
        <div className="command-menu" role="listbox" aria-label="Commands">
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
            <kbd>↓</kbd> choose · <kbd>Tab</kbd> complete · <kbd>Esc</kbd> close
          </div>
        </div>
      )}
      {attachments.length > 0 && (
        <div className="attachments">
          {attachments.map((a) => (
            <div key={a.key} className={`attachment ${a.error ? "failed" : ""}`} title={a.error || `${a.name}${a.detail ? ` · ${a.detail}` : ""}`}>
              <img src={a.url} alt="" />
              <span className="attachment-text">
                <span className="ellipsis">{a.name}</span>
                <small className={a.error ? "error-text ellipsis" : "muted ellipsis"}>{a.error || a.detail || "Uploading…"}</small>
              </span>
              <IconButton icon={mdiClose} label={`Remove ${a.name}`} small onClick={() => onRemoveAttachment(a.key)} />
            </div>
          ))}
        </div>
      )}
      <textarea
        ref={ref}
        value={draft}
        rows={1}
        onChange={(e) => setDraft(e.target.value)}
        onPaste={(e) => {
          const files = imageFiles(e.clipboardData.files);
          if (files.length) {
            e.preventDefault();
            onAddFiles(files);
          }
        }}
        onKeyDown={(e) => {
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
            if (e.key === "Tab" || (e.key === "Enter" && !e.shiftKey && draft !== `/${chosen.name}`)) {
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
        placeholder={running ? "Steer the agent: it reads this after its current step" : plan ? "Describe the goal to plan for…" : "Ask Blitz to do something, or type / for commands…"}
        aria-label="Message"
      />
      <div className="composer-bar">
        {!running && imagesOn && (
          <>
            <IconButton icon={mdiImagePlusOutline} label="Attach images (or paste or drop them)" small onClick={() => picker.current?.click()} />
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
        <Menu
          placement="up start"
          trigger={(p) => (
            <Chip icon={mode.icon} tone={mode.value === "bypass" ? "danger" : undefined} selected={mode.value !== "default" && mode.value !== "bypass"} title={mode.detail} {...p}>
              {mode.label}
            </Chip>
          )}
          items={[{ heading: "Permission mode" }, ...modes.map((m) => ({ label: m.label, detail: m.detail, icon: m.icon, on: m.value === mode.value, onSelect: () => setMode(m.value) }))]}
        />
        <Menu
          placement="up start"
          trigger={(p) => (
            <Chip icon={effortIcon} selected={!!effort.value} title="How hard the model thinks" {...p}>
              {effort.value ? `Effort: ${effort.label}` : "Effort"}
            </Chip>
          )}
          items={[{ heading: "Reasoning effort" }, ...efforts.map((e) => ({ label: e.label, detail: e.detail, on: e.value === effort.value, onSelect: () => setEffort(e.value) }))]}
        />
        {!running && (
          <Chip icon={mdiClipboardCheckOutline} selected={plan} onClick={() => setPlan(!plan)} title="Plan first: the agent investigates and shows you a plan to approve before changing anything">
            Plan first
          </Chip>
        )}
        <span className="spacer" />
        <span className="t-body-sm muted hint">
          <kbd>Enter</kbd> to send · <kbd>Shift</kbd>+<kbd>Enter</kbd> for a new line
        </span>
        {running && <IconButton icon={mdiStop} label="Stop" variant="tonal" onClick={onStop} />}
        <IconButton icon={mdiArrowUp} label={busy ? "Uploading images…" : running ? "Steer" : "Send"} variant="filled" disabled={!draft.trim() || busy} onClick={send} />
      </div>
    </div>
  );
}

function UsageFooter({ turn, total }: { turn: string; total?: Usage }) {
  const parts = [turn];
  if (total && total.calls > 0) {
    parts.push(`session ${k(total.input + total.output)} tokens` + (total.priced ? ` · $${total.costUsd.toFixed(4)}` : ""));
  }
  const line = parts.filter(Boolean).join("  ·  ");
  return <div className="usage t-body-sm muted">{line || " "}</div>;
}

const k = (n: bigint) => (n >= 1_000_000n ? `${(Number(n) / 1e6).toFixed(1)}M` : n >= 1000n ? `${(Number(n) / 1000).toFixed(1)}k` : String(n));

function usageLine(before?: Usage, after?: Usage): string {
  if (!after || !before || after.calls === before.calls) return "";
  let s = `This turn: ${k(after.input - before.input)} in · ${k(after.output - before.output)} out · context ${k(after.lastPrompt)}`;
  if (after.priced) s += ` · $${(after.costUsd - before.costUsd).toFixed(4)}`;
  return s;
}
