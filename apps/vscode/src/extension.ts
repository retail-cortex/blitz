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

// Blitz in VS Code (spec_027_parity PAR-INT-02): the desktop app's
// conversation in a view of its own, attached to the Blitz service like
// the desktop app, for the window's workspace folder. Files the chat
// links to open here, an approval's change shows in the diff view, and
// the selection or a file goes to the chat.
import { readFile } from "node:fs/promises";
import { isAbsolute, join } from "node:path";
import * as vscode from "vscode";
import { inWorkspace, selectionText } from "./compose";
import { pageHtml } from "./page";
import { applyPatch, parsePatch, ProposedTexts } from "./patch";
import { startProxy, type Proxy } from "./proxy";
import { serviceSocket } from "./socket";

/** Messages from the page (its host.ts ToEditor). */
type FromPage =
  | { type: "ready" }
  | { type: "open"; path: string; line?: number; column?: number }
  | { type: "diff"; diff: string; title: string }
  | { type: "notify"; title: string; body: string };

/** Messages to the page (its host.ts FromEditor). */
type ToPage = { type: "compose"; text: string } | { type: "addToContext"; path: string };

const proposedScheme = "blitz-proposed";

/** The proposed versions of files, shown on the diff view's right. */
class Proposed implements vscode.TextDocumentContentProvider {
  readonly texts = new ProposedTexts();
  private n = 0;
  add(path: string, text: string): vscode.Uri {
    const uri = vscode.Uri.from({ scheme: proposedScheme, path: `/${path}`, query: String(++this.n) });
    this.texts.set(uri.toString(), text);
    return uri;
  }
  provideTextDocumentContent(uri: vscode.Uri): string {
    return this.texts.get(uri.toString()) ?? "";
  }
  /** Forgets a proposed version once its document closes. */
  closed(doc: vscode.TextDocument) {
    if (doc.uri.scheme === proposedScheme) this.texts.delete(doc.uri.toString());
  }
}

/** The chat view. */
class ChatView implements vscode.WebviewViewProvider {
  private view?: vscode.WebviewView;
  private proxy?: Promise<Proxy>;
  private ready = false;
  private queue: ToPage[] = [];
  // The workspace the page was opened for (VSC-02): the messages from it
  // are about this folder, whichever file is in front later.
  private bound?: string;

  constructor(
    private readonly ctx: vscode.ExtensionContext,
    private readonly proposed: Proposed,
  ) {}

  /** The workspace for a new page: the folder of the file in front, else the first. */
  private dir(): string | undefined {
    const doc = vscode.window.activeTextEditor?.document.uri;
    const folder = (doc && vscode.workspace.getWorkspaceFolder(doc)) ?? vscode.workspace.workspaceFolders?.[0];
    return folder?.uri.fsPath;
  }

  async resolveWebviewView(view: vscode.WebviewView): Promise<void> {
    this.view = view;
    this.ready = false;
    const pageDir = vscode.Uri.joinPath(this.ctx.extensionUri, "media", "page");
    view.webview.options = { enableScripts: true, localResourceRoots: [pageDir] };
    view.webview.onDidReceiveMessage((m: FromPage) => this.onMessage(m));
    view.onDidDispose(() => (this.view = undefined));
    const dir = this.dir();
    this.bound = dir;
    if (!dir) {
      view.webview.html = notice(vscode.l10n.t("Open a folder: Blitz works in a workspace."));
      return;
    }
    try {
      this.proxy ??= startProxy(serviceSocket(vscode.workspace.getConfiguration("blitz").get("socket", "")));
      const proxy = await this.proxy;
      const index = await readFile(join(pageDir.fsPath, "index.html"), "utf8");
      view.webview.html = pageHtml(index, { dir, api: proxy.url, token: proxy.token }, {
        asset: (p) => view.webview.asWebviewUri(vscode.Uri.joinPath(pageDir, p)).toString(),
        cspSource: view.webview.cspSource,
      });
    } catch (e) {
      this.proxy = undefined; // the next time the view opens tries again
      view.webview.html = notice(vscode.l10n.t("Blitz couldn't start its view: {0}", e instanceof Error ? e.message : String(e)));
    }
  }

