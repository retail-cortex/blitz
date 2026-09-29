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

// Whether the running service is the one this app expects (DSK-51a). The
// app talks to whatever service holds the socket, which may be an older
// install, or one whose program has since been removed or replaced.
import { Code, ConnectError } from "@connectrpc/connect";
import { workspaces } from "./api";
import { programExists } from "./desktop";

/** What the running service says about itself (GetServiceInfo). */
export interface ServiceInfo {
  version: string;
  executable: string;
  pid: number;
  /** Its program file was replaced or removed since it started. */
  replaced?: boolean;
}

/** Why the service should be restarted, or "" when it's the expected one. */
export type Stale = "" | "old" | "mismatch" | "missing";

/**
 * old: the service predates GetServiceInfo (info undefined); missing: its
 * program is gone or was replaced since it started (a new build or package
 * installed: it runs the old one, whatever the versions say); mismatch:
 * another version than the app. Development builds are all "dev" and
 * match each other, so a replaced program is what tells them apart.
 */
export function staleReason(appVersion: string, info: ServiceInfo | undefined, programThere: boolean): Stale {
  if (!info) return "old";
  if (!programThere || info.replaced) return "missing";
  if (info.version !== appVersion) return "mismatch";
  return "";
}

/** Asks the service what it is; undefined when it's too old to say. */
export async function serviceInfo(): Promise<ServiceInfo | undefined> {
  try {
    const res = await workspaces.getServiceInfo({});
    return { version: res.version, executable: res.executable, pid: res.pid, replaced: res.replaced };
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

/** How long to wait for the service to start or stop after an action. */
export const settleTimeout = 15_000;

/**
 * Asks status until running is want, every interval, for at most timeout:
 * after installing or restarting the service (which returns before the
 * new one listens) or stopping it. Resolves with the last status and
 * whether it settled.
 */
export async function waitForService<S extends { running: boolean }>(
  status: () => Promise<S>,
  want: boolean,
  timeout = settleTimeout,
  interval = 300,
  sleep: (ms: number) => Promise<void> = (ms) => new Promise((r) => setTimeout(r, ms)),
): Promise<{ status: S; settled: boolean }> {
  let waited = 0;
  for (;;) {
    const s = await status();
    if (s.running === want) return { status: s, settled: true };
    if (waited >= timeout) return { status: s, settled: false };
    await sleep(interval);
    waited += interval;
  }
}

/** p, or a rejection after ms: a call to a service that's starting may never answer. */
export function withTimeout<T>(p: Promise<T>, ms: number): Promise<T> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error(`no answer in ${ms / 1000} s`)), ms);
    p.then(
      (v) => (clearTimeout(timer), resolve(v)),
      (e) => (clearTimeout(timer), reject(e)),
    );
  });
}
