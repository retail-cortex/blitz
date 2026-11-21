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

import { Code, ConnectError, type ServiceImpl } from "@connectrpc/connect";
import { FileKind, FileService, GitAction, RevisionDiff } from "../gen/blitz/v1/file_pb";
import { ErrorInfoSchema } from "../gen/blitz/v1/turn_pb";
import { fakeVersion, notFound } from "./fakeHelpers";
import { fakeFiles, fakeGit, fakeHidden, fakeIgnored, fakeOldText, fakeRevisions } from "./fakeWorkspaceFiles";

const fakeUploads = new Map<string, string>();
let fakeUploadSeq = 0;
const fakeTrash = new Map<string, Map<string, string>>();
let fakeTrashSeq = 0;

/** Returns the fake ConnectRPC handler implementation for FileService. */
export function fakeFileService(): ServiceImpl<typeof FileService> {
  return {
    listDir: ({ path, showHidden }) => {
      const prefix = path ? path + "/" : "";
      const seen = new Map<string, boolean>();
      for (const p of fakeFiles.keys()) {
        if (!p.startsWith(prefix)) continue;
        const rest = p.slice(prefix.length);
        const name = rest.split("/")[0];
        seen.set(name, seen.get(name) || rest.includes("/"));
      }
      const entries = [...seen].map(([name, folder]) => {
        const p = prefix + name;
        const git = folder ? (Object.keys(fakeGit).some((g) => g.startsWith(p + "/")) ? "changed" : "") : (fakeGit[p] ?? "");
        return { name, path: p, kind: folder ? FileKind.FOLDER : FileKind.FILE, size: BigInt(fakeFiles.get(p)?.length ?? 0), git, hidden: fakeHidden(p), ignored: fakeIgnored(p), agentRule: p === ".env" ? "blocked" : "" };
      });
      entries.sort((a, b) => (a.kind !== b.kind ? (a.kind === FileKind.FOLDER ? -1 : 1) : a.name.localeCompare(b.name)));
      return { entries: showHidden ? entries : entries.filter((e) => !e.hidden), repo: true };
    },
    readPreview: ({ path }) => {
      if (!fakeFiles.has(path) && !path.startsWith(".agents/skills/")) notFound(path); // a skill's references preview too
      if (path.endsWith(".pdf")) {
        const page = (n: number) => `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Contents ${n} 0 R /Resources << /Font << /F1 5 0 R >> >> >>`;
        const text = (s: string) => `<< /Length ${s.length} >>\nstream\n${s}\nendstream`;
        const objs = ["<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>", page(4), text("BT /F1 28 Tf 30 90 Td (Blitz spec, page 1) Tj ET"), "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>", page(7), text("BT /F1 28 Tf 30 90 Td (Page 2) Tj ET")];
        let out = "%PDF-1.4\n";
        const offs: number[] = [];
        objs.forEach((o, i) => {
          offs.push(out.length);
          out += `${i + 1} 0 obj\n${o}\nendobj\n`;
        });
        const x = out.length;
        out += `xref\n0 ${objs.length + 1}\n0000000000 65535 f \n${offs.map((o) => `${String(o).padStart(10, "0")} 00000 n \n`).join("")}trailer\n<< /Size ${objs.length + 1} /Root 1 0 R >>\nstartxref\n${x}\n%%EOF\n`;
        return { path, mime: "application/pdf", data: new TextEncoder().encode(out) };
      }
      if (path.endsWith(".wav")) {
        const rate = 8000;
        const pcm = new Int16Array(rate).map((_, i) => Math.round(8000 * Math.sin((2 * Math.PI * 440 * i) / rate)));
        const head = new DataView(new ArrayBuffer(44));
        const str = (o: number, s: string) => [...s].forEach((c, i) => head.setUint8(o + i, c.charCodeAt(0)));
        str(0, "RIFF");
        head.setUint32(4, 36 + pcm.byteLength, true);
        str(8, "WAVEfmt ");
        head.setUint32(16, 16, true);
        head.setUint16(20, 1, true);
        head.setUint16(22, 1, true);
        head.setUint32(24, rate, true);
        head.setUint32(28, rate * 2, true);
        head.setUint16(32, 2, true);
        head.setUint16(34, 16, true);
        str(36, "data");
        head.setUint32(40, pcm.byteLength, true);
        const data = new Uint8Array(44 + pcm.byteLength);
        data.set(new Uint8Array(head.buffer), 0);
        data.set(new Uint8Array(pcm.buffer), 44);
        return { path, mime: "audio/wav", data };
      }
      const png = Uint8Array.from(atob("iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAABytg0kAAAAFklEQVR4nGP8z8DwnwEIGP8zMDAAAB0IAgBfJnXoAAAAAElFTkSuQmCC"), (c) => c.charCodeAt(0));
      return { path, mime: "image/png", data: png };
    },
    readFile: ({ path }) => {
      const text = fakeFiles.get(path);
      if (text === undefined) notFound(path);
      const binary = text.includes("\u0000");
      return { path, text: binary ? "" : text, version: fakeVersion(text), size: BigInt(text.length), binary, agentRule: path === ".env" ? "blocked" : "" };
    },
    writeFile: ({ path, text, version }) => {
      const nowText = fakeFiles.get(path);
      if ((nowText === undefined ? "" : fakeVersion(nowText)) !== version) {
        const err = new ConnectError(`${path} was changed since it was opened`, Code.FailedPrecondition);
        err.details.push({ desc: ErrorInfoSchema, value: { reason: "FILE_CHANGED", metadata: { current_version: nowText === undefined ? "" : fakeVersion(nowText) } } });
        throw err;
      }
      fakeFiles.set(path, text);
      return { version: fakeVersion(text) };
    },
    writeBinaryFile: ({ path, data, version }) => {
      const nowText = fakeFiles.get(path);
      if ((nowText === undefined ? "" : fakeVersion(nowText)) !== version) throw new ConnectError(`${path} was changed since it was opened`, Code.FailedPrecondition);
      const text = new TextDecoder().decode(data);
      fakeFiles.set(path, text);
      return { version: fakeVersion(text) };
    },
    uploadFile: ({ path, uploadId, data, done, replace }) => {
      const id = uploadId || `upload-${++fakeUploadSeq}`;
      const text = (fakeUploads.get(id) ?? "") + new TextDecoder().decode(data);
      if (!done) {
        fakeUploads.set(id, text);
        return { uploadId: id, version: "" };
      }
      fakeUploads.delete(id);
      if (fakeFiles.has(path) && !replace) throw new ConnectError(`${path}: file already exists`, Code.AlreadyExists);
      fakeFiles.set(path, text);
      return { uploadId: id, version: fakeVersion(text) };
    },
    createFolder: ({ path }) => {
      fakeFiles.set(path + "/.keep", "");
      return {};
    },
    renameFile: ({ from, to }) => {
      for (const [p, v] of [...fakeFiles]) {
        if (p === from || p.startsWith(from + "/")) {
          fakeFiles.delete(p);
          fakeFiles.set(to + p.slice(from.length), v);
        }
      }
      return {};
    },
    gitFileAction: ({ path, action }) => {
      ((window as unknown as { __gitActions?: unknown[] }).__gitActions ??= []).push({ path, action });
      if (action === GitAction.DISCARD) for (const g of Object.keys(fakeGit)) if (g === path || g.startsWith(path + "/")) delete fakeGit[g];
      if (action === GitAction.IGNORE) fakeFiles.set(".gitignore", (fakeFiles.get(".gitignore") ?? "") + `/${path}${fakeFiles.has(path) ? "" : "/"}\n`);
      if (action === GitAction.UNIGNORE) {
        const lines = (fakeFiles.get(".gitignore") ?? "").split("\n");
        const kept = lines.filter((l) => l !== `/${path}` && l !== `/${path}/`);
        if (kept.length === lines.length && fakeIgnored(path)) {
          const [pattern, line] = path.startsWith("bin") ? ["bin/", 1] : ["*.log", 2];
          throw new ConnectError(`${path} is ignored by \`${pattern}\` (.gitignore:${line}), not by a line Blitz added: edit that file to change it.`, Code.FailedPrecondition);
        }
        fakeFiles.set(".gitignore", kept.join("\n"));
      }
      return {};
    },
    getFileHistory: ({ path, skip }) => ({ revisions: skip ? [] : fakeRevisions(path), more: false }),
    getFileRevisionDiff: ({ path, commit, against, otherCommit }) => {
      const file = fakeFiles.has(path) ? path : "internal/cart/discount.go";
      const nowText = fakeFiles.get(file) ?? "";
      if (against === RevisionDiff.NOW && commit === fakeRevisions(path)[0].commit) return { diff: "" };
      const then = fakeOldText(nowText);
      const add = nowText.split("\n").slice(3).filter((l) => l !== "");
      const note = against === RevisionDiff.OTHER ? ` (${commit.slice(0, 7)}..${otherCommit.slice(0, 7)})` : "";
      return { diff: `diff --git a/${file} b/${file}\n--- a/${file}\n+++ b/${file}${note}\n@@ -1,3 +1,${3 + add.length} @@\n${then.trimEnd().split("\n").map((l) => " " + l).join("\n")}\n${add.map((l) => "+" + l).join("\n")}\n`, truncated: false };
    },
    restoreFileRevision: ({ path, commit, preview }) => {
      if (!fakeFiles.has(path)) {
        const under = [...fakeFiles.keys()].filter((p) => p.startsWith(path ? path + "/" : "")).slice(0, 3);
        return { files: under.map((p, i) => ({ path: p, recreated: i === 2 })), version: "" };
      }
      if (preview) return { files: [], version: "" };
      const text = commit === fakeRevisions(path)[0].commit ? (fakeFiles.get(path) ?? "") : fakeOldText(fakeFiles.get(path) ?? "");
      fakeFiles.set(path, text);
      return { version: fakeVersion(text) };
    },
    deleteFile: ({ path, undoable }) => {
      const held = new Map<string, string>();
      for (const [p, v] of [...fakeFiles]) {
        if (p === path || p.startsWith(path + "/")) {
          held.set(p, v);
          fakeFiles.delete(p);
        }
      }
      if (!undoable) return {};
      const restoreId = `trash-${++fakeTrashSeq}`;
      fakeTrash.set(restoreId, held);
      return { restoreId };
    },
    restoreFile: ({ restoreId, path }) => {
      const held = fakeTrash.get(restoreId);
      if (!held) throw new ConnectError("it's no longer in the workspace's trash", Code.NotFound);
      if ([...fakeFiles.keys()].some((p) => p === path || p.startsWith(path + "/"))) throw new ConnectError(`${path}: file already exists`, Code.AlreadyExists);
      for (const [p, v] of held) fakeFiles.set(p, v);
      fakeTrash.delete(restoreId);
      return {};
    },
    copyFile: ({ from, to }) => {
      if ([...fakeFiles.keys()].some((p) => p === to || p.startsWith(to + "/"))) throw new ConnectError(`${to}: file already exists`, Code.AlreadyExists);
      for (const [p, v] of [...fakeFiles]) if (p === from || p.startsWith(from + "/")) fakeFiles.set(to + p.slice(from.length), v);
      return {};
    },
    findFiles: ({ query, folders, limit, extensions }) => {
      const q = query.toLowerCase().replace(/\s/g, "");
      const match = (p: string) => {
        let i = 0;
        for (const c of p.toLowerCase()) if (c === q[i]) i++;
        return i === q.length;
      };
      let all = [...fakeFiles.keys()].filter((p) => !fakeHidden(p) && !p.split("/").some((x) => x.startsWith(".")));
      if (extensions.length > 0) {
        all = all.filter((p) => extensions.some((e) => p.toLowerCase().endsWith("." + e.replace(/^\./, "").toLowerCase())));
        folders = false;
      }
      if (folders) for (const p of [...all]) for (let d = p.slice(0, p.lastIndexOf("/")); d; d = d.slice(0, Math.max(0, d.lastIndexOf("/")))) if (!all.includes(d + "/")) all.push(d + "/");
      return { paths: all.filter(match).sort((a, b) => a.length - b.length).slice(0, limit || 50) };
    },
    readTable: ({ path, search, filters, sortColumn, descending, offset, limit }) => {
      const [head, ...lines] = (fakeFiles.get(path) ?? "").trim().split("\n");
      if (!head) notFound(path);
      const names = head.split(",");
      const intents: Record<string, string> = { units: "units sold", price: "unit price, USD" };
      let rows = lines.map((l, i) => ({ number: i + 1, cells: l.split(",") }));
      const q = search.trim().toLowerCase();
      if (q) rows = rows.filter((r) => r.cells.some((c) => c.toLowerCase().includes(q)));
      for (const f of filters) {
        const m = /^(>=|<=|>|<)\s*(-?[\d.]+)$/.exec(f.expr);
        rows = rows.filter((r) => {
          const v = r.cells[f.column] ?? "";
          if (!m) return v.toLowerCase().includes(f.expr.toLowerCase());
          const n = Number(v), x = Number(m[2]);
          return { ">": n > x, ">=": n >= x, "<": n < x, "<=": n <= x }[m[1] as ">" | ">=" | "<" | "<="];
        });
      }
      if (sortColumn > 0) {
        const c = sortColumn - 1;
        rows = [...rows].sort((a, b) => (Number.isNaN(Number(a.cells[c])) ? a.cells[c].localeCompare(b.cells[c]) : Number(a.cells[c]) - Number(b.cells[c])) * (descending ? -1 : 1));
      }
      const numeric = (i: number) => lines.every((l) => !Number.isNaN(Number(l.split(",")[i])));
      return {
        columns: names.map((n, i) => ({ name: n, kind: n === "date" ? "date" : numeric(i) ? "number" : "text", empty: 0, min: 0, max: 0, intent: intents[n] ?? "" })),
        rows: rows.slice(offset, offset + (limit || 100)),
        matched: rows.length,
        total: lines.length,
        sorted: sortColumn > 0,
        latin1: false,
      };
    },
    renderDocx: ({ markdown, diagrams }) => {
      ((window as unknown as { __diagrams?: unknown[] }).__diagrams ??= []).push(...diagrams.map((d) => ({ source: d.source, bytes: d.png.length, width: d.width, height: d.height })));
      return { data: new TextEncoder().encode("PK fake docx\n" + markdown) };
    },
    statFiles: ({ paths }) => ({ versions: Object.fromEntries(paths.map((p) => [p, fakeFiles.has(p) ? fakeVersion(fakeFiles.get(p)!) : ""])) }),
    async *watchFiles(_r, ctx) {
      yield { folders: [], polling: false };
      await new Promise<void>((done) => (ctx.signal.aborted ? done() : ctx.signal.addEventListener("abort", () => done())));
    },
  };
}