  private async onMessage(m: FromPage) {
    const dir = this.bound;
    switch (m.type) {
      case "ready":
        this.ready = true;
        for (const q of this.queue.splice(0)) this.view?.webview.postMessage(q);
        return;
      case "open": {
        if (!dir) return;
        const uri = vscode.Uri.file(isAbsolute(m.path) ? m.path : join(dir, m.path));
        const pos = new vscode.Position(Math.max((m.line ?? 1) - 1, 0), Math.max((m.column ?? 1) - 1, 0));
        await vscode.window.showTextDocument(uri, { selection: new vscode.Range(pos, pos), preview: false });
        return;
      }
      case "diff":
        if (dir) await this.showDiff(dir, m.diff);
        return;
      case "notify":
        vscode.window.showInformationMessage(`${m.title}: ${m.body}`);
        return;
    }
  }

  /** Shows each file an approval would change in the diff view. */
  private async showDiff(dir: string, diff: string) {
    for (const p of parsePatch(diff)) {
      const path = p.newPath ?? p.oldPath!;
      const file = vscode.Uri.file(join(dir, path));
      let now = "";
      if (p.oldPath) {
        try {
          now = new TextDecoder().decode(await vscode.workspace.fs.readFile(file));
        } catch {
          // a file the change creates
        }
      }
      try {
        const left = p.oldPath ? file : this.proposed.add(path, "");
        const right = this.proposed.add(path, p.newPath ? applyPatch(now, p) : "");
        await vscode.commands.executeCommand("vscode.diff", left, right, vscode.l10n.t("{0} ↔ proposed by Blitz", path));
      } catch (e) {
        vscode.window.showWarningMessage(`${path}: ${e instanceof Error ? e.message : String(e)}`);
      }
    }
  }

  /** Sends the page a message, once it listens. */
  post(m: ToPage) {
    if (this.view && this.ready) this.view.webview.postMessage(m);
    else this.queue.push(m);
  }

  /** The editor's selection, as text for the chat's composer. */
  addSelection() {
    const ed = vscode.window.activeTextEditor;
    if (!ed) return;
    const sel = ed.selection;
    const text = ed.document.getText(sel);
    if (!text) return this.addFile(ed.document.uri);
    const path = this.chatPath(ed.document.uri);
    if (path === undefined) return;
    this.post({ type: "compose", text: selectionText(path, sel.start.line + 1, sel.end.line + (sel.end.character === 0 && sel.end.line > sel.start.line ? 0 : 1), ed.document.languageId, text) });
    this.reveal();
  }

  /** A file (the one in front, or the one clicked), mentioned in the composer. */
  addFile(uri?: vscode.Uri) {
    uri ??= vscode.window.activeTextEditor?.document.uri;
    if (!uri) return;
    const path = this.chatPath(uri);
    if (path === undefined) return;
    this.post({ type: "addToContext", path });
    this.reveal();
  }

  /**
   * uri's path in the chat's workspace (the page's, else the one a page
   * would open for), or undefined, saying so, when it isn't in it.
   */
  private chatPath(uri: vscode.Uri): string | undefined {
    const dir = this.view ? this.bound : this.dir();
    if (!dir) return undefined;
    const path = inWorkspace(dir, uri.fsPath);
    if (path === undefined) vscode.window.showWarningMessage(vscode.l10n.t("{0} isn't in the workspace Blitz works in ({1}).", uri.fsPath, dir));
    return path;
  }

  private reveal() {
    if (this.view) this.view.show(true);
    else vscode.commands.executeCommand("blitz.chat.focus");
  }

  async dispose() {
    await (await this.proxy)?.close();
  }
}

function notice(text: string): string {
  const esc = text.replace(/&/g, "&amp;").replace(/</g, "&lt;");
  return `<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'"></head><body style="padding:12px;font-family:var(--vscode-font-family)"><p>${esc}</p></body></html>`;
}

/** Starts the extension. */
export function activate(ctx: vscode.ExtensionContext) {
  const proposed = new Proposed();
  const chat = new ChatView(ctx, proposed);
  ctx.subscriptions.push(
    vscode.window.registerWebviewViewProvider("blitz.chat", chat, { webviewOptions: { retainContextWhenHidden: true } }),
    vscode.workspace.registerTextDocumentContentProvider(proposedScheme, proposed),
    vscode.workspace.onDidCloseTextDocument((doc) => proposed.closed(doc)),
    vscode.commands.registerCommand("blitz.focus", () => vscode.commands.executeCommand("blitz.chat.focus")),
    vscode.commands.registerCommand("blitz.addSelection", () => chat.addSelection()),
    vscode.commands.registerCommand("blitz.addFile", (uri?: vscode.Uri) => chat.addFile(uri)),
    { dispose: () => void chat.dispose() },
  );
}

/** Stops the extension (its subscriptions are disposed). */
export function deactivate() {}
