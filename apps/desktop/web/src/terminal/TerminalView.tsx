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

import { mdiChevronDown, mdiChevronUp, mdiClose, mdiMagnify } from "@mdi/js";
import { ContextMenu, Icon, IconButton, useSnackbar } from "@blitz/webui";
import { FitAddon } from "@xterm/addon-fit";
import { SearchAddon } from "@xterm/addon-search";
import { Unicode11Addon } from "@xterm/addon-unicode11";
import { WebLinksAddon } from "@xterm/addon-web-links";
import { WebglAddon } from "@xterm/addon-webgl";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { useEffect, useRef, useState } from "react";
import { files, terminals } from "../api";
import { openURL } from "../desktop";
import { message } from "../errors";
import { t } from "../i18n";
import { useApp } from "../state";
import { isToggleTerminalKey, type TermEntry } from "./drawer";
import { bundledFont, fontFamily, pathLinks, termColors, termKey } from "./terminals";
import "./terminal.css";

const mac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);

// Whether el is on screen with a size: a hidden tab's isn't (offsetParent
// can't tell, under the window's fixed layout).
const shown = (el: HTMLElement | null) => !!el && el.isConnected && el.offsetWidth > 0 && el.offsetHeight > 0;

/**
 * A terminal in the drawer (spec_062_terminal TRM-20 to TRM-26): xterm.js
 * drawing the service's terminal, its output from the stream, its keys and
 * size sent back. It stays mounted while another terminal is shown or the
 * drawer is up (hidden), so nothing is replayed when it's shown again.
 */
