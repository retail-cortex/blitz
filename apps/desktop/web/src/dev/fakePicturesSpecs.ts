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

import { Code, ConnectError } from "@connectrpc/connect";
import { ErrorInfoSchema } from "../gen/blitz/v1/turn_pb";
import { flowNode } from "./fakeFlows";
import { notFound, now } from "./fakeHelpers";
import { fakeFiles } from "./fakeWorkspaceFiles";

const fakeGeminiRatios = ["1:1", "2:3", "3:2", "3:4", "4:3", "4:5", "5:4", "9:16", "16:9", "21:9", "1:4", "4:1", "1:8", "8:1"];
const fakePictureModels = [
  { ref: "gemini/gemini-nano-banana-2.1", id: "gemini-nano-banana-2.1", name: "Gemini Nano Banana 2.1", kind: "gemini", default: true, organization: false, assumed: false, maxReferences: 14, maxCharacters: 4, maxObjects: 10, maxPictures: 1, sizes: ["1K", "2K", "4K"], ratios: fakeGeminiRatios, thinking: ["MINIMAL", "MEDIUM", "HIGH"], thinkingAlways: false, search: "image", negativePrompt: false, people: false, seed: false, enhance: false, formats: ["image/png", "image/jpeg", "image/webp"], inputPerMtok: 1.5, outputPerMtok: 30, perPicture: 0, sizeTokens: { "1K": 1120, "2K": 1680, "4K": 3767 } },
  { ref: "gemini/gemini-3.1-flash-image", id: "gemini-3.1-flash-image", name: "Gemini 3.1 Flash Image", kind: "gemini", default: false, organization: false, assumed: false, maxReferences: 14, maxCharacters: 4, maxObjects: 10, maxPictures: 1, sizes: ["0.5K", "1K", "2K", "4K"], ratios: fakeGeminiRatios, thinking: ["MINIMAL", "MEDIUM", "HIGH"], thinkingAlways: false, search: "image", negativePrompt: false, people: false, seed: false, enhance: false, formats: ["image/png", "image/jpeg", "image/webp"], inputPerMtok: 0.5, outputPerMtok: 60, perPicture: 0, sizeTokens: { "0.5K": 560, "1K": 1120, "2K": 1680, "4K": 2520 } },
  { ref: "gemini/imagen-4.0-generate-preview", id: "imagen-4.0-generate-preview", name: "Imagen 4 Preview", kind: "imagen", default: false, organization: false, assumed: false, maxReferences: 0, maxCharacters: 0, maxObjects: 0, maxPictures: 4, sizes: ["1K", "2K"], ratios: ["1:1", "3:4", "4:3", "9:16", "16:9"], thinking: [] as string[], thinkingAlways: false, search: "", negativePrompt: true, people: true, seed: true, enhance: true, formats: ["image/png", "image/jpeg"], inputPerMtok: 0, outputPerMtok: 0, perPicture: 0.04, sizeTokens: {} as Record<string, number> },
];
type FakePictureTurn = { id: string; at: ReturnType<typeof now>; model: string; kind: string; prompt: string; references: { file: string; mime: string; name: string; path: string; role: string }[]; count: number; aspectRatio: string; size: string; thinking: string; search: string; avoid: string; people: string; format: string; startFrom: string; pictures: { id: string; mime: string; saved: string[] }[]; text: string; thoughts: string; sources: { title: string; url: string }[]; problem: string; costUsd: number; seed: number; hasSeed: boolean };
const fakePictureSessions: { id: string; name: string; created: ReturnType<typeof now>; updated: ReturnType<typeof now>; turns: FakePictureTurn[] }[] = [];
const fakePictureData = new Map<string, string>();

