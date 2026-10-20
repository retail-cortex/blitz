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
import { DatabaseConnectionSchema, DatabaseKindSchema } from "../gen/blitz/v1/workspace_pb";
import { connectionFromForm, formFromConnection, newDatabaseForm, readsOnly, sameDatabaseForm, statementParams, withKind } from "./databaseForm";

const postgres = create(DatabaseKindSchema, {
  type: "postgres",
  fields: [{ key: "host", required: true, defaultValue: "localhost" }, { key: "database", required: true }, { key: "password", secret: true, required: true }],
  tools: [{ id: "execute_sql", on: true }, { id: "list_tables", on: true }, { id: "list_views" }],
});
const sqlite = create(DatabaseKindSchema, { type: "sqlite", fields: [{ key: "database", required: true }], tools: [{ id: "execute_sql", writes: true, on: true }, { id: "list_tables", on: true }] });

describe("the database form", () => {
  it("starts read-only with the kind's usual tools", () => {
    expect(newDatabaseForm(postgres)).toMatchObject({ type: "postgres", scope: "workspace", readOnly: true, tools: ["execute_sql", "list_tables"] });
    expect(newDatabaseForm(undefined, "global")).toMatchObject({ type: "postgres", scope: "global", tools: [] });
  });

  it("keeps the shared settings when the kind changes, never a password", () => {
    const f = { ...newDatabaseForm(postgres), settings: { host: "db", database: "shop" }, secretValues: { password: "pw" } };
    expect(withKind(f, sqlite)).toMatchObject({ type: "sqlite", settings: { database: "shop" }, secretValues: {}, tools: ["execute_sql", "list_tables"] });
  });

  it("reads a connection and writes it back, a typed password apart", () => {
    const c = create(DatabaseConnectionSchema, {
      name: "shop",
      scope: "global",
      type: "postgres",
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
    expect(out.connection).toMatchObject({ name: "shop", scope: "global", settings: { host: "db2", database: "shop" }, readOnly: true, tools: ["execute_sql"], text: "" });
    expect(out.connection.queries[0]).toEqual({ name: "top", description: "Best customers", statement: "SELECT 1", params: [{ name: "n", type: "integer", description: "How many" }] });
    expect(out.secretValues).toEqual({ password: "new-pw" });
    expect(sameDatabaseForm(f, { ...f })).toBe(true);
    expect(sameDatabaseForm(f, { ...f, secretValues: { password: "x" } })).toBe(false);
    expect(sameDatabaseForm(f, { ...f, readOnly: false })).toBe(false);
  });

  it("keeps a custom file's YAML", () => {
    const c = create(DatabaseConnectionSchema, { name: "raw", type: "", custom: true, text: "kind: source\n" });
    const f = formFromConnection(c, undefined);
    expect(connectionFromForm(f).connection).toMatchObject({ custom: true, text: "kind: source\n" });
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
