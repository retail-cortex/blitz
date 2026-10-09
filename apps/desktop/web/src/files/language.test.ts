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


import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect";
import { Text } from "@codemirror/state";
import { afterEach, describe, expect, it, vi } from "vitest";
import { setTransport } from "../api";
import { ErrorInfoSchema } from "../gen/blitz/v1/turn_pb";
import { CompletionItemSchema, LanguageService, ServerState } from "../gen/blitz/v1/language_pb";
import { create } from "@bufbuild/protobuf";
import { completionOf, diagnosticsOf, LanguageSession, offsetOf, positionOf, snippetTemplate } from "./language";

describe("positions", () => {
  const doc = Text.of(["package main", "", "func Foo() {}"]);
  it.each([
    [1, 1, 0],
    [3, 6, 19],
    [3, 99, 27], // past the line's end: its end
    [9, 1, 14], // past the last line: the last line
    [0, 0, 0],
  ])("line %i column %i is offset %i", (line, column, offset) => {
    expect(offsetOf(doc, line, column)).toBe(offset);
  });
  it("goes back from offsets", () => {
    expect(positionOf(doc, 19)).toEqual({ line: 3, column: 6 });
  });
  it("makes diagnostics, never backwards", () => {
    const d = diagnosticsOf(doc, [
      { from: { line: 3, column: 6 }, to: { line: 3, column: 9 }, severity: "error", message: "unused", source: "compiler" },
      { from: { line: 3, column: 9 }, to: { line: 1, column: 1 }, severity: "hint", message: "odd", source: "" },
    ]);
    expect(d).toEqual([
      { from: 19, to: 22, severity: "error", message: "unused", source: "compiler" },
      { from: 22, to: 22, severity: "hint", message: "odd", source: undefined },
    ]);
  });
});

describe("completions", () => {
  it.each([
    ["Println($1)", "Println(${1})"],
    ["${1:name} := $0", "${1:name} := ${0}"],
    ["plain", "plain"],
  ])("LSP's snippet %s is CodeMirror's %s", (lsp, cm) => {
    expect(snippetTemplate(lsp)).toBe(cm);
  });
  it("ranks the server's first, in its order, with CodeMirror's types", () => {
    const items = ["Foo", "Bar", "Baz"].map((label, i) => create(CompletionItemSchema, { label, kind: ["function", "struct", "made up"][i], detail: i === 0 ? "func()" : "" }));
    const out = items.map((it, i) => completionOf(it, i, items.length));
    expect(out.map((c) => c.boost)).toEqual([99, 66, 34]);
    expect(out.map((c) => c.type)).toEqual(["function", "class", undefined]);
    expect(out[0].detail).toBe("func()");
    expect(out[1].detail).toBeUndefined();
    expect(out[0].info).toBeUndefined();
  });
});

// A fake LanguageService that records what the session sends.
function fakeService() {
  const calls: string[] = [];
  let unknown = false;
  const texts = new Map<string, string>();
  setTransport(
    createRouterTransport(({ service }) => {
      service(LanguageService, {
        openDocument: (r) => {
          calls.push(`open ${r.path} v${r.version}`);
          texts.set("doc-1", r.text);
          return { document: "doc-1", language: "go", state: ServerState.READY };
        },
        changeDocument: (r) => {
          calls.push(`change v${r.version}`);
          if (unknown) {
            unknown = false;
            const info = create(ErrorInfoSchema, { reason: "UNKNOWN_DOCUMENT" });
            throw new ConnectError("gone", Code.NotFound, undefined, [{ desc: ErrorInfoSchema, value: info }]);
          }
          texts.set(r.document, r.text);
          return {};
        },
        closeDocument: (r) => {
          calls.push(`close ${r.document}`);
          return {};
        },
        keepDocuments: () => ({ documents: [] }),
        async *watchDiagnostics() {
          yield { document: "doc-1", version: 2n, diagnostics: [{ range: { start: { line: 1, column: 1 }, end: { line: 1, column: 4 } }, severity: "warning", message: "hm", source: "vet" }] };
        },
      });
    }),
  );
  return { calls, texts, forget: () => (unknown = true) };
}

describe("a language session", () => {
  afterEach(() => vi.useRealTimers());

  it("opens once, sends changes after a pause or before a request, and closes", async () => {
    const f = fakeService();
    const s = new LanguageSession("/ws");
    await s.open("main.go", "package main\n");
    await s.open("main.go", "package main\n"); // again: nothing new
    expect(f.calls).toEqual(["open main.go v1"]);
    expect(s.status("main.go")).toMatchObject({ language: "go", state: ServerState.READY, errors: 0, warnings: 0 });

    vi.useFakeTimers();
    s.change("main.go", "package main // 1\n");
    s.change("main.go", "package main // 2\n");
    expect(f.calls).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(200);
    expect(f.calls).toEqual(["open main.go v1", "change v3"]);
    vi.useRealTimers();

    s.change("main.go", "package main // 3\n");
    expect(await s.ready("main.go")).toEqual({ id: "doc-1", version: 4n });
    expect(f.calls.at(-1)).toBe("change v4");

    s.close("main.go");
    expect(s.status("main.go")).toBeUndefined();
    await vi.waitFor(() => expect(f.calls.at(-1)).toBe("close doc-1"));
  });

  it("opens a document the service forgot again", async () => {
    const f = fakeService();
    const s = new LanguageSession("/ws");
    await s.open("a.go", "package a\n");
    f.forget();
    s.change("a.go", "package a // later\n");
    await s.flush("a.go");
    await vi.waitFor(() => expect(f.calls).toContain("open a.go v1"));
    expect(f.calls.filter((c) => c.startsWith("open"))).toHaveLength(2);
    expect(f.texts.get("doc-1")).toBe("package a // later\n");
    s.close("a.go");
  });

  it("hears its problems, for the text it has", async () => {
    fakeService();
    const s = new LanguageSession("/ws");
    const heard = vi.fn();
    s.subscribe("main.go", heard);
    await s.open("main.go", "package main\n");
    s.change("main.go", "package main // v2\n"); // the problems are for version 2
    await vi.waitFor(() => expect(s.problems("main.go")).toHaveLength(1));
    expect(s.problems("main.go")?.[0]).toMatchObject({ severity: "warning", message: "hm", source: "vet" });
    expect(s.status("main.go")).toMatchObject({ errors: 0, warnings: 1 });
    expect(heard).toHaveBeenCalled();
    s.change("main.go", "package main // v3\n");
    expect(s.problems("main.go")).toBeUndefined(); // older than the text
    s.close("main.go");
  });

  it("goes on without a language service", async () => {
    setTransport(createRouterTransport(() => {}));
    const s = new LanguageSession("/ws");
    await s.open("main.go", "package main\n");
    expect(s.status("main.go")).toBeUndefined();
    expect(await s.ready("main.go")).toBeUndefined();
    s.close("main.go");
  });
});