function fakePictureSVG(prompt: string, ratio: string, variant: number): string {
  const [a, b] = ratio.split(":").map(Number);
  const w = 640;
  const h = Math.round((w * (b || 1)) / (a || 1));
  const skies = [["#1b2a4a", "#f2994a"], ["#0f1c33", "#c0392b"], ["#22313f", "#f5b971"], ["#2c3e50", "#9b59b6"]];
  const [top, bottom] = skies[variant % skies.length];
  const words = prompt.replace(/[<&>]/g, "").slice(0, 60);
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}" viewBox="0 0 ${w} ${h}"><defs><linearGradient id="s" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="${top}"/><stop offset="1" stop-color="${bottom}"/></linearGradient></defs><rect width="${w}" height="${h}" fill="url(#s)"/><circle cx="${w * 0.72}" cy="${h * 0.62}" r="${Math.min(w, h) * 0.08}" fill="#ffd27a" opacity="0.9"/><rect x="0" y="${h * 0.78}" width="${w}" height="${h * 0.22}" fill="#0b1626"/><rect x="${w * 0.28}" y="${h * 0.38}" width="${w * 0.05}" height="${h * 0.4}" fill="#ecf0f1"/><polygon points="${w * 0.27},${h * 0.38} ${w * 0.305},${h * 0.3} ${w * 0.34},${h * 0.38}" fill="#c0392b"/><text x="24" y="40" font-family="sans-serif" font-size="22" fill="#fff" opacity="0.85">${words}</text></svg>`;
}

let fakeSpecCurrent = "";
const fakeApproved = new Map<string, string>();
const fakeVerified = new Map<string, "checked" | "judged">();

function fakeSpecRequirements(path: string) {
  const text = fakeFiles.get(path) ?? "";
  const base = fakeApproved.get(path);
  const how = fakeVerified.get(path);
  const out: { spec: string; id: string; text: string; line: number; change: string; state: string; judged: boolean; reason: string; stale: boolean; evidence: { path: string; line: number; kind: string; text: string }[] }[] = [];
  text.split("\n").forEach((l, i) => {
    const m = /^- \*\*([A-Z]+-\d+)\*\*\s*(.*)$/.exec(l);
    if (!m) return;
    const found = m[1] === "OV-02" || m[1] === "ORD-02";
    const evidence = found ? [{ path: m[1] === "OV-02" ? "internal/auth/signin.go" : "internal/orders/queue.go", line: 14, kind: "code", text: `// ${m[1]}` }, { path: m[1] === "OV-02" ? "internal/auth/signin_test.go" : "internal/orders/queue_test.go", line: 9, kind: "test", text: `// ${m[1]}` }] : [];
    const judged = how === "judged";
    const state = !how ? "" : judged ? (found ? "implemented" : m[1] === "ORD-01" ? "partial" : "missing") : found ? "traced" : "untraced";
    const reason = !judged ? "" : found ? `${evidence[0].path}:14 does it, and its test checks it.` : m[1] === "ORD-01" ? "Checkout is timed in cart.go:88, but nothing keeps it under 30 s." : "Nothing in the work does this yet.";
    out.push({ spec: path.slice(".agents/specs/".length), id: m[1], text: m[2], line: i + 1, change: base !== undefined && !base.includes(l) ? "changed" : "", state, judged, reason, stale: false, evidence: how ? evidence : [] });
  });
  return out;
}

