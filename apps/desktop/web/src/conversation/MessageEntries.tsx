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

import { memo, useRef, useState } from "react";
import type { JsonObject } from "@bufbuild/protobuf";
import {
  mdiCheck,
  mdiChevronDown,
  mdiChevronRight,
  mdiClipboardCheckOutline,
  mdiClose,
  mdiContentCopy,
  mdiDotsHorizontal,
  mdiFileEditOutline,
  mdiLanguageMarkdownOutline,
  mdiLoading,
  mdiMessageReplyTextOutline,
  mdiMicrophoneOutline,
  mdiPencilOutline,
  mdiPlaylistEdit,
  mdiProgressClock,
  mdiRobotOutline,
  mdiTextBoxMultipleOutline,
  mdiUndoVariant,
  mdiWaveform,
} from "@mdi/js";
import { Icon, IconButton, Menu, useSnackbar } from "@blitz/webui";
import { copyRendered, copyText } from "../clipboard";
import { message } from "../errors";
import { useOpenPath } from "../files/links";
import { t, tn, useLanguage } from "../i18n";
import { Markdown } from "../Markdown";
import { effortIcon } from "../options";
import { useAdvanced } from "../state";
import { failed, summarizeArgs, type Citation, type Entry, type ToolEntry, type UserEntry } from "../turns";
import { kindIcon, kindOfName, toolIcon, webIcon } from "./icons";

/** Renders one entry of the conversation transcript (user, model, thought, tool, spoken, notice). */
export const EntryView = memo(function EntryView({
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
    case "spoken":
      return (
        <div className="notice info row spoken-line">
          <Icon path={entry.who === "person" ? mdiMicrophoneOutline : mdiWaveform} size="sm" />
          <span>
            <strong>{t(entry.who === "person" ? "desktop.voice.bar.you" : "desktop.voice.bar.voice")}: </strong>
            {entry.text}
          </span>
        </div>
      );
    case "reflect":
    case "shell":
    case "file":
      return null; // a ReflectCard, a ShellCard or a ModelFile, drawn with the workspace
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

/** The sources an answer cites, as a numbered list of links (spec_056 CA-84). */
function sourcesMarkdown(cites: Citation[]): string {
  const label = (s: string) => s.replace(/[[\]\\]/g, "\\$&");
  return `${t("desktop.sources")}\n\n${cites.map((c, i) => `${i + 1}. [${label(c.title || c.url)}](<${c.url}>)`).join("\n")}`;
}

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
      {!!entry.citations?.length && (
        <div className="t-body-sm muted">
          <Markdown text={sourcesMarkdown(entry.citations)} />
        </div>
      )}
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
            {entry.images.map((img, i) =>
              img.url ? (
                <img key={img.url} src={img.url} alt={img.name} title={img.name} />
              ) : (
                <span key={`${i}-${img.name}`} className="bubble-doc t-body-sm" title={img.name}>
                  <Icon path={img.web ? webIcon(img.web) : kindIcon(kindOfName(img.name))} size="sm" />
                  <span className="ellipsis">{img.name}</span>
                </span>
              ),
            )}
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

/**
 * A run of tool calls. One call is just its row; from two, a head says how
 * many, open while the turn runs (or when one failed) and folded after.
 * Either way the rows stay mounted as calls arrive, so nothing redraws.
 */
export function ToolGroup({ tools, live }: { tools: ToolEntry[]; live: boolean }) {
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
  // Working: a spinner, and for a skill's script (which can take a while) the word.
  const status =
    result === undefined ? (
      <span className="row tool-running" style={{ gap: 6 }}>
        {name === "run_skill_script" && <span className="t-body-sm muted">{t("desktop.tool.running")}</span>}
        <Icon path={mdiLoading} size="sm" spin />
      </span>
    ) : bad ? (
      <Icon path={mdiClose} size="sm" />
    ) : (
      <Icon path={mdiCheck} size="sm" />
    );
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
