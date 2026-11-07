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

import { describe, expect, it } from "vitest";
import { dbOptions, wantsDb, type DbConnection } from "./dbMentions";

const conns: DbConnection[] = [
  {
    name: "shop",
    kind: "postgres",
    description: "Orders",
    queries: [
      { name: "top_customers", description: "Who spent most", params: [] },
      { name: "by_day", description: "One day", params: [{}] },
      { name: "today", description: "Today's orders", params: [] },
    ],
  },
  { name: "shipping", kind: "sqlite", description: "", queries: [] },
  { name: "warehouse", kind: "bigquery", description: "Sales", queries: [] },
];
const mentions = (q: string) => dbOptions(q, conns).map((o) => o.mention);

describe("@db: completions (spec_059_data DAT-40)", () => {
  it("starts once the mention is on its way to db:", () => {
    expect(mentions("")).toEqual([]);
    expect(mentions("src/")).toEqual([]);
    expect(mentions("dx")).toEqual([]);
    expect(mentions("d")).toEqual(["db:shop", "db:shipping", "db:warehouse"]);
    expect(mentions("DB:")).toEqual(["db:shop", "db:shipping", "db:warehouse"]);
    expect(wantsDb("d")).toBe(true);
    expect(wantsDb("db:sh")).toBe(true);
    expect(wantsDb("docs")).toBe(false);
    expect(wantsDb("")).toBe(false);
  });

  it("offers the databases, then a whole one's queries without parameters", () => {
    expect(mentions("db:sh")).toEqual(["db:shop", "db:shipping"]);
    expect(mentions("db:shop")).toEqual(["db:shop", "db:shop/top_customers", "db:shop/today"]);
    expect(dbOptions("db:shop", conns)[0]).toEqual({ mention: "db:shop", query: false, detail: "postgres · Orders" });
    expect(dbOptions("db:shipping", conns)[0].detail).toBe("sqlite");
  });

  it("after a /, that database's queries", () => {
    expect(mentions("db:shop/")).toEqual(["db:shop/top_customers", "db:shop/today"]);
    expect(mentions("db:shop/to")).toEqual(["db:shop/top_customers", "db:shop/today"]);
    expect(mentions("db:shop/tod")).toEqual(["db:shop/today"]);
    // It takes parameters: the menu fills them.
    expect(mentions("db:shop/by")).toEqual([]);
    expect(mentions("db:nope/x")).toEqual([]);
    expect(dbOptions("db:shop/today", conns)[0]).toEqual({ mention: "db:shop/today", query: true, detail: "Today's orders" });
  });
});
