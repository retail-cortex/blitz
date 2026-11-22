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

import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { GitOperation } from "../gen/blitz/v1/workspace_pb";
import { fakeParam } from "./fakeHelpers";

// A long document the search finds a passage deep in, for the preview's
// marks (files/find.ts).
const fakePricing = [
  "# Pricing",
  "",
  "How prices, discounts and taxes work, for support staff. Totals are rounded once, at the end.",
  "",
  ...Array.from({ length: 24 }, (_, i) => [`## Discount rule ${i + 1}`, "", `A coupon of kind ${i + 1} takes its percentage off the items it names, never off shipping.`, ""]).flat(),
  "## Rounding",
  "",
  "The cart total is rounded half to even, per line.",
  "",
  "## Taxes",
  "",
  "Taxes are added after discounts.",
  "",
].join("\n");
const pricingRounding = fakePricing.split("\n").indexOf("## Rounding") + 1;

/** What the fake workspace's search finds (SearchWorkspace), by source. */
export const fakeSearchCorpus = [
  { source: "files", ref: "internal/cart/cart.go", title: "internal/cart/cart.go", line: 42, section: "", snippet: "// Total rounds each line to cents before summing.\nfunc (c *Cart) Total() Money {\n\treturn c.sum(math.Round)", score: 0.9, summary: "The shopping cart: lines, totals and rounding to cents.", tags: ["checkout", "money", "rounding"] },
  { source: "files", ref: "internal/cart/cart_test.go", title: "internal/cart/cart_test.go", line: 18, section: "", snippet: "func TestCartTotalRounds(t *testing.T) {\n\tc := New()\n\tc.Add(Item{Price: 0.105, Qty: 3})", score: 0.7 },
  { source: "files", ref: "docs/pricing.md", title: "docs/pricing.md", line: pricingRounding, section: "", snippet: "## Rounding\n\nThe cart total is rounded half to even, per line.", score: 0.6, summary: "How prices, discounts and rounding work, for support staff.", tags: ["pricing", "discounts", "rounding"] },
  { source: "documents", ref: "specs/payments.pdf", title: "specs/payments.pdf", line: 88, section: "page 4", snippet: "Totals must be rounded per line item, not on the sum,\nto match the invoice.", score: 0.5 },
  { source: "chats", ref: "session-2", title: "Fix the cart rounding bug", line: 3, section: "", snippet: "The cart total is off by a cent when three items are rounded.", score: 0.4 },
  { source: "notes", ref: ".blitz/plans/session-2-1.md", title: ".blitz/plans/session-2-1.md", line: 1, section: "", snippet: "Plan: round each cart line before summing; add a test.", score: 0.3 },
];

// A sales table for the table viewer (ReadTable reads it).
const fakeSales = ["region,product,units,price,date", ...Array.from({ length: 240 }, (_, i) => `${["north", "south", "east", "west"][i % 4]},${["coupon", "cart", "gift card"][i % 3]},${(i * 7) % 50},${((i * 13) % 997) / 10},2026-0${1 + (i % 9)}-1${i % 10}`)].join("\n") + "\n";

