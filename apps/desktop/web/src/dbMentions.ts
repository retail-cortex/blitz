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

// @db: mentions in the composer (spec_059_data DAT-40): completing a
// database, or one of its saved queries, as it's typed. The service reads
// what they name as the message is sent.

/** What a database offers @db: (a Connector's fields that matter here). */
export interface DbConnection {
  name: string;
  kind: string;
  description: string;
  queries: { name: string; description: string; params: unknown[] }[];
}

/** A completion: the mention's text after @ (db:shop, db:shop/top), what it is, and what it's for. */
export interface DbOption {
  mention: string;
  query: boolean;
  detail: string;
}

/** The most completions offered at once. */
const most = 12;

/**
 * The completions for an @mention's query: none unless it's on its way
 * to db: ("d", "db", "db:…"). Before a "/", the databases whose names
 * start with what's typed (and, once one's name is whole, its queries);
 * after it, that database's queries whose names start with the rest. A
 * query that takes parameters isn't offered: a mention can't fill them
 * (the database menu does).
 */
export function dbOptions(query: string, conns: DbConnection[]): DbOption[] {
  const q = query.toLowerCase();
  if (!q || !(q.startsWith("db:") || "db:".startsWith(q))) return [];
  const rest = q.startsWith("db:") ? query.slice(3) : "";
  const queriesOf = (c: DbConnection, prefix: string) =>
    c.queries.filter((x) => x.params.length === 0 && x.name.toLowerCase().startsWith(prefix.toLowerCase())).map((x) => ({ mention: `db:${c.name}/${x.name}`, query: true, detail: x.description }));
  const slash = rest.indexOf("/");
  if (slash >= 0) {
    const c = conns.find((x) => x.name === rest.slice(0, slash));
    return c ? queriesOf(c, rest.slice(slash + 1)).slice(0, most) : [];
  }
  const out: DbOption[] = [];
  for (const c of conns.filter((x) => x.name.toLowerCase().startsWith(rest.toLowerCase()))) {
    out.push({ mention: `db:${c.name}`, query: false, detail: [c.kind, c.description].filter(Boolean).join(" · ") });
    if (c.name === rest) out.push(...queriesOf(c, ""));
  }
  return out.slice(0, most);
}

/** Whether an @mention's query may be on its way to db: (the databases are worth loading). */
export const wantsDb = (query: string) => {
  const q = query.toLowerCase();
  return q !== "" && (q.startsWith("db:") || "db:".startsWith(q));
};
