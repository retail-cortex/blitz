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

// Clients for the Blitz service. The desktop app's Go side forwards
// /blitz.v1.* requests from this page to the service's Unix socket, so
// the page talks to its own origin. A development build can use a fake
// service instead (see dev/fake.ts).
import { createClient, type Interceptor, type Transport } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { ConfigService } from "./gen/blitz/v1/config_pb";
import { FileService } from "./gen/blitz/v1/file_pb";
import { SessionService } from "./gen/blitz/v1/session_pb";
import { WorkerService } from "./gen/blitz/v1/worker_pb";
import { WorkspaceService } from "./gen/blitz/v1/workspace_pb";
import { isUnavailable } from "./errors";

type Listener = () => void;
const lost = new Set<Listener>();

/** Calls f whenever a call finds the service unreachable. */
export function onServiceLost(f: Listener): () => void {
  lost.add(f);
  return () => lost.delete(f);
}

/** Reports that the service can't be reached (from a failed stream, say). */
export function serviceLost() {
  lost.forEach((f) => f());
}

const watchAvailability: Interceptor = (next) => async (req) => {
  try {
    return await next(req);
  } catch (e) {
    if (isUnavailable(e)) serviceLost();
    throw e;
  }
};

// The page talks to its own origin (tests import this without a window).
const origin = typeof window === "undefined" ? "http://localhost" : window.location.origin;
let transport: Transport = createConnectTransport({ baseUrl: origin, interceptors: [watchAvailability] });

/** Replaces the transport (development: the fake service). */
export function setTransport(t: Transport) {
  transport = t;
  clients = make();
}

const make = () => ({
  sessions: createClient(SessionService, transport),
  workspaces: createClient(WorkspaceService, transport),
  workers: createClient(WorkerService, transport),
  config: createClient(ConfigService, transport),
  files: createClient(FileService, transport),
});
let clients = make();

// Proxies, so a transport swapped in before the first render is used.
export const sessions = new Proxy({} as ReturnType<typeof make>["sessions"], { get: (_, k) => Reflect.get(clients.sessions, k) });
/** WorkspaceService: agents, models, settings, permissions, checkpoints, images and the rest. */
export const workspaces = new Proxy({} as ReturnType<typeof make>["workspaces"], { get: (_, k) => Reflect.get(clients.workspaces, k) });
/** WorkerService: the workspace's workers and their runs. */
export const workers = new Proxy({} as ReturnType<typeof make>["workers"], { get: (_, k) => Reflect.get(clients.workers, k) });
/** ConfigService: providers, API keys and the settings files. */
export const config = new Proxy({} as ReturnType<typeof make>["config"], { get: (_, k) => Reflect.get(clients.config, k) });
/** FileService: the workspace's files, for the Files shelf and the editor. */
export const files = new Proxy({} as ReturnType<typeof make>["files"], { get: (_, k) => Reflect.get(clients.files, k) });
