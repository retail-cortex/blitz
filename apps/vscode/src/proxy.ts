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

// The page's way to the service: a webview can't open a Unix socket, so
// the extension forwards its API calls (/blitz.v1.*) from a local port to
// the service's socket, as the desktop app's Go side does. Only calls
// carrying the panel's token pass: any web page can reach a local port,
// but none can read the token.
import { randomBytes } from "node:crypto";
import { createServer, request, type IncomingMessage, type ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";

/** A running proxy. */
export interface Proxy {
  /** Where the page sends its calls (http://127.0.0.1:<port>). */
  url: string;
  /** What each call carries in x-blitz-token. */
  token: string;
  close(): Promise<void>;
}

const apiPrefix = "/blitz.v1.";

/** Whether an origin is one of VS Code's webviews. */
export function isWebviewOrigin(origin: string | undefined): boolean {
  return !!origin && origin.startsWith("vscode-webview://");
}

// CORS for the webview's origin: the page is cross-origin to the proxy.
function allow(req: IncomingMessage, res: ServerResponse) {
  const origin = req.headers.origin;
  if (!isWebviewOrigin(origin)) return;
  res.setHeader("Access-Control-Allow-Origin", origin!);
  res.setHeader("Vary", "Origin");
  res.setHeader("Access-Control-Allow-Methods", "POST, GET, OPTIONS");
  res.setHeader("Access-Control-Allow-Headers", req.headers["access-control-request-headers"] ?? "*");
  res.setHeader("Access-Control-Expose-Headers", "*");
  res.setHeader("Access-Control-Max-Age", "600");
}

/** Starts a proxy from 127.0.0.1 to the service listening on socket. */
export async function startProxy(socket: string, token = randomBytes(24).toString("hex")): Promise<Proxy> {
  const server = createServer((req, res) => {
    allow(req, res);
    if (req.method === "OPTIONS") {
      res.writeHead(isWebviewOrigin(req.headers.origin) ? 204 : 403).end();
      return;
    }
    if (!req.url?.startsWith(apiPrefix) || req.headers["x-blitz-token"] !== token) {
      res.writeHead(req.url?.startsWith(apiPrefix) ? 403 : 404).end();
      return;
    }
    const headers: Record<string, string | string[] | undefined> = { ...req.headers, host: "blitz" };
    delete headers["x-blitz-token"];
    delete headers.origin;
    const out = request({ socketPath: socket, path: req.url, method: req.method, headers }, (up) => {
      res.writeHead(up.statusCode ?? 502, up.headers);
      up.pipe(res); // streamed: turns send their events as they come
    });
    out.on("error", () => {
      if (!res.headersSent) {
        // Connect's JSON error shape, so the page's client reads it.
        res.writeHead(503, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ code: "unavailable", message: "the Blitz service isn't answering" }));
      } else res.destroy();
    });
    req.pipe(out);
  });
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const { port } = server.address() as AddressInfo;
  return {
    url: `http://127.0.0.1:${port}`,
    token,
    close: () =>
      new Promise((resolve) => {
        server.closeAllConnections();
        server.close(() => resolve());
      }),
  };
}
