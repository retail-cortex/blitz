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

import { mkdtempSync, rmSync } from "node:fs";
import { createServer, type Server } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { isWebviewOrigin, startProxy, type Proxy } from "./proxy";

// A stand-in for the service on a Unix socket: it echoes what it got, and
// streams /blitz.v1.Stream in two parts.
let dir: string;
let service: Server;
let proxy: Proxy;
const origin = "vscode-webview://abc";

beforeAll(async () => {
  dir = mkdtempSync(join(tmpdir(), "bp"));
  service = createServer((req, res) => {
    let body = "";
    req.on("data", (c) => (body += c));
    req.on("end", () => {
      if (req.url === "/blitz.v1.Stream") {
        res.writeHead(200, { "Content-Type": "application/connect+json" });
        res.write("first;");
        setTimeout(() => res.end("second"), 20);
        return;
      }
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ url: req.url, body, token: req.headers["x-blitz-token"] ?? null, origin: req.headers.origin ?? null }));
    });
  });
  await new Promise<void>((r) => service.listen(join(dir, "s.sock"), r));
  proxy = await startProxy(join(dir, "s.sock"), "secret");
});
afterAll(async () => {
  await proxy.close();
  await new Promise((r) => service.close(r));
  rmSync(dir, { recursive: true, force: true });
});

const call = (path: string, headers: Record<string, string> = {}, body = "{}") =>
  fetch(proxy.url + path, { method: "POST", headers: { "Content-Type": "application/json", ...headers }, body });

describe("the proxy", () => {
  it("forwards the page's calls with the token, and not the token or origin", async () => {
    const res = await call("/blitz.v1.SessionService/ListSessions", { "x-blitz-token": "secret", Origin: origin }, '{"workspace":"/w"}');
    expect(res.status).toBe(200);
    expect(res.headers.get("access-control-allow-origin")).toBe(origin);
    expect(await res.json()).toEqual({ url: "/blitz.v1.SessionService/ListSessions", body: '{"workspace":"/w"}', token: null, origin: null });
  });
  it.each([
    ["without the token", "/blitz.v1.SessionService/ListSessions", {}, 403],
    ["with another token", "/blitz.v1.SessionService/ListSessions", { "x-blitz-token": "guess" }, 403],
    ["outside the API", "/etc/passwd", { "x-blitz-token": "secret" }, 404],
  ])("refuses a call %s", async (_, path, headers, status) => {
    expect((await call(path, headers)).status).toBe(status);
  });
  it("answers the webview's preflight, and only its", async () => {
    const pre = await fetch(proxy.url + "/blitz.v1.X/Y", { method: "OPTIONS", headers: { Origin: origin, "Access-Control-Request-Headers": "x-blitz-token,content-type" } });
    expect(pre.status).toBe(204);
    expect(pre.headers.get("access-control-allow-headers")).toBe("x-blitz-token,content-type");
    const other = await fetch(proxy.url + "/blitz.v1.X/Y", { method: "OPTIONS", headers: { Origin: "https://evil.example" } });
    expect(other.status).toBe(403);
    expect(other.headers.get("access-control-allow-origin")).toBeNull();
  });
  it("streams responses as they come", async () => {
    const res = await call("/blitz.v1.Stream", { "x-blitz-token": "secret" });
    expect(await res.text()).toBe("first;second");
  });
  it("says so when the service isn't there", async () => {
    const lonely = await startProxy(join(dir, "none.sock"), "t");
    const res = await fetch(lonely.url + "/blitz.v1.X/Y", { method: "POST", headers: { "x-blitz-token": "t" }, body: "{}" });
    expect(res.status).toBe(503);
    expect(((await res.json()) as { code: string }).code).toBe("unavailable");
    await lonely.close();
  });
  it("knows a webview's origin", () => {
    expect(isWebviewOrigin(origin)).toBe(true);
    expect(isWebviewOrigin("http://localhost")).toBe(false);
    expect(isWebviewOrigin(undefined)).toBe(false);
  });
});
