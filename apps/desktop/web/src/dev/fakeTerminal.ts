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

// The fake service's terminals (spec_062_terminal, ?fake): a shell that
// draws an agnoster-style prompt with powerline glyphs, Nerd Font icons and
// ANSI colours, and echoes what's typed, a line at a time.

import { Code, ConnectError } from "@connectrpc/connect";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";

const enc = new TextEncoder();
const esc = "\x1b[";
const rgb = (bg: string, fg: string) => `${esc}48;2;${bg}m${esc}38;2;${fg}m`;

/** The fake prompt: user, folder and git branch in powerline segments. */
export function fakePrompt(dir: string): string {
  const folder = dir ? `~/shop/${dir}` : "~/shop";
  return (
    `${rgb("97;175;239", "40;44;52")}  me@blitz ` +
    `${rgb("152;195;121", "97;175;239")}${rgb("152;195;121", "40;44;52")}  ${folder} ` +
    `${rgb("229;192;123", "152;195;121")}${rgb("229;192;123", "40;44;52")}  main ` +
    `${esc}49m${esc}38;2;229;192;123m${esc}0m `
  );
}

/** What the fake terminal shows first: a welcome with the 16 colours and a few icons. */
export function fakeBanner(): string {
  let colours = "";
  for (let i = 0; i < 8; i++) colours += `${esc}4${i}m  `;
  colours += `${esc}0m\r\n`;
  for (let i = 0; i < 8; i++) colours += `${esc}10${i}m  `;
  return (
    `${esc}1mBlitz terminal${esc}0m ${esc}2m(fake)${esc}0m\r\n` +
    `${colours}${esc}0m\r\n` +
    `${esc}34m src${esc}0m  ${esc}32m main.go${esc}0m  ${esc}33m package.json${esc}0m  ${esc}35m tool.py${esc}0m  ${esc}36m README.md${esc}0m\r\n\r\n`
  );
}

interface Term {
  id: string;
  dir: string;
  started: Date;
  out: Uint8Array[];
  line: string;
  wake: Set<() => void>;
  closed: boolean;
}

/** The fake TerminalService. */
export function fakeTerminals() {
  const terms = new Map<string, Term>();
  let next = 1;
  const emit = (t: Term, text: string) => {
    t.out.push(enc.encode(text));
    for (const w of t.wake) w();
  };
  const find = (id: string) => {
    const t = terms.get(id);
    if (!t || t.closed) throw new ConnectError(`no terminal ${id}`, Code.NotFound);
    return t;
  };
  const info = (t: Term) => ({ id: t.id, dir: t.dir, shell: "/bin/zsh", started: timestampFromDate(t.started), exited: false, exitCode: 0 });
  return {
    openTerminal: ({ dir }: { dir: string }) => {
      if (dir.startsWith("/") || dir.split("/").includes("..")) throw new ConnectError(`${dir} is outside the workspace`, Code.InvalidArgument);
      const t: Term = { id: `term-${next++}`, dir, started: new Date(), out: [], line: "", wake: new Set(), closed: false };
      terms.set(t.id, t);
      emit(t, fakeBanner() + fakePrompt(dir));
      return { terminal: info(t) };
    },
    async *watchTerminal({ id }: { id: string }, ctx: { signal: AbortSignal }) {
      const t = find(id);
      let sent = 0;
      let offset = 0n;
      while (!ctx.signal.aborted && !t.closed) {
        while (sent < t.out.length) {
          const data = t.out[sent++];
          offset += BigInt(data.length);
          yield { data, offset, skipped: 0n, exited: false, exitCode: 0 };
        }
        if (t.closed || ctx.signal.aborted) break; // closed while its output was read
        await new Promise<void>((resolve) => {
          const done = () => (t.wake.delete(done), resolve());
          t.wake.add(done);
          ctx.signal.addEventListener("abort", done, { once: true });
        });
      }
    },
    // Echoes keys; Enter ends the line and shows the prompt again.
    writeTerminal: ({ id, data }: { id: string; data: Uint8Array }) => {
      const t = find(id);
      const text = new TextDecoder().decode(data);
      for (const ch of text) {
        if (ch === "\r") {
          const said = t.line.trim();
          t.line = "";
          emit(t, `\r\n${said ? `${esc}2m${said}: fake shell, nothing runs${esc}0m\r\n` : ""}${fakePrompt(t.dir)}`);
        } else if (ch === "\x7f") {
          if (t.line) {
            t.line = t.line.slice(0, -1);
            emit(t, "\b \b");
          }
        } else if (ch >= " ") {
          t.line += ch;
          emit(t, ch);
        }
      }
      return {};
    },
    resizeTerminal: ({ id, cols, rows }: { id: string; cols: number; rows: number }) => {
      find(id);
      if (!cols || !rows) throw new ConnectError("a size of 0", Code.InvalidArgument);
      return {};
    },
    listTerminals: () => ({ terminals: [...terms.values()].filter((t) => !t.closed).map(info) }),
    closeTerminal: ({ id }: { id: string }) => {
      const t = find(id);
      t.closed = true;
      for (const w of t.wake) w();
      return {};
    },
  };
}