/** In-memory files of the fake workspace. */
export const fakeFiles = new Map<string, string>([
  ["go.mod", "module example.com/shop\n\ngo 1.27\n"],
  ["data/sales.csv", fakeSales],
  ["docs/pricing.md", fakePricing],
  // The project's specification, as /spec writes it (Studio › Specs).
  [".agents/specs/README.md", "# Specification\n\nA small shop server for one store. Kind: software.\n\n1. [Overview](001-overview.md): what the shop is for.\n2. [Orders](002-orders.md): taking and filling orders.\n"],
  [".agents/specs/001-overview.md", "# 001. Overview\n\nStatus: approved\nKind: software\n\n## Context\n\nA small shop server for **one store**: its staff take orders at the counter and online.\n\n## Requirements\n\n- **OV-01** One store, with up to 3 tills open at once.\n- **OV-02** Staff sign in before they take an order.\n\n## Done when\n\nA till takes an order from start to payment.\n"],
  [".agents/specs/002-orders.md", "# 002. Orders\n\nStatus: draft\nDepends on: 001-overview\n\n## Requirements\n\n- **ORD-01** Checkout is fast.\n- **ORD-02** Online orders wait in a queue until someone fills them.\n\n## Done when\n\nAn order is paid and filled.\n\n## Open questions\n\n- Do we take cash?\n"],
  // A page with its own stylesheet and script, previewed in a sandboxed frame.
  ["site/index.html", '<!doctype html>\n<html>\n<head>\n  <title>Shop</title>\n  <link rel="stylesheet" href="site.css">\n</head>\n<body>\n  <h1>The shop</h1>\n  <p id="when">Loading…</p>\n  <script src="app.js"></script>\n</body>\n</html>\n'],
  ["site/site.css", "body { font-family: Georgia, serif; margin: 40px; color: #234; }\nh1 { color: #b04a1a; border-bottom: 2px solid #b04a1a; }\n"],
  ["site/app.js", 'document.getElementById("when").textContent = "Script ran: " + (2 + 2);\n'],
  [
    "README.md",
    "# Shop\n\nA small shop server. Run it with `go run ./cmd/shop`. See [the design](docs/design.md#requests), [main.go](cmd/shop/main.go#L10), [the cart package](internal/cart/) and [Usage](#usage).\n\n## Usage\n\n| Path | What |\n| --- | --- |\n| `/cart` | the cart |\n\n- [x] discounts\n- [ ] checkout\n\n```mermaid\nflowchart LR\n  client[Browser] --> cart[\"/cart handler\"] --> store[(SQLite)]\n```\n",
  ],
  ["docs/design.md", "# Design\n\nBack to [the README](../README.md).\n\n## Requests\n\n```mermaid\nsequenceDiagram\n  Browser->>Shop: POST /cart\n  Shop->>Store: ListItems\n  Store-->>Shop: items\n  Shop-->>Browser: total\n```\n\n> Coupons apply once, rounded in cents.\n"],
  ["docs/logo.svg", '<svg xmlns="http://www.w3.org/2000/svg" width="120" height="120"><circle cx="60" cy="60" r="50" fill="#1a73e8"/></svg>\n'],
  ["docs/shot.png", "\u0000png"],
  ["docs/spec.pdf", "\u0000pdf"],
  ["docs/overview.wav", "\u0000wav"],
  ["cmd/shop/main.go", 'package main\n\nimport (\n\t"log"\n\t"net/http"\n\n\t"example.com/shop/internal/cart"\n)\n\nfunc main() {\n\thttp.HandleFunc("/cart", cart.Handler)\n\tlog.Fatal(http.ListenAndServe(":8080", nil))\n}\n'],
  [
    "internal/cart/discount.go",
    "package cart\n\n// ApplyCoupon returns the total of items with the coupon's percentage off,\n// rounded once, in cents.\nfunc ApplyCoupon(items []Item, c Coupon) int {\n\ttotal := 0\n\tfor _, it := range items {\n\t\ttotal += it.Cents\n\t}\n\treturn round(total * (100 - c.Percent) / 100)\n}\n\nfunc round(cents int) int {\n\tif cents < 0 {\n\t\treturn 0\n\t}\n\treturn cents\n}\n",
  ],
  ["internal/cart/discount_test.go", "package cart\n\nimport \"testing\"\n\nfunc TestApplyCouponRounding(t *testing.T) {\n}\n"],
  ["internal/cart/cart.go", "package cart\n\ntype Item struct {\n\tName  string\n\tCents int\n}\n\ntype Coupon struct{ Percent int }\n"],
  ["internal/store/queries.sql", "-- name: ListItems :many\nSELECT id, name, cents FROM items ORDER BY name;\n"],
  ["web/app.ts", "export function total(items: { cents: number }[]): number {\n  return items.reduce((a, b) => a + b.cents, 0);\n}\n"],
  [".env", "STRIPE_KEY=sk_test_fake\n"],
  [".gitignore", "bin/\n*.log\n"],
  ["AGENTS.md", "# Shop\n\nRun `go test ./...` before committing.\n"],
  ["bin/shop", "\u0000binary"],
]);

