// Clients for the Blitz service. The desktop app's Go side forwards
// /blitz.v1.* requests from this page to the service's Unix socket, so
// the page talks to its own origin. A development build can use a fake
// service instead (see dev/fake.ts).
import { createClient, type Interceptor, type Transport } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { ConfigService } from "./gen/blitz/v1/config_pb";
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
});
let clients = make();

// Proxies, so a transport swapped in before the first render is used.
export const sessions = new Proxy({} as ReturnType<typeof make>["sessions"], { get: (_, k) => Reflect.get(clients.sessions, k) });
export const workspaces = new Proxy({} as ReturnType<typeof make>["workspaces"], { get: (_, k) => Reflect.get(clients.workspaces, k) });
export const workers = new Proxy({} as ReturnType<typeof make>["workers"], { get: (_, k) => Reflect.get(clients.workers, k) });
export const config = new Proxy({} as ReturnType<typeof make>["config"], { get: (_, k) => Reflect.get(clients.config, k) });
