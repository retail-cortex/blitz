// Whether the running service is the one this app expects (DSK-51a). The
// app talks to whatever service holds the socket, which may be an older
// install, or one whose program has since been removed or replaced.
import { Code, ConnectError } from "@connectrpc/connect";
import { workspaces } from "./api";
import { programExists } from "./desktop";

export interface ServiceInfo {
  version: string;
  executable: string;
}

/** Why the service should be restarted, or "" when it's the expected one. */
export type Stale = "" | "old" | "mismatch" | "missing";

/**
 * old: the service predates GetServiceInfo (info undefined); missing: its
 * program is gone; mismatch: another version than the app. Development
 * builds are all "dev" and match each other.
 */
export function staleReason(appVersion: string, info: ServiceInfo | undefined, programThere: boolean): Stale {
  if (!info) return "old";
  if (!programThere) return "missing";
  if (info.version !== appVersion) return "mismatch";
  return "";
}

/** Asks the service what it is; undefined when it's too old to say. */
export async function serviceInfo(): Promise<ServiceInfo | undefined> {
  try {
    const res = await workspaces.getServiceInfo({});
    return { version: res.version, executable: res.executable };
  } catch (e) {
    if (e instanceof ConnectError && (e.code === Code.Unimplemented || e.code === Code.NotFound)) return undefined;
    throw e;
  }
}

/** What checkService found: the service's info, the app's version, and why the service is stale. */
export interface ServiceCheck {
  info?: ServiceInfo;
  app: string;
  stale: Stale;
}

/** The service's info and why it's stale, for this app's version. */
export async function checkService(app: string): Promise<ServiceCheck> {
  const info = await serviceInfo();
  const there = info ? await programExists(info.executable) : true;
  return { info, app, stale: staleReason(app, info, there) };
}