/** Git status per workspace-relative file path. */
export const fakeGit: Record<string, string> = { "internal/cart/discount.go": "modified", "internal/cart/discount_test.go": "untracked", "web/app.ts": "added" };

/** Mutable git repository state for the fake workspace. */
export const fakeGitState = {
  staged: false,
  repo: !fakeParam("norepo"),
  branch: "fix/coupon-rounding",
  branches: [
    { name: "fix/coupon-rounding", subject: "Round the total, not each line", upstream: "origin/fix/coupon-rounding" },
    { name: "main", subject: "Release 1.4.0", upstream: "origin/main" },
    { name: "spike/lazy-totals", subject: "Compute totals on read", upstream: "" },
    { name: "origin/feature/gift-cards", subject: "Gift card balance lookup", upstream: "", remote: true },
  ] as { name: string; subject: string; upstream: string; remote?: boolean }[],
  remote: {
    upstream: fakeParam("nopush") ? "" : "origin/fix/coupon-rounding",
    ahead: 1,
    behind: 2,
    operation: GitOperation.UNSPECIFIED,
    conflicts: [] as string[],
    merging: "",
  },
};

/** Returns the fake workspace's git status summary. */
export const fakeGitStatus = () =>
  fakeGitState.repo ? { git: true, repo: true, branch: fakeGitState.branch, changed: 2, remote: true, ...fakeGitState.remote, conflicts: [...fakeGitState.remote.conflicts] } : { git: true, repo: false };

/** What .gitignore ignores in the fake: bin/, *.log, and lines added by Ignore. */
export const fakeIgnored = (p: string) =>
  p.startsWith("bin") || p.endsWith(".log") || (fakeFiles.get(".gitignore") ?? "").split("\n").some((l) => l === `/${p}` || l === `/${p}/`);

const fakeInstructionFiles = ["AGENTS.md", "CLAUDE.md", "GEMINI.md", "BLITZ.md"];

/** Hidden/blocked classification for a path in the fake file tree. */
export const fakeHidden = (p: string) => {
  const name = p.split("/").pop()!;
  return p === ".env" ? "blocked" : name.startsWith(".") ? "dot" : fakeInstructionFiles.includes(name) ? "agent" : fakeIgnored(p) ? "ignored" : "";
};

/** Six-commit sample revision history for a file path. */
export const fakeRevisions = (p: string) => {
  const h = (n: number) => n.toString(16).padStart(2, "0").repeat(20);
  const day = (d: number, hours = 0) => timestampFromDate(new Date(Date.now() - d * 86400000 - hours * 3600000));
  const rows = [
    { n: 6, parents: [5, 4], subject: "Merge branch 'coupon-rounding'", author: "Ana Ruiz", email: "ana@example.com", when: day(0, 2), added: 0, removed: 0 },
    { n: 5, parents: [3], subject: "Read the coupon from the request", author: "Sam Lee", email: "sam@example.com", when: day(0, 5), added: 14, removed: 3 },
    { n: 4, parents: [3], subject: "Round coupon discounts half up", author: "Ana Ruiz", email: "ana@example.com", when: day(1), added: 6, removed: 2 },
    { n: 3, parents: [2], subject: "Add the cart's tests", author: "Kim Park", email: "kim@example.com", when: day(4), added: 120, removed: 0 },
    { n: 2, parents: [1], subject: "Price items in cents", author: "Sam Lee", email: "sam@example.com", when: day(12), added: 40, removed: 38 },
    { n: 1, parents: [], subject: `Add ${p.split("/").pop() || "the shop"}`, author: "Ana Ruiz", email: "ana@example.com", when: day(40), added: 210, removed: 0 },
  ];
  return rows.map((r) => ({ commit: h(r.n), short: h(r.n).slice(0, 7), parents: r.parents.map(h), subject: r.subject, author: r.author, email: r.email, when: r.when, path: p, added: r.added, removed: r.removed, files: r.added + r.removed ? 2 : 0 }));
};

/** Simulated previous revision content for diff previews. */
export const fakeOldText = (text: string) => text.split("\n").slice(0, 3).join("\n") + "\n";
