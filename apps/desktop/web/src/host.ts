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

// The page inside an editor (spec_parity_027 PAR-INT-02): the VS Code
// extension shows it in a webview for one workspace, with the service's
// API through its own proxy, and the two talk by messages. The extension
// sets window.blitzEditor before the page loads.

/** What the extension tells the page as it loads. */
export interface EditorConfig {
  /** The workspace the panel is for. */
  dir: string;
  /** The extension's proxy to the service. */
  api: string;
  /** Sent with every API call: the proxy refuses calls without it. */
  token: string;
}

/** Messages the page sends the extension. */
export type ToEditor =
  | { type: "ready" }
  | { type: "open"; path: string; line?: number; column?: number }
  | { type: "diff"; diff: string; title: string }
  | { type: "notify"; title: string; body: string };

/** Messages the extension sends the page. */
export type FromEditor = { type: "compose"; text: string } | { type: "addToContext"; path: string };

type VSCodeAPI = { postMessage(msg: unknown): void };
declare global {
  interface Window {
    blitzEditor?: EditorConfig;
    acquireVsCodeApi?: () => VSCodeAPI;
  }
}

let api: VSCodeAPI | undefined;

/** The editor the page is in, or undefined in the desktop app or a browser. */
export function editorConfig(): EditorConfig | undefined {
  return typeof window === "undefined" ? undefined : window.blitzEditor;
}

/** Sends the extension a message (nothing outside an editor). */
export function toEditor(msg: ToEditor) {
  if (!editorConfig()) return;
  api ??= window.acquireVsCodeApi?.();
  api?.postMessage(msg);
}

/** Calls f with each message from the extension. */
export function onEditorMessage(f: (msg: FromEditor) => void): () => void {
  const listener = (e: MessageEvent) => {
    const m = e.data as FromEditor | undefined;
    if (m && (m.type === "compose" || m.type === "addToContext")) f(m);
  };
  window.addEventListener("message", listener);
  return () => window.removeEventListener("message", listener);
}

/** Whether VS Code's theme is dark (its webviews mark the body). */
export function editorIsDark(): boolean {
  const c = document.body.classList;
  return c.contains("vscode-dark") || c.contains("vscode-high-contrast");
}

/** Calls f whenever VS Code's theme switches between light and dark. */
export function onEditorThemeChange(f: (dark: boolean) => void): () => void {
  const o = new MutationObserver(() => f(editorIsDark()));
  o.observe(document.body, { attributes: true, attributeFilter: ["class"] });
  return () => o.disconnect();
}
