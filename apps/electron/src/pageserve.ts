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

// A workspace's pages in a browser (spec_038_electron EL-102): Open in
// Browser and Open in System Browser serve a workspace's files on this
// computer's loopback address, under a random token of the workspace's,
// so a page's scripts run and its relative files (scripts, styles,
// pictures, data) load as they would on a web server. Never a file
// outside the workspace, links included; only GET and HEAD; nothing
// listed. The window's own preview, which runs no scripts, stays the
// default.

import { randomBytes } from "node:crypto";
import { createReadStream } from "node:fs";
import { realpath, stat } from "node:fs/promises";
import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";
import path from "node:path";

/** The types of what pages load, by extension; anything else is application/octet-stream. */
const types: Record<string, string> = {
  ".html": "text/html; charset=utf-8",
  ".htm": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".map": "application/json; charset=utf-8",
  ".txt": "text/plain; charset=utf-8",
  ".md": "text/plain; charset=utf-8",
  ".csv": "text/csv; charset=utf-8",
  ".xml": "application/xml; charset=utf-8",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".gif": "image/gif",
  ".webp": "image/webp",
  ".avif": "image/avif",
  ".ico": "image/x-icon",
  ".woff": "font/woff",
  ".woff2": "font/woff2",
  ".ttf": "font/ttf",
  ".otf": "font/otf",
  ".wasm": "application/wasm",
  ".pdf": "application/pdf",
  ".mp3": "audio/mpeg",
  ".wav": "audio/wav",
  ".mp4": "video/mp4",
  ".webm": "video/webm",
};

/** typeOf is the Content-Type a file is served with. */
export const typeOf = (file: string) => types[path.extname(file).toLowerCase()] ?? "application/octet-stream";

/** isPage is whether a workspace's file is a page Open in Browser opens: HTML. */
export const isPage = (rel: string) => /\.html?$/i.test(rel);

/**
 * inside is the file a request's path names in root, or undefined when it
 * names nothing there: decoded, without . or .. leaving root, a folder
 * meaning its index.html. It doesn't follow links: serve checks where
 * they go.
 */
export function inside(root: string, urlPath: string): string | undefined {
  let rel: string;
  try {
    rel = decodeURIComponent(urlPath);
  } catch {
    return undefined;
  }
  if (rel.includes("\0") || rel.includes("\\")) return undefined;
  const file = path.resolve(root, "." + path.posix.normalize("/" + rel));
  return file === root || file.startsWith(root + path.sep) ? file : undefined;
}

/** pageURL is where rel (a workspace-relative path) is served, under base (the server's address and the workspace's token). */
export const pageURL = (base: string, rel: string) => `${base}/${rel.split("/").map(encodeURIComponent).join("/")}`;

/** Pages serves workspaces' files on 127.0.0.1, each under a token of its own. */
export class Pages {
  private server: Server | undefined;
  private port = 0;
  /** Workspaces by token, and tokens by workspace (their real paths). */
  private roots = new Map<string, string>();
  private tokens = new Map<string, string>();

  /**
   * url is where a workspace's page is served, starting the server the
   * first time. dir must be a folder, rel a page in it.
   */
  async url(dir: string, rel: string): Promise<string> {
    if (!path.isAbsolute(dir)) throw new Error(`not serving ${dir}: not an absolute path`);
    const root = await realpath(dir);
    if (!(await stat(root)).isDirectory()) throw new Error(`not serving ${dir}: not a folder`);
    if (!isPage(rel)) throw new Error(`not opening ${rel}: only pages (.html) open in a browser`);
    const file = inside(root, rel);
    if (!file || !(await this.within(root, file))?.isFile()) throw new Error(`not opening ${rel}: no such page in the workspace`);
    let token = this.tokens.get(root);
    if (!token) {
      token = randomBytes(18).toString("base64url");
      this.tokens.set(root, token);
      this.roots.set(token, root);
    }
    await this.start();
    return pageURL(`http://127.0.0.1:${this.port}/${token}`, rel);
  }

  /** within is file's stat when it is in root once its links are followed. */
  private async within(root: string, file: string) {
    try {
      const real = await realpath(file);
      if (real !== root && !real.startsWith(root + path.sep)) return undefined;
      return await stat(real);
    } catch {
      return undefined;
    }
  }

  private start(): Promise<void> {
    if (this.server) return Promise.resolve();
    const server = createServer((req, res) => void this.serve(req, res));
    this.server = server;
    return new Promise((resolve, reject) => {
      server.once("error", reject);
      server.listen(0, "127.0.0.1", () => {
        this.port = (server.address() as AddressInfo).port;
        resolve();
      });
    });
  }

  /** address is the server's base address (tests), "" before it starts. */
  address = () => (this.port ? `http://127.0.0.1:${this.port}` : "");

  /** close stops the server. */
  close = () => this.server?.close();

  private async serve(req: IncomingMessage, res: ServerResponse) {
    const no = (code: number) => {
      res.writeHead(code, { "Content-Type": "text/plain; charset=utf-8" });
      res.end(code === 405 ? "Only GET and HEAD.\n" : "Not found.\n");
    };
    if (req.method !== "GET" && req.method !== "HEAD") return no(405);
    const url = new URL(req.url ?? "/", "http://127.0.0.1");
    const [, token = "", ...rest] = url.pathname.split("/");
    const root = this.roots.get(token);
    if (!root) return no(404);
    let file = inside(root, rest.join("/"));
    if (!file) return no(404);
    let info = await this.within(root, file);
    if (info?.isDirectory()) {
      file = path.join(file, "index.html");
      info = await this.within(root, file);
    }
    if (!info?.isFile()) return no(404);
    res.writeHead(200, {
      "Content-Type": typeOf(file),
      "Content-Length": info.size,
      "Cache-Control": "no-store",
      "X-Content-Type-Options": "nosniff",
      "Referrer-Policy": "no-referrer",
    });
    if (req.method === "HEAD") return res.end();
    createReadStream(file)
      .on("error", () => res.destroy())
      .pipe(res);
  }
}
