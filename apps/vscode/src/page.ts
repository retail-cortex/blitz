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

// The webview's document: the desktop app's page (built into media/page),
// its asset paths turned into webview URIs, with the panel's settings
// before it loads and a content security policy that allows only the
// page's own files and the proxy.
import { randomBytes } from "node:crypto";

/** What the page is told as it loads (the page's host.ts EditorConfig). */
export interface PanelConfig {
  dir: string;
  api: string;
  token: string;
}

/** How the webview names the page's files and what its policy allows. */
export interface Webview {
  /** A path under the page's folder, as the webview loads it. */
  asset(path: string): string;
  /** The webview's own source, for the policy (webview.cspSource). */
  cspSource: string;
}

/** Escapes text for a <script> block's JSON. */
function scriptJSON(v: unknown): string {
  return JSON.stringify(v).replace(/</g, "\\u003c").replace(/>/g, "\\u003e").replace(/&/g, "\\u0026");
}

/** The webview's HTML, from the page's built index.html. */
export function pageHtml(index: string, config: PanelConfig, view: Webview, nonce = randomBytes(16).toString("base64")): string {
  const csp = [
    "default-src 'none'",
    `script-src 'nonce-${nonce}' ${view.cspSource}`,
    `style-src ${view.cspSource} 'unsafe-inline'`,
    `img-src ${view.cspSource} data: blob:`,
    `font-src ${view.cspSource} data:`,
    `worker-src ${view.cspSource} blob:`,
    `connect-src ${config.api}`,
  ].join("; ");
  let html = index.replace(/(src|href)="\.?\/?(assets\/[^"]+)"/g, (_, attr: string, path: string) => `${attr}="${view.asset(path)}"`);
  html = html.replace(/<script /g, `<script nonce="${nonce}" `);
  const head = `<meta http-equiv="Content-Security-Policy" content="${csp}">\n<script nonce="${nonce}">window.blitzEditor = ${scriptJSON(config)};</script>`;
  return html.replace(/<head>/, `<head>\n${head}`);
}
