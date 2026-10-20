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
import { describe, expect, it } from "vitest";
import { ConnectorKindSchema, ConnectorSchema } from "../gen/blitz/v1/workspace_pb";
import { can, connectionFromForm, formFromConnection, newConnectorForm, queryForm, readsOnly, sameConnectorForm, statementParams, textIn, withKind } from "./connectorForm";

const postgres = create(ConnectorKindSchema, {
  id: "postgres",
  category: "databases",
  capabilities: ["test", "try", "queries", "schema", "query-builder"],
  canReadOnly: true,
  fields: [{ key: "host", required: true, defaultValue: "localhost" }, { key: "database", required: true }, { key: "password", type: "secret", required: true }],
  tools: [{ id: "execute_sql", on: true }, { id: "list_tables", on: true }, { id: "list_views" }],
});
const sqlite = create(ConnectorKindSchema, { id: "sqlite", category: "databases", capabilities: ["test"], canReadOnly: true, fields: [{ key: "database", required: true }], tools: [{ id: "execute_sql", writes: true, on: true }, { id: "list_tables", on: true }] });
const writable = create(ConnectorKindSchema, { id: "mail", category: "email", canReadOnly: false, tools: [] });

describe("the connector form", () => {
  it("starts read-only, when it can be, with the kind's usual tools", () => {
    expect(newConnectorForm("databases", postgres)).toMatchObject({ category: "databases", kind: "postgres", scope: "workspace", readOnly: true, tools: ["execute_sql", "list_tables"] });
    expect(newConnectorForm("databases", undefined, "global")).toMatchObject({ kind: "", scope: "global", readOnly: false, tools: [] });
    expect(newConnectorForm("email", writable).readOnly).toBe(false);
  });

  it("keeps the shared settings when the kind changes, never a password, and queries only where it takes them", () => {
    const f = { ...newConnectorForm("databases", postgres), settings: { host: "db", database: "shop" }, secretValues: { password: "pw" }, queries: [{ name: "q", description: "d", statement: "SELECT 1", params: [] }] };
    expect(withKind(f, sqlite)).toMatchObject({ kind: "sqlite", settings: { database: "shop" }, secretValues: {}, tools: ["execute_sql", "list_tables"], queries: [] });
    expect(withKind(f, postgres).queries).toHaveLength(1);
  });

  it("reads a connection and writes it back, a typed password apart", () => {
    const c = create(ConnectorSchema, {
      name: "shop",
      category: "databases",
      scope: "global",
      kind: "postgres",
      description: "Orders",
      settings: { host: "db", database: "shop", password: "${DB_SHOP_PASSWORD}" },
      secretsSet: ["password"],
      readOnly: true,
      tools: ["execute_sql"],
      queries: [{ name: "top", description: "Best customers", statement: "SELECT 1", params: [{ name: "n", type: "integer", description: "How many" }] }],
    });
    const f = formFromConnection(c, postgres);
    expect(f.settings).toEqual({ host: "db", database: "shop" });
    expect(f.secretsSet).toEqual(["password"]);
    expect(f.scope).toBe("global");
    const out = connectionFromForm({ ...f, settings: { ...f.settings, host: " db2 ", empty: "" }, secretValues: { password: "new-pw", other: "" } });
    expect(out.connection).toMatchObject({ name: "shop", category: "databases", kind: "postgres", scope: "global", settings: { host: "db2", database: "shop" }, readOnly: true, tools: ["execute_sql"], text: "" });
    expect(out.connection.queries[0]).toEqual({ name: "top", description: "Best customers", statement: "SELECT 1", params: [{ name: "n", type: "integer", description: "How many" }] });
    expect(out.secretValues).toEqual({ password: "new-pw" });
    expect(sameConnectorForm(f, { ...f })).toBe(true);
    expect(sameConnectorForm(f, { ...f, secretValues: { password: "x" } })).toBe(false);
    expect(sameConnectorForm(f, { ...f, readOnly: false })).toBe(false);
    expect(queryForm(c.queries[0])).toEqual(f.queries[0]);
  });

  it("keeps a custom file's text", () => {
    const c = create(ConnectorSchema, { name: "raw", category: "databases", custom: true, text: "kind: source\n" });
    const f = formFromConnection(c, undefined);
    expect(connectionFromForm(f).connection).toMatchObject({ custom: true, text: "kind: source\n" });
  });

  it("picks a descriptor's text in the language, else English", () => {
    const text = { en: "Host", es: "Servidor", fr: "Hôte" };
    expect(textIn(text, "es")).toBe("Servidor");
    expect(textIn(text, "fr-CA")).toBe("Hôte");
    expect(textIn(text, "en-US")).toBe("Host");
    expect(textIn(text, "de")).toBe("Host");
    expect(textIn(undefined, "en")).toBe("");
  });

  it("says what a kind can do", () => {
    expect(can(postgres, "query-builder")).toBe(true);
    expect(can(sqlite, "query-builder")).toBe(false);
    expect(can(undefined, "test")).toBe(false);
  });

  it.each([
    ["SELECT * FROM t", true],
    ["  select\n count(*) from t;", true],
    ["-- totals\nSELECT sum(n) FROM t", true],
    ["(SELECT 1)", true],
    ["WITH gone AS (DELETE FROM t RETURNING *) SELECT * FROM gone", false],
    ["DELETE FROM t", false],
    ["SELECT 1; DROP TABLE t", false],
    ["", false],
  ])("judges %j reads only: %s", (sql, want) => {
    expect(readsOnly(sql)).toBe(want);
  });

  it("finds a statement's parameters", () => {
    expect(statementParams("SELECT * FROM t WHERE a = $1 AND b = $2 OR c = $1")).toEqual(["p1", "p2"]);
    expect(statementParams("SELECT * FROM t WHERE day = @day AND n < @limit")).toEqual(["day", "limit"]);
  });
});
