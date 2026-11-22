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

import { useEffect, useRef, useState, type ReactNode } from "react";
import {
  mdiArrowUp,
  mdiBrushVariant,
  mdiClose,
  mdiDatabaseOutline,
  mdiFolderOutline,
  mdiImagePlusOutline,
  mdiKeyboardOutline,
  mdiPuzzleOutline,
  mdiStop,
  mdiTableSearch,
} from "@mdi/js";
import { Icon, IconButton } from "@blitz/webui";
import { files, workspaces } from "../api";
import { failedUpload, uploading, type Attachment, type WebBlock } from "../attachments";
import { matchCommands, type CommandSpec } from "../commands";
import { chatConnections, DataMenu, DataResultDialog } from "../DataContext";
import { dbOptions, wantsDb, type DbConnection } from "../dbMentions";
import { openSettings, showView } from "../events";
import { fileIcon } from "../files/icons";
import { t } from "../i18n";
import { insertMention, isAttachablePath, mentionAt } from "../mentions";
import { useSecretSuggest } from "../SecretSuggest";
import { shellLine } from "../shellRun";
import { insertSkill, skillAt, skillOptions, skillsFor } from "../skillMention";
import { kindIcon, webIcon } from "./icons";

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

/** Chat message composer with slash-command, `@file`/`@db`, `\skill`, and secret completions plus attachments. */
export function Composer({
  commands,
  attachments,
  onAddFiles,
  onAddImagePath,
  onRemoveAttachment,
  onAddData,
  imagesOn,
  accept,
  draft,
  setDraft,
  running,
  dir,
  onSubmit,
  onStop,
  voice,
}: {
  commands: CommandSpec[];
  attachments: Attachment[];
  onAddFiles: (files: File[]) => void;
  onAddImagePath: (path: string) => void;
  onRemoveAttachment: (key: string) => void;
  /** Adds a database, or a query's result, to the next message (spec_059_data DAT-41, DAT-42). */
  onAddData: (b: WebBlock) => void;
  imagesOn: boolean;
  accept: string; // the file picker's: what the model takes
  draft: string;
  setDraft: (t: string) => void;
  running: boolean;
  dir: string;
  onSubmit: (t: string) => void;
  onStop: () => void;
  /** The microphone, beside Send (spec_063_voice VOX-20). */
  voice?: ReactNode;
}) {
  const ref = useRef<HTMLTextAreaElement>(null);
  const picker = useRef<HTMLInputElement>(null);
  const [viewing, setViewing] = useState<WebBlock | null>(null);
  const busy = uploading(attachments);
  // An attachment that didn't upload is removed before sending, not left out.
  const failed = failedUpload(attachments);
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
  // @db: completes the workspace's databases and their saved queries
  // (spec_059_data DAT-40), above the files.
  const dbWanted = !!mention && wantsDb(mention.query);
  const [dbConns, setDbConns] = useState<DbConnection[]>([]);
  useEffect(() => {
    if (!dbWanted) return;
    let live = true;
    workspaces.listConnectors({ workspace: dir }).then(
      (r) => live && setDbConns(chatConnections(r.connections)),
      () => live && setDbConns([]),
    );
    return () => {
      live = false;
    };
  }, [dbWanted, dir]);
  const dbFound = mention && dbWanted ? dbOptions(mention.query, dbConns) : [];
  const paths = mention && found.query === mention.query ? found.paths : [];
  const mentionCount = dbFound.length + paths.length;
  const mentionOpen = !!mention && mentionCount > 0 && dismissed !== draft;
  const choosePath = (path: string) => {
    if (!mention) return;
    const r = insertMention(draft, mention, path);
    setDraft(r.text);
    setCaret(r.caret);
    requestAnimationFrame(() => ref.current?.setSelectionRange(r.caret, r.caret));
    if (isAttachablePath(path)) onAddImagePath(path);
  };
  const chooseDb = (m: string) => {
    if (!mention) return;
    const r = insertMention(draft, mention, m);
    setDraft(r.text);
    setCaret(r.caret);
    requestAnimationFrame(() => ref.current?.setSelectionRange(r.caret, r.caret));
  };
  const chooseMention = (i: number) => (i < dbFound.length ? chooseDb(dbFound[i].mention) : choosePath(paths[i - dbFound.length]));
  // Completing \skill-name: the skills the workspace sees.
  const skillToken = menuOpen || mention ? null : skillAt(draft, caret);
  const [skillList, setSkillList] = useState<{ name: string; description: string }[]>([]);
  useEffect(() => {
    if (skillToken) void skillsFor(dir).then(setSkillList);
  }, [!!skillToken, dir]); // eslint-disable-line react-hooks/exhaustive-deps
  const skillFound = skillToken ? skillOptions(skillToken.query, skillList) : [];
  const skillOpen = !!skillToken && skillFound.length > 0 && dismissed !== draft;
  const chooseSkill = (name: string) => {
    if (!skillToken) return;
    const r = insertSkill(draft, skillToken, name);
    setDraft(r.text);
    setCaret(r.caret);
    requestAnimationFrame(() => ref.current?.setSelectionRange(r.caret, r.caret));
  };
  // Completing {secrets:…}: the workspace's and the global names.
  const secretSuggest = useSecretSuggest(draft, setDraft, ref, dir);
  const trackCaret = (e: { currentTarget: HTMLTextAreaElement }) => {
    setCaret(e.currentTarget.selectionStart ?? 0);
    secretSuggest.trackCaret(e);
  };
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
    if (!text || busy || failed) return;
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
      {!menuOpen && !mentionOpen && !skillOpen && secretSuggest.menu}
      {skillOpen && (
        <div className="command-menu" role="listbox" aria-label={t("desktop.skills.mention")}>
          {skillFound.map((s, i) => (
            <button
              key={s.name}
              role="option"
              aria-selected={i === pick}
              className={`command-option ${i === pick ? "on" : ""}`}
              onMouseDown={(e) => {
                e.preventDefault(); // keep the focus in the field
                chooseSkill(s.name);
              }}
            >
              <Icon path={mdiPuzzleOutline} size="sm" />
              <code>\{s.name}</code>
              <span className="spacer ellipsis muted t-body-sm">{s.description}</span>
            </button>
          ))}
          <div className="command-hint t-body-sm muted">
            <kbd>↑</kbd>
            <kbd>↓</kbd> {t("desktop.keys.choose")} · <kbd>{t("desktop.keys.tab")}</kbd> {t("desktop.skills.mention_add")} · <kbd>{t("desktop.keys.esc")}</kbd> {t("desktop.keys.close")}
          </div>
        </div>
      )}
      {mentionOpen && (
        <div className="command-menu" role="listbox" aria-label={t("desktop.mention.files")}>
          {dbFound.map((o, i) => (
            <button
              key={o.mention}
              role="option"
              aria-selected={i === pick}
              className={`command-option ${i === pick ? "on" : ""}`}
              onMouseDown={(e) => {
                e.preventDefault(); // keep the focus in the field
                chooseDb(o.mention);
              }}
            >
              <Icon path={o.query ? mdiTableSearch : mdiDatabaseOutline} size="sm" />
              <code>@{o.mention}</code>
              <span className="spacer ellipsis muted t-body-sm">{o.detail}</span>
            </button>
          ))}
          {paths.map((p, j) => {
            const i = j + dbFound.length;
            return (
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
            );
          })}
          <div className="command-hint t-body-sm muted">
            <kbd>↑</kbd>
            <kbd>↓</kbd> {t("desktop.keys.choose")} · <kbd>{t("desktop.keys.tab")}</kbd> {t("desktop.mention.add")} · <kbd>{t("desktop.keys.esc")}</kbd> {t("desktop.keys.close")}
          </div>
        </div>
      )}
      {attachments.length > 0 && (
        <div className="attachments">
          {attachments.map((a) => (
            <div
              key={a.key}
              className={`attachment ${a.error ? "failed" : ""} ${a.web?.table ? "clickable" : ""}`}
              title={a.error || `${a.name}${a.detail ? ` · ${a.detail}` : ""}`}
              onClick={a.web?.table ? () => setViewing(a.web!) : undefined}
            >
              {a.url ? <img src={a.url} alt="" /> : <Icon path={a.web ? webIcon(a.web.kind) : kindIcon(a.kind ?? (a.document ? "document" : "image"))} />}
              <span className="attachment-text">
                <span className="ellipsis">{a.name}</span>
                <small className={a.error ? "error-text ellipsis" : "muted ellipsis"}>{a.error || a.detail || t("desktop.attach.uploading")}</small>
              </span>
              <IconButton icon={mdiClose} label={t("desktop.attach.remove", { name: a.name })} small onClick={() => onRemoveAttachment(a.key)} />
            </div>
          ))}
        </div>
      )}
      {viewing && <DataResultDialog block={viewing} onClose={() => setViewing(null)} />}
      {/* The prompt mark: shown only when pinned (composerDock.ts). */}
      <span className="composer-mark" aria-hidden="true">
        ›
      </span>
      <textarea
        ref={ref}
        value={draft}
        rows={1}
        className={shellLine(draft).shell ? "shell-mode" : undefined}
        onChange={(e) => {
          setDraft(e.target.value);
          trackCaret(e);
        }}
        onSelect={trackCaret}
        onPaste={(e) => {
          const files = Array.from(e.clipboardData.files);
          if (files.length) {
            e.preventDefault();
            onAddFiles(files);
          }
        }}
        onKeyDown={(e) => {
          if (!menuOpen && !mentionOpen && !skillOpen && secretSuggest.onKeyDown(e)) return;
          if (skillOpen && !e.nativeEvent.isComposing) {
            const n = skillFound.length;
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
              chooseSkill(skillFound[Math.min(pick, n - 1)].name);
              return;
            }
          }
          if (mentionOpen && !e.nativeEvent.isComposing) {
            const n = mentionCount;
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
              chooseMention(Math.min(pick, n - 1));
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
            <IconButton title="" icon={mdiImagePlusOutline} label={t("desktop.attach")} small onClick={() => picker.current?.click()} />
            <input
              ref={picker}
              type="file"
              accept={accept}
              multiple
              hidden
              onChange={(e) => {
                onAddFiles(Array.from(e.target.files ?? []));
                e.target.value = "";
              }}
            />
          </>
        )}
        {!running && <IconButton title="" icon={mdiBrushVariant} label={t("desktop.composer.make_picture")} small onClick={() => showView({ dir, view: "pictures" })} />}
        {!running && <DataMenu dir={dir} onAdd={onAddData} onConnect={() => openSettings({ section: "connectors" })} />}
        {shellLine(draft).shell && <span className="t-body-sm shell-text ellipsis">{t("desktop.shell.hint")}</span>}
        <span className="spacer" />
        <KeyHint />
        {voice}
        {running && <IconButton title="" icon={mdiStop} label={t("desktop.stop")} variant="tonal" onClick={onStop} />}
        <IconButton title="" icon={mdiArrowUp} label={busy ? t("desktop.uploading") : failed ? t("desktop.attach.failed") : running ? t("desktop.steer") : t("desktop.send")} variant="filled" disabled={!draft.trim() || busy || failed} onClick={send} />
      </div>
    </div>
  );
}
