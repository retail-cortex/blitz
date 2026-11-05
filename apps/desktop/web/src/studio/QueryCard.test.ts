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

import { create } from "@bufbuild/protobuf";
import { Window } from "happy-dom";
import { act, createElement } from "react";
import { describe, expect, it, vi } from "vitest";
import { ConnectorKindSchema } from "../gen/blitz/v1/workspace_pb";
import type { ConnectorApi } from "./connectorApi";
import { newConnectorForm } from "./connectorForm";
import { QueryCard } from "./QueryCard";

const postgres = create(ConnectorKindSchema, { id: "postgres", category: "databases", capabilities: ["try", "queries"], tools: [] });

// Try it runs a query for real: one that may write is asked about first
// (N26 desktop-4#9), one that only reads runs at once.
describe("a query's Try it", () => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  it.each([
    { name: "a read runs at once", statement: "SELECT * FROM orders WHERE id = $1", asks: false },
    { name: "a write asks first", statement: "DELETE FROM orders WHERE id = $1", asks: true },
  ])("$name", async ({ statement, asks }) => {
    const g = globalThis as { window?: unknown; document?: unknown };
    const win = new Window();
    g.window ??= win;
    g.document ??= win.document;
    const doc = g.document as Document;
    const { createRoot } = await import("react-dom/client");
    const tryTool = vi.fn(async () => ({ output: "ok", isError: false, problem: "" }));
    const api = { tryTool } as unknown as ConnectorApi;
    const form = { ...newConnectorForm("databases", postgres), name: "shop" };
    const query = { key: "q", name: "orders", description: "", statement, params: [] };
    const host = doc.createElement("div");
    doc.body.appendChild(host);
    const root = createRoot(host);
    act(() => root.render(createElement(QueryCard, { api, form, kind: postgres, prefix: "db", query, onChange: () => {}, onRemove: () => {} })));
    const button = (label: string) => [...doc.querySelectorAll("button")].find((b) => b.textContent?.includes(label));
    await act(async () => button("Try it")?.click());
    expect(tryTool).toHaveBeenCalledTimes(asks ? 0 : 1);
    expect(doc.body.textContent?.includes("Run a statement that may write?")).toBe(asks);
    if (asks) {
      await act(async () => button("Run it")?.click());
      expect(tryTool).toHaveBeenCalledTimes(1);
    }
    act(() => root.unmount());
    host.remove();
  });
});