export function TerminalView({
  workspace,
  entry,
  name,
  hidden,
  onExit,
  onOpenFile,
}: {
  workspace: string;
  entry: TermEntry;
  /** What it's called (its folder's name). */
  name: string;
  hidden: boolean;
  /** Its shell exited: the drawer closes it. */
  onExit: () => void;
  /** Opens a workspace file the shell printed (a ⌘-click on its path). */
  onOpenFile: (path: string, line?: number, column?: number) => void;
}) {
  const { prefs, theme } = useApp();
  const host = useRef<HTMLDivElement>(null);
  const term = useRef<Terminal | null>(null);
  const fit = useRef<FitAddon | null>(null);
  const search = useRef<SearchAddon | null>(null);
  const [finding, setFinding] = useState(false);
  const [query, setQuery] = useState("");
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const snack = useSnackbar();
  const id = entry.id;
  // The latest callbacks, for xterm's handlers made once.
  const calls = useRef({ onExit, onOpenFile, setFinding });
  calls.current = { onExit, onOpenFile, setFinding };

  // The terminal, its addons and its stream: made once a tab.
  useEffect(() => {
    const el = host.current;
    if (!el) return;
    const css = getComputedStyle(document.documentElement);
    const x = new Terminal({
      fontFamily: fontFamily(prefs.terminal_font),
      fontSize: prefs.terminal_font_size,
      theme: termColors((n) => css.getPropertyValue(n), theme === "dark"),
      cursorBlink: true,
      scrollback: 10000,
      allowProposedApi: true, // the unicode11 addon's
      macOptionIsMeta: false,
    });
    term.current = x;
    const fitter = new FitAddon();
    fit.current = fitter;
    const finder = new SearchAddon();
    search.current = finder;
    x.loadAddon(fitter);
    x.loadAddon(finder);
    x.loadAddon(new Unicode11Addon());
    x.unicode.activeVersion = "11";
    x.loadAddon(new WebLinksAddon((_e, url) => void openURL(url)));
    // ⌘-click (Ctrl-click) on a path the shell printed opens the file (TRM-21).
    const links = x.registerLinkProvider({
      provideLinks: (y, done) => {
        const line = x.buffer.active.getLine(y - 1)?.translateToString(true) ?? "";
        const found = pathLinks(line);
        if (!found.length) return done(undefined);
        done(
          found.map((l) => ({
            range: { start: { x: l.start + 1, y }, end: { x: l.end, y } },
            text: line.slice(l.start, l.end),
            decorations: { underline: true, pointerCursor: true },
            activate: (e: MouseEvent) => {
              if (mac ? e.metaKey : e.ctrlKey) void openPrinted(workspace, entry.dir, l.path, (p) => calls.current.onOpenFile(p, l.line, l.column));
            },
          })),
        );
      },
    });
    // Copy, paste and find on the platform's keys (TRM-24); the rest is the shell's.
    x.attachCustomKeyEventHandler((e) => {
      if (isToggleTerminalKey(e)) return false; // ⌃` is the drawer's (the window's handler), not the shell's
      const k = termKey(e, mac);
      if (k === "copy") {
        if (x.hasSelection()) void navigator.clipboard?.writeText(x.getSelection());
        return !x.hasSelection() && !mac; // Ctrl+Shift+C with nothing selected does nothing either
      }
      if (k === "paste") {
        void navigator.clipboard?.readText().then((text) => x.paste(text), () => {});
        return false;
      }
      if (k === "find") {
        calls.current.setFinding(true);
        return false;
      }
      return true;
    });

    let disposed = false;
    const ready = (document.fonts?.load(`${prefs.terminal_font_size}px "${bundledFont}"`) ?? Promise.resolve()).catch(() => {});
    void ready.then(() => {
      if (disposed) return;
      x.open(el);
      try {
        const gl = new WebglAddon();
        gl.onContextLoss(() => gl.dispose());
        x.loadAddon(gl);
      } catch {
        // no WebGL here: xterm's own renderer
      }
      if (shown(el)) {
        fitter.fit();
        x.focus(); // a new terminal takes the keys
      }
    });

    // Keys and pastes, in order (TRM-13).
    const enc = new TextEncoder();
    let sending = Promise.resolve();
    const send = (data: Uint8Array) => {
      sending = sending.then(() => terminals.writeTerminal({ workspace, id, data }).then(() => {}, () => {}));
    };
    const input = x.onData((d) => send(enc.encode(d)));
    const binary = x.onBinary((d) => send(Uint8Array.from(d, (c) => c.charCodeAt(0) & 0xff)));
    // Its size, a moment after it settles (TRM-11).
    let sizing: ReturnType<typeof setTimeout> | undefined;
    const resized = x.onResize(({ cols, rows }) => {
      clearTimeout(sizing);
      sizing = setTimeout(() => void terminals.resizeTerminal({ workspace, id, cols, rows }).catch(() => {}), 60);
    });
    const observer = new ResizeObserver(() => {
      if (shown(el) && x.element) fitter.fit();
    });
    observer.observe(el);

    // Its output, from the scrollback on (TRM-12, TRM-26).
    const stop = new AbortController();
    void (async () => {
      await ready;
      try {
        for await (const m of terminals.watchTerminal({ workspace, id, from: 0n }, { signal: stop.signal })) {
          if (m.skipped > 0n) x.write("\x1b[2m…\x1b[0m\r\n");
          if (m.data.length) x.write(m.data);
          if (m.exited) calls.current.onExit();
        }
      } catch (e) {
        if (!stop.signal.aborted) snack(message(e));
      }
    })();

    return () => {
      disposed = true;
      stop.abort();
      clearTimeout(sizing);
      observer.disconnect();
      input.dispose();
      binary.dispose();
      resized.dispose();
      links.dispose();
      x.dispose();
      term.current = null;
    };
    // Made once a terminal: settings and colours change it in place below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workspace, id]);

  // The window's colours and the terminal's font follow the settings (TRM-23, TRM-25).
  useEffect(() => {
    const x = term.current;
    if (!x) return;
    const css = getComputedStyle(document.documentElement);
    x.options.theme = termColors((n) => css.getPropertyValue(n), theme === "dark");
    x.options.fontFamily = fontFamily(prefs.terminal_font);
    x.options.fontSize = prefs.terminal_font_size;
    if (shown(host.current) && x.element) fit.current?.fit();
  }, [theme, prefs.terminal_font, prefs.terminal_font_size, prefs.light_theme, prefs.dark_theme]);

  // Shown again: fill the tab and take the keys.
  useEffect(() => {
    if (hidden) return;
    const x = term.current;
    requestAnimationFrame(() => {
      if (x?.element && shown(host.current)) {
        fit.current?.fit();
        x.focus();
      }
    });
  }, [hidden]);

  const find = (back: boolean) => {
    if (!query) return;
    if (back) search.current?.findPrevious(query);
    else search.current?.findNext(query);
  };
  const paste = () => void navigator.clipboard?.readText().then((text) => term.current?.paste(text), () => {});

  return (
    <div className="terminal-view" hidden={hidden} aria-label={t("desktop.terminal.label", { name })}>
      {finding && (
        <div className="find-bar" role="search">
          <Icon path={mdiMagnify} size="sm" />
          <input
            autoFocus
            value={query}
            aria-label={t("desktop.files.find")}
            placeholder={t("desktop.files.find")}
            spellCheck={false}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                find(e.shiftKey);
              } else if (e.key === "Escape") {
                e.preventDefault();
                setFinding(false);
                search.current?.clearDecorations();
                term.current?.focus();
              }
            }}
          />
          <IconButton icon={mdiChevronUp} label={t("desktop.files.find_previous")} small disabled={!query} onClick={() => find(true)} />
          <IconButton icon={mdiChevronDown} label={t("desktop.files.find_next")} small disabled={!query} onClick={() => find(false)} />
          <IconButton
            icon={mdiClose}
            label={t("desktop.close")}
            small
            onClick={() => {
              setFinding(false);
              search.current?.clearDecorations();
              term.current?.focus();
            }}
          />
        </div>
      )}
      <div
        className="terminal-host"
        ref={host}
        onContextMenu={(e) => {
          e.preventDefault();
          setMenu({ x: e.clientX, y: e.clientY });
        }}
      />
      {menu && (
        <ContextMenu
          x={menu.x}
          y={menu.y}
          onClose={() => setMenu(null)}
          items={[
            { label: t("desktop.terminal.copy"), disabled: !term.current?.hasSelection(), onSelect: () => void navigator.clipboard?.writeText(term.current?.getSelection() ?? "") },
            { label: t("desktop.terminal.paste"), onSelect: paste },
            "divider",
            { label: t("desktop.terminal.clear"), onSelect: () => term.current?.clear() },
            { label: t("desktop.terminal.find"), icon: mdiMagnify, onSelect: () => setFinding(true) },
          ]}
        />
      )}
    </div>
  );
}

/**
 * Opens a path the shell printed, if it names a file: from the terminal's
 * folder, else from the workspace.
 */
async function openPrinted(workspace: string, dir: string, path: string, open: (path: string) => void) {
  const tries = [...new Set([dir ? `${dir}/${path}` : path, path])].filter((p) => !p.startsWith("/") && !p.split("/").includes(".."));
  try {
    const { versions } = await files.statFiles({ workspace, paths: tries });
    const hit = tries.find((p) => versions[p]);
    if (hit) open(hit);
  } catch {
    // the service is away: nothing to open
  }
}
