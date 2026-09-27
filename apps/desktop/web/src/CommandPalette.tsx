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

import { useEffect, useMemo, useRef, useState } from "react";
import {
  mdiCalendarClock,
  mdiCodeBraces,
  mdiCogOutline,
  mdiConsoleLine,
  mdiFileCompare,
  mdiFileSearchOutline,
  mdiFileTreeOutline,
  mdiFolderOpenOutline,
  mdiFolderOutline,
  mdiHistory,
  mdiMagnify,
  mdiMonitor,
  mdiTuneVariant,
  mdiWeatherNight,
  mdiWhiteBalanceSunny,
} from "@mdi/js";
import { sessions, workspaces } from "./api";
import { allCommands, filterPalette, type CommandSpec, type PaletteItem } from "./commands";
import { compose, goToFile, loadSession, showView } from "./events";
import { displayName, openWorkspace, openWorkspaces, recentWorkspaces } from "./prefs";
import { useApp } from "./state";
import { t, tn } from "./i18n";
import { Icon } from "./ui/controls";

/**
 * Cmd/Ctrl+K: commands, workspaces, chats, views and settings, found by
 * typing. A command without arguments runs at once; one with arguments
 * goes into the composer to finish.
 */
export function CommandPalette({ onClose, onOpenWorkspace, onSettings }: { onClose: () => void; onOpenWorkspace: () => void; onSettings: () => void }) {
  const { prefs, update } = useApp();
  const dir = prefs.active;
  const [query, setQuery] = useState("");
  const [pick, setPick] = useState(0);
  const [custom, setCustom] = useState<CommandSpec[]>([]);
  const [chats, setChats] = useState<{ id: string; title: string; detail: string }[]>([]);
  const input = useRef<HTMLInputElement>(null);
  const list = useRef<HTMLDivElement>(null);

  useEffect(() => {
    input.current?.focus();
    if (!dir) return;
    workspaces
      .listCommands({ workspace: dir })
      .then((r) => setCustom(r.commands.map((c) => ({ name: c.name, args: c.argumentHint || undefined, description: c.description || t("desktop.cmd.run", { name: c.name }), source: (c.source || "project") as CommandSpec["source"] }))))
      .catch(() => {});
    sessions
      .listSessions({ workspace: dir })
      .then((r) => setChats(r.sessions.slice(0, 30).map((s) => ({ id: s.id, title: s.snapshot ? `📸 ${s.snapshot}` : s.title || t("desktop.untitled"), detail: tn("desktop.messages", s.messageCount) }))))
      .catch(() => {});
  }, [dir]);

  const items = useMemo<PaletteItem[]>(() => {
    const done = (f: () => void) => () => {
      onClose();
      f();
    };
    const out: PaletteItem[] = [];
    if (dir) {
      for (const c of allCommands(custom)) {
        const needsArgs = !!c.args && !c.args.startsWith("[");
        out.push({
          group: t("desktop.palette.group.commands"),
          label: `/${c.name}`,
          detail: c.description,
          icon: mdiConsoleLine,
          run: done(() => {
            showView({ dir, view: "chat" });
            compose({ dir, text: needsArgs ? `/${c.name} ` : `/${c.name}`, run: !needsArgs && c.source === "builtin" });
          }),
        });
      }
      out.push(
        { group: t("desktop.palette.group.view"), label: t("desktop.files.go_to"), detail: "⌘P", icon: mdiFileSearchOutline, run: done(() => goToFile({ dir })) },
        { group: t("desktop.palette.group.view"), label: prefs.files ? t("desktop.files.hide") : t("desktop.files.show"), icon: mdiFileTreeOutline, run: done(() => update((p) => ({ ...p, files: !p.files }))) },
        { group: t("desktop.palette.group.view"), label: t("desktop.palette.show_editor"), icon: mdiCodeBraces, run: done(() => showView({ dir, view: "editor" })) },
        { group: t("desktop.palette.group.view"), label: t("desktop.palette.show_changes"), icon: mdiFileCompare, run: done(() => showView({ dir, view: "changes" })) },
        { group: t("desktop.palette.group.view"), label: t("desktop.palette.show_workers"), icon: mdiCalendarClock, run: done(() => showView({ dir, view: "workers" })) },
      );
      for (const c of chats) out.push({ group: t("desktop.palette.group.chats"), label: c.title, detail: c.detail, icon: mdiHistory, run: done(() => (showView({ dir, view: "chat" }), loadSession({ dir, id: c.id }))) });
    }
    for (const w of openWorkspaces(prefs))
      out.push({ group: t("desktop.palette.group.workspaces"), label: displayName(w), detail: w.dir, icon: mdiFolderOutline, run: done(() => update((p) => openWorkspace(p, w.dir))) });
    for (const w of recentWorkspaces(prefs))
      out.push({ group: t("desktop.palette.group.workspaces"), label: t("desktop.palette.reopen", { name: displayName(w) }), detail: w.dir, icon: mdiFolderOutline, run: done(() => update((p) => openWorkspace(p, w.dir))) });
    out.push(
      { group: t("desktop.palette.group.workspaces"), label: t("desktop.palette.open_workspace"), icon: mdiFolderOpenOutline, run: done(onOpenWorkspace) },
      { group: t("desktop.palette.group.settings"), label: t("desktop.settings"), icon: mdiCogOutline, run: done(onSettings) },
      { group: t("desktop.palette.group.settings"), label: t("desktop.palette.theme_system"), icon: mdiMonitor, run: done(() => update((p) => ({ ...p, theme: "system" }))) },
      { group: t("desktop.palette.group.settings"), label: t("desktop.palette.theme_light"), icon: mdiWhiteBalanceSunny, run: done(() => update((p) => ({ ...p, theme: "light" }))) },
      { group: t("desktop.palette.group.settings"), label: t("desktop.palette.theme_dark"), icon: mdiWeatherNight, run: done(() => update((p) => ({ ...p, theme: "dark" }))) },
      { group: t("desktop.palette.group.settings"), label: prefs.run_settings ? t("desktop.palette.hide_panel") : t("desktop.palette.show_panel"), icon: mdiTuneVariant, run: done(() => update((p) => ({ ...p, run_settings: !p.run_settings }))) },
    );
    return out;
  }, [dir, custom, chats, prefs, update, onClose, onOpenWorkspace, onSettings]);

  const shown = filterPalette(items, query);
  useEffect(() => setPick(0), [query]);
  useEffect(() => list.current?.querySelector(".palette-item.on")?.scrollIntoView({ block: "nearest" }), [pick]);

  let lastGroup = "";
  return (
    <div className="scrim palette-scrim" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="palette" role="dialog" aria-modal="true" aria-label={t("desktop.palette.label")}>
        <div className="palette-search">
          <Icon path={mdiMagnify} />
          <input
            ref={input}
            value={query}
            placeholder={t("desktop.palette.placeholder")}
            aria-label={t("desktop.search")}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape") onClose();
              else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
                e.preventDefault();
                if (shown.length) setPick((p) => (p + (e.key === "ArrowDown" ? 1 : shown.length - 1)) % shown.length);
              } else if (e.key === "Enter") {
                e.preventDefault();
                shown[pick]?.run();
              }
            }}
          />
        </div>
        <div className="palette-list" ref={list} role="listbox">
          {shown.length === 0 && <p className="palette-empty muted">{t("desktop.palette.none")}</p>}
          {shown.map((it, i) => {
            const head = it.group !== lastGroup && !query ? <div className="palette-group">{it.group}</div> : null;
            lastGroup = it.group;
            return (
              <div key={`${it.group}:${it.label}:${i}`}>
                {head}
                <button role="option" aria-selected={i === pick} className={`palette-item ${i === pick ? "on" : ""}`} onMouseEnter={() => setPick(i)} onClick={it.run}>
                  {it.icon && <Icon path={it.icon} />}
                  <span className="ellipsis">{it.label}</span>
                  {it.detail && <span className="detail ellipsis muted t-body-sm">{it.detail}</span>}
                </button>
              </div>
            );
          })}
        </div>
        <div className="palette-foot t-body-sm muted">
          <kbd>↑</kbd>
          <kbd>↓</kbd> {t("desktop.keys.choose")} · <kbd>{t("desktop.keys.enter")}</kbd> {t("desktop.keys.run")} · <kbd>{t("desktop.keys.esc")}</kbd> {t("desktop.keys.close")}
        </div>
      </div>
    </div>
  );
}
