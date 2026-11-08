/*
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

import { mdiClose, mdiConsole, mdiConsoleLine, mdiPlus } from "@mdi/js";
import { Drawer, Icon, IconButton, useSnackbar } from "@blitz/webui";
import { useState, type PointerEvent as ReactPointerEvent } from "react";
import { message } from "../errors";
import { t } from "../i18n";
import { closeTerminal, drawerHeight, hideTerminals, newTerminal, showTerminals, terminalExited, useTermDrawer } from "./drawer";
import { termName } from "./terminals";
import { TerminalView } from "./TerminalView";

/** Where the drawer's height is kept (0: the default, half the window). */
const heightKey = "blitz.terminal.height";

/**
 * A workspace's terminals in a drop-down drawer (spec_062_terminal
 * TRM-20, TRM-30), Yakuake-style: it slides down from under the top menu
 * over the middle of the window, a tab for each terminal. Putting it up
 * only hides it: the terminals go on, drawn, until they're closed. Escape
 * stays the shell's (vim's, less's), so ✕, its button or ⌃` put it up.
 */
export function TerminalDrawer({ dir, visible, onOpenFile }: { dir: string; visible: boolean; onOpenFile: (path: string, line?: number, column?: number) => void }) {
  const st = useTermDrawer(dir);
  const snack = useSnackbar();
  const [height, setHeight] = useState(() => {
    try {
      return Number(localStorage.getItem(heightKey)) || 0;
    } catch {
      return 0;
    }
  });
  if (!st.list.length) return null;
  const open = visible && st.open;
  const fail = (e: unknown) => snack(message(e), { error: true });
  const keep = (h: number) => {
    setHeight(h);
    try {
      localStorage.setItem(heightKey, String(h));
    } catch {
      // the default next time
    }
  };
  // Its bottom edge, dragged: the height it's left at is kept.
  const drag = (e: ReactPointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    const drawer = e.currentTarget.closest<HTMLElement>(".drawer")!;
    const top = drawer.getBoundingClientRect().top;
    const room = (drawer.parentElement?.getBoundingClientRect().height ?? 600) - 8;
    let h = drawer.getBoundingClientRect().height;
    const move = (ev: PointerEvent) => {
      h = Math.round(Math.min(Math.max(ev.clientY - top, 160), room));
      drawer.style.height = `${h}px`;
    };
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      document.body.classList.remove("resizing");
      keep(h);
    };
    document.body.classList.add("resizing");
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };
  const room = typeof window === "undefined" ? 0 : window.innerHeight - 120;
  const h = drawerHeight(height, room);
  return (
    <Drawer
      title={t("desktop.terminal.title")}
      icon={mdiConsole}
      open={open}
      escapeCloses={false}
      onClose={() => hideTerminals(dir)}
      className="terminal-drawer"
      style={h ? { height: h } : undefined}
      headerActions={<IconButton icon={mdiPlus} label={t("desktop.terminal.new")} onClick={() => void newTerminal(dir, "").catch(fail)} />}
    >
      <div className="editor-tabs terminal-tabs" role="tablist">
        {st.list.map((x) => {
          const name = termName(x.dir, dir);
          return (
            <div
              key={x.id}
              role="tab"
              aria-selected={x.id === st.active}
              className={`editor-tab ${x.id === st.active ? "active" : ""}`}
              title={x.dir || name}
              onClick={() => showTerminals(dir, x.id)}
              onAuxClick={(e) => e.button === 1 && void closeTerminal(dir, x.id)}
            >
              <Icon path={mdiConsoleLine} size="sm" />
              <span className="ellipsis">{name}</span>
              <IconButton
                icon={mdiClose}
                label={t("desktop.terminal.close", { name })}
                small
                onClick={(e) => {
                  e.stopPropagation();
                  void closeTerminal(dir, x.id);
                }}
              />
            </div>
          );
        })}
      </div>
      {st.list.map((x) => (
        <TerminalView
          key={x.id}
          workspace={dir}
          entry={x}
          name={termName(x.dir, dir)}
          hidden={!open || x.id !== st.active}
          onExit={() => void terminalExited(dir, x.id)}
          onOpenFile={onOpenFile}
        />
      ))}
      <div
        className="terminal-drawer-resize"
        role="separator"
        aria-orientation="horizontal"
        aria-label={t("desktop.terminal.resize")}
        title={t("desktop.resize_hint")}
        onPointerDown={drag}
        onDoubleClick={() => keep(0)}
      />
    </Drawer>
  );
}
