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

// What the page sees of the window (window.blitzElectron), and nothing
// else: its calls go to the main process by name, checked there, and it
// hears only the window's own events. A sandboxed preload can't load the
// app's other modules, so the lists are here as well as in logic.ts.
import { contextBridge, ipcRenderer, webUtils, type IpcRendererEvent } from "electron";

const methods = [
  "ChooseWorkspace",
  "ChooseFile",
  "Notify",
  "OpenURL",
  "PrintPDF",
  "SetAppearance",
  "SetMenu",
  "SetUnsaved",
  "PendingLinks",
  "ToggleFullscreen",
  "Quit",
  "OpenApp",
  "PreviewOpen",
  "PreviewBounds",
  "PreviewVisible",
  "PreviewNavigate",
  "PreviewClose",
  "PreviewCapture",
  "PreviewContent",
  "PreviewConsole",
  "PreviewPrintPDF",
  "PreviewPicture",
  "DragOut",
  "TrashDropped",
  "SignInWindow",
  "AgentBrowserOpen",
  "AgentBrowserSend",
  "AgentBrowserClose",
  "AgentBrowserTakeOver",
];
const events = new Set(["menu:about", "menu:settings", "deeplink:open", "notification:open", "preview:state", "tasks:open", "agentbrowser:message", "agentbrowser:closed"]);

contextBridge.exposeInMainWorld("blitzElectron", {
  /** The calls the window answers. */
  methods,
  /** Whether the window is translucent (macOS), for the page's glass. */
  nativeGlass: process.platform === "darwin",
  /** Calls one of methods with its arguments. */
  call: (method: string, ...args: unknown[]) => ipcRenderer.invoke("blitz:call", method, args),
  /**
   * The paths on the computer of files dropped on the page (EL-15), ""
   * for one that isn't a file there. The window remembers them: they're
   * the only ones TrashDropped (a move into the workspace) can take.
   */
  pathsOf: async (files: File[]) => {
    const paths = files.map((f) => {
      try {
        return webUtils.getPathForFile(f);
      } catch {
        return "";
      }
    });
    await ipcRenderer.invoke("blitz:dropped", paths.filter(Boolean));
    return paths;
  },
  /** Calls f with each of the window's event; returns what stops that. */
  on: (event: string, f: (arg: unknown) => void) => {
    if (!events.has(event)) return () => {};
    const listener = (_e: IpcRendererEvent, arg: unknown) => f(arg);
    ipcRenderer.on(event, listener);
    return () => {
      ipcRenderer.removeListener(event, listener);
    };
  },
});