function fakeSpecChecks(text: string) {
  const out: { line: number; id: string; severity: string; code: string; message: string; judged: boolean }[] = [];
  text.split("\n").forEach((l, i) => {
    const m = /\*\*([A-Z]+-\d+)\*\*.*\bfast\b/.exec(l);
    if (m) out.push({ line: i + 1, id: m[1], severity: "warning", code: "vague", message: `${m[1]}: "fast" is a judgement, not a check: say how much, or how it's measured`, judged: false });
  });
  if (!/^#+\s*Done when|^Done when/im.test(text)) out.push({ line: 0, id: "", severity: "warning", code: "no-done-when", message: 'no "Done when": say how anyone can tell it\'s finished', judged: false });
  if (/^#+\s*Open questions\n+- /im.test(text)) out.push({ line: 0, id: "", severity: "info", code: "open-questions", message: "1 open question(s) to answer before it's approved", judged: false });
  return out;
}

function fakeSpecSummary(path: string, text: string) {
  const file = path.slice(".agents/specs/".length);
  const checks = fakeSpecChecks(text);
  return {
    file,
    path,
    number: Number(/^(\d{3})-/.exec(file)?.[1] ?? 0),
    title: (/^#\s+(?:\d+\.\s+)?(.+)$/m.exec(text)?.[1] ?? "").trim(),
    status: (/^Status:\s*(.+)$/m.exec(text)?.[1] ?? "").trim().toLowerCase(),
    dependsOn: [...(/^Depends on:(.*)$/m.exec(text)?.[1] ?? "").matchAll(/\b(\d{3})\b/g)].map((m) => Number(m[1])),
    items: (text.match(/\*\*[A-Z]+-\d+\*\*/g) ?? []).length,
    errors: 0,
    warnings: file === "README.md" ? 0 : checks.filter((c) => c.severity === "warning").length,
    openQuestions: checks.some((c) => c.code === "open-questions") ? 1 : 0,
    approved: fakeApproved.has(path),
    traced: fakeSpecRequirements(path).filter((r) => r.state === "traced" || r.state === "implemented").length,
    implemented: fakeSpecRequirements(path).filter((r) => r.state === "implemented").length,
    changed: fakeSpecRequirements(path).filter((r) => r.change).length,
    done: false,
    next: file === "001-overview.md",
    blockedBy: file === "002-orders.md" ? [1] : [],
  };
}

/** Picture generation, specification ledger, and AI compose RPC handlers for WorkspaceService. */
export function fakePicturesAndSpecsHandlers() {
  return {
    listPictureModels: () => ({ models: fakePictureModels, folder: "pictures" }),
    makePicture: (r: { model: string; size: string; count: number; estimate: boolean; search: boolean; searchImages: boolean; prompt: string; session: string; aspectRatio: string; thinking: string; avoid: string; people: string; format: string; startFrom: string; seed: number; useSeed: boolean; references: { name: string; path: string; role: string }[] }) => {
      const m = fakePictureModels.find((x) => x.ref === r.model) ?? fakePictureModels[0];
      const each = m.kind === "imagen" ? m.perPicture : ((m.sizeTokens[r.size] ?? 1120) * m.outputPerMtok) / 1e6;
      const n = m.kind === "imagen" ? Math.max(r.count, 1) : 1;
      if (r.estimate) return { costUsd: each * n + (r.search ? 0.014 : 0), priced: true };
      if (r.prompt.includes("unsupported")) throw new ConnectError("your organization's service: fast can't draw pictures: choose one of your organization's Gemini image models", Code.FailedPrecondition);
      let s = fakePictureSessions.find((x) => x.id === r.session);
      if (!s) {
        s = { id: `pic-${String(fakePictureSessions.length + 1).padStart(12, "0")}`, name: r.prompt.slice(0, 48), created: now(), updated: now(), turns: [] };
        fakePictureSessions.unshift(s);
      }
      const turnNo = s.turns.length;
      const pictures = Array.from({ length: n }, (_, i) => {
        const id = `p-${String(fakePictureData.size + 1).padStart(12, "0")}`;
        fakePictureData.set(id, fakePictureSVG(r.prompt, r.aspectRatio || "1:1", turnNo + i));
        return { id, mime: "image/svg+xml", saved: [] as string[] };
      });
      const references = r.references.map((ref, i) => {
        const file = `r-${String(fakePictureData.size + i + 1).padStart(12, "0")}.png`;
        fakePictureData.set(file, ref.path ? fakePictureSVG(ref.path, "1:1", 9) : fakePictureSVG(ref.name || "reference", "1:1", 7));
        return { file, mime: "image/svg+xml", name: ref.name || ref.path, path: ref.path, role: ref.role };
      });
      s.turns.push({
        id: `t-${turnNo}`, at: now(), model: m.ref, kind: m.kind, prompt: r.prompt, references, count: n, aspectRatio: r.aspectRatio, size: r.size, thinking: r.thinking,
        search: r.search ? (r.searchImages ? "image" : "web") : "", avoid: r.avoid, people: r.people, format: r.format, startFrom: r.startFrom, pictures,
        text: m.kind === "gemini" ? (turnNo ? "Here it is, changed as you asked." : "Here's a first take.") : "", thoughts: r.thinking ? "Composition first: the subject a third in, the light from the left." : "",
        sources: r.search ? [{ title: "Lighthouses of the Atlantic coast", url: "https://example.com/lighthouses" }] : [], problem: "", costUsd: each * n, seed: r.seed, hasSeed: r.useSeed,
      });
      s.updated = now();
      return { session: s, costUsd: each * n, priced: true };
    },
    listPictureSessions: () => ({ sessions: fakePictureSessions }),
    getPictureSession: ({ id }: { id: string }) => {
      const s = fakePictureSessions.find((x) => x.id === id);
      if (!s) notFound(id);
      return { session: s };
    },
    renamePictureSession: ({ id, name }: { id: string; name: string }) => {
      const s = fakePictureSessions.find((x) => x.id === id);
      if (!s) notFound(id);
      s.name = name;
      return { session: s };
    },
    deletePictureSession: ({ id }: { id: string }) => {
      const i = fakePictureSessions.findIndex((x) => x.id === id);
      if (i >= 0) fakePictureSessions.splice(i, 1);
      return {};
    },
    readPicture: ({ picture }: { picture: string }) => {
      const svg = fakePictureData.get(picture);
      if (!svg) notFound(picture);
      return { data: new TextEncoder().encode(svg), mime: "image/svg+xml" };
    },
    savePicture: ({ session, picture, path }: { session: string; picture: string; path: string }) => {
      const s = fakePictureSessions.find((x) => x.id === session);
      const pic = s?.turns.flatMap((t) => t.pictures).find((p) => p.id === picture);
      if (!s || !pic) notFound(picture);
      const turn = s.turns.find((t) => t.pictures.includes(pic));
      const slug = (turn?.prompt ?? "picture").toLowerCase().replace(/[^a-z0-9]+/g, " ").trim().split(" ").slice(0, 5).join("-") || "picture";
      let out = path && !path.endsWith("/") ? (/\.[a-z]+$/.test(path) ? path : `${path}.svg`) : "";
      for (let n = 1; !out; n++) {
        const p = `${path || "pictures/"}${slug}-${n}.svg`;
        if (!fakeFiles.has(p)) out = p;
      }
      fakeFiles.set(out, fakePictureData.get(picture) ?? "");
      pic.saved.push(out);
      return { path: out };
    },
    deletePicture: ({ session, picture }: { session: string; picture: string }) => {
      const s = fakePictureSessions.find((x) => x.id === session);
      if (!s) notFound(session);
      for (const t of s.turns) t.pictures = t.pictures.filter((p) => p.id !== picture);
      return { session: s };
    },
    listSpecs: () => ({
      specs: [...fakeFiles.keys()]
        .filter((p) => p.startsWith(".agents/specs/") && p.endsWith(".md"))
        .sort((a, b) => (a.endsWith("README.md") ? -1 : b.endsWith("README.md") ? 1 : a.localeCompare(b)))
        .map((path) => fakeSpecSummary(path, fakeFiles.get(path) ?? "")),
    }),
    traceSpecs: ({ path }: { path: string }) => ({ requirements: [...fakeFiles.keys()].filter((p) => p.startsWith(".agents/specs/0") && (!path || p === path)).sort().flatMap((p) => fakeSpecRequirements(p)) }),
    approveSpec: ({ path, force }: { path: string; force: boolean }) => {
      const text = fakeFiles.get(path) ?? "";
      if (!force && /^#+\s*Open questions\n+- /im.test(text)) throw new ConnectError("the spec isn't ready to approve: 1 open question(s)", Code.FailedPrecondition, undefined, [{ desc: ErrorInfoSchema, value: { reason: "SPEC_NOT_READY", domain: "blitz", metadata: {} } }]);
      const approved = text.replace(/^Status:.*$/m, "Status: approved");
      fakeFiles.set(path, approved);
      fakeApproved.set(path, approved);
      return { text: approved, diff: "" };
    },
    verifySpec: ({ path, judge, estimate }: { path: string; judge: boolean; estimate: boolean }) => {
      if (judge && estimate) return { requirements: fakeSpecRequirements(path), costUsd: 0, estimatedUsd: 0.0034, priced: true, problem: "" };
      fakeVerified.set(path, judge ? "judged" : "checked");
      const reqs = fakeSpecRequirements(path);
      const drift = fakeApproved.has(path) ? reqs.filter((r) => r.change || (r.judged && (r.state === "missing" || r.state === "contradicted"))).map((r) => ({ id: r.id, why: r.change ? "changed" : "judged" })) : [];
      return { requirements: reqs, drift, costUsd: judge ? 0.0031 : 0, estimatedUsd: 0, priced: true, problem: "" };
    },
    planSpec: ({ path }: { path: string }) => {
      const pending = fakeSpecRequirements(path).filter((r) => r.state !== "implemented");
      const text = fakeFiles.get(path) ?? "";
      return {
        pending,
        blockedBy: /Depends on: 001/.test(text) && !fakeApproved.has(".agents/specs/001-overview.md") ? [1] : [],
        prompt: pending.length ? `Implement ${path}.\n\nThe requirements to do, in order:\n${pending.map((r) => `- ${r.id}: ${r.text}`).join("\n")}\n\nWork tests first, and cite each requirement's ID where it's done.` : "",
      };
    },
    recordSpecChange: ({ path }: { path: string }) => {
      const changes = fakeSpecRequirements(path).filter((r) => r.change).map((r) => ({ id: r.id, kind: r.change, old: "Checkout is fast.", new: r.text }));
      if (!changes.length) return { changes: [], diff: "", text: "" };
      const text = (fakeFiles.get(path) ?? "").replace(/^Status:.*$/m, "Status: changed");
      fakeFiles.set(path, text);
      return { changes, diff: ".agents/.ledger/diffs/20261107-120000-002-orders.diff", text };
    },
    impactSpec: ({ path, judge, estimate }: { path: string; judge: boolean; estimate: boolean }) => {
      const changes = fakeSpecRequirements(path).filter((r) => r.change).map((r) => ({ id: r.id, kind: r.change, old: "Checkout is fast.", new: r.text }));
      const found = {
        changes,
        symbols: [{ name: "Pay", path: "internal/cart/checkout.go", line: 42, uses: 3 }],
        places: [
          { path: "internal/cart/checkout.go", line: 40, kind: "code", text: "// Pay takes a payment (ORD-01).", why: "ORD-01" },
          { path: "cmd/shop/main.go", line: 18, kind: "code", text: "cart.Pay(order)", why: "Pay" },
          { path: "internal/cart/checkout_test.go", line: 12, kind: "test", text: "_ = Pay(order)", why: "Pay" },
        ],
        summary: "",
        steps: [] as { title: string; detail: string; files: string[] }[],
        conflicts: [] as { id: string; why: string }[],
        report: "",
        costUsd: 0,
        estimatedUsd: judge && estimate ? 0.0052 : 0,
        priced: true,
        problem: "",
      };
      if (!judge || estimate) return found;
      return {
        ...found,
        summary: "Checkout now has a time limit: Pay must finish under 30 seconds, and nothing checks that today.",
        steps: [
          { title: "Test the limit first", detail: "Add a case to TestPay that fails when Pay takes 30 s or more.", files: ["internal/cart/checkout_test.go"] },
          { title: "Time out the payment", detail: "Give Pay a 30 s context deadline and return a clear error when it passes.", files: ["internal/cart/checkout.go"] },
          { title: "Say so at the till", detail: "Show the timeout's message where main calls Pay.", files: ["cmd/shop/main.go"] },
        ],
        conflicts: [{ id: "OV-01", why: "Three tills paying at once share one card terminal; 30 s each may not hold at peak." }],
        report: ".agents/.ledger/impact/20261107-120000-002-orders.md",
        costUsd: 0.0049,
      };
    },
    reviewSpec: ({ path, text, judge }: { path: string; text: string; judge: boolean }) => {
      const findings = fakeSpecChecks(text);
      if (judge) findings.push({ line: 0, id: "", severity: "warning", code: "judged", message: "Nothing says what happens to an order when payment fails: is it kept, and for how long?", judged: true });
      if (!path.startsWith(".agents/specs/")) throw new ConnectError("not a spec", Code.InvalidArgument);
      return { findings, costUsd: judge ? 0.0021 : 0, problem: "" };
    },
    startCompose: ({ request, kind, current }: { request: string; kind: string; current: string }) =>
      kind === "spec"
        ? ((fakeSpecCurrent = current), { session: { id: "compose-fake-spec", kind, asking: true, costUsd: 0.0009, turns: [{ role: "user", text: request }, { role: "composer", text: "How long may a refund be asked for?", options: ["30 days", "14 days", "Any time"] }] } })
        : kind === "job"
        ? { session: { id: "compose-fake-job", kind, asking: true, costUsd: 0.0011, turns: [{ role: "user", text: request }, { role: "composer", text: "Which time zone should 8 am be in?", options: ["America/Chicago", "Europe/Paris", "The service machine's"] }] } }
        : { session: { id: "compose-fake", kind: "workflow", asking: true, costUsd: 0.0012, turns: [{ role: "user", text: request }, { role: "composer", text: "Where are the notes kept?", options: ["notes/today.md", "A folder of notes", "In the chat"] }] } },
    answerCompose: ({ answer, id }: { answer: string; id: string }) => {
      if (id === "compose-fake-spec") {
        const file = fakeSpecCurrent.replace("- **ORD-01** Checkout is fast.", "- **ORD-01** Checkout takes under 30 seconds at a till.").replace("\n## Done when", `- **ORD-03** Staff refund a card payment within ${answer}; the till prints a receipt.\n\n## Done when`);
        return { session: { id, kind: "spec", asking: false, costUsd: 0.0026, turns: [{ role: "user", text: "Add refunds" }, { role: "composer", text: "How long may a refund be asked for?", options: [] }, { role: "user", text: answer }, { role: "composer", text: "Made checkout measurable and added refunds as ORD-03." }], draft: { name: "002-orders", summary: "Made checkout measurable and added refunds as ORD-03.", file, problems: [] } } };
      }
      if (id === "compose-fake-job") {
        const worker = { name: "ai-news", description: "A summary of the day's AI news", schedule: "0 8 * * *", timezone: answer.includes("/") ? answer : "", agent: "", model: "", permissions: ["web:news.google.com", "write:reports/ai-news/**"], maxTurns: 0, maxCostUsd: 0, timeout: "", catchUp: "", prompt: "Fetch the latest AI news from news.google.com.\nWrite a one-page summary to reports/ai-news/MM-DD-YYYY.html, glass morphic with a dark theme.", workflow: "" };
        return {
          session: {
            id: "compose-fake-job",
            kind: "job",
            asking: false,
            costUsd: 0.0024,
            turns: [{ role: "user", text: "AI news every morning" }, { role: "composer", text: "Which time zone should 8 am be in?", options: [] }, { role: "user", text: answer }, { role: "composer", text: "Every day at 8:00, fetches the AI news and writes a summary page." }],
            draft: { name: "ai-news", summary: "Every day at 8:00, fetches the AI news and writes a summary page.", file: "", problems: [], worker },
          },
        };
      }
      const file = "description: A daily brief from the notes\nnodes:\n  - {id: notes, type: read_file, title: Read the notes, path: notes/today.md, position: {x: 0, y: 80}}\n  - {id: draft, type: agent, title: Draft the brief, instructions: Write a short brief for the team., position: {x: 280, y: 80}}\n  - {id: save, type: save_file, title: Save it, path: briefs/today.md, position: {x: 560, y: 80}}\nedges:\n  - {from: notes, to: draft}\n  - {from: draft, to: save}\n";
      return {
        session: {
          id: "compose-fake",
          kind: "workflow",
          asking: false,
          costUsd: 0.0031,
          turns: [{ role: "user", text: "A daily brief" }, { role: "composer", text: "Where are the notes kept?", options: [] }, { role: "user", text: answer }, { role: "composer", text: "Reads today's notes, drafts a short brief and saves it in briefs/." }],
          draft: {
            name: "daily-brief",
            summary: "Reads today's notes, drafts a short brief and saves it in briefs/.",
            file,
            problems: [],
            workflow: {
              name: "daily-brief",
              description: "A daily brief from the notes",
              nodes: [
                flowNode("notes", "read_file", "Read the notes", 0, 80, { path: "notes/today.md" }),
                flowNode("draft", "agent", "Draft the brief", 280, 80, { instructions: "Write a short brief for the team." }),
                flowNode("save", "save_file", "Save it", 560, 80, { path: "briefs/today.md" }),
              ],
              edges: [
                { from: "notes", to: "draft" },
                { from: "draft", to: "save" },
              ],
            },
          },
        },
      };
    },
    dropCompose: () => ({}),
  };
}
