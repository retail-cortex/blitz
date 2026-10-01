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

// The OS sandbox on Linux, where the page can fix it: Settings › Service's
// row, and the notice a workspace shows when it didn't open because the
// sandbox is required and isn't working (SANDBOX_UNAVAILABLE). Fixing it
// installs an AppArmor profile for bubblewrap with the system's password
// dialog, then restarts the service, so what it opens next is sandboxed.
import { useEffect, useState } from "react";
import { mdiAlertCircleOutline, mdiShieldLockOutline } from "@mdi/js";
import { fixSandbox, restartService, sandboxStatus, serviceStatus, type SandboxFix, type SandboxStatus } from "./desktop";
import { t } from "./i18n";
import { serviceInfo, waitForService } from "./serviceVersion";
import { Button, Icon } from "./ui/controls";

/** Where fixing the sandbox is: asking for the password, then restarting the service. */
export type FixPhase = "idle" | "fixing" | "restarting" | "done";

/**
 * The sandbox's state, and allow(): the fix, then a restart of the service.
 * status is null while checking and undefined in a browser.
 */
export function useSandboxFix(onFixed?: () => void) {
  const [status, setStatus] = useState<SandboxStatus | undefined | null>(null);
  const [phase, setPhase] = useState<FixPhase>("idle");
  const [fix, setFix] = useState<SandboxFix | null>(null);
  const [restartError, setRestartError] = useState("");
  useEffect(() => {
    sandboxStatus().then(setStatus, () => setStatus(undefined));
  }, []);
  const allow = async () => {
    setPhase("fixing");
    setRestartError("");
    const done = await fixSandbox().catch((e): SandboxFix => ({ status: status ?? { state: "broken" }, error: String(e) }));
    setFix(done);
    setStatus(done.status);
    if (done.error) {
      setPhase("idle");
      return;
    }
    // What the service opened before still has no sandbox: start afresh.
    setPhase("restarting");
    try {
      const info = await serviceInfo().catch(() => undefined);
      await restartService(info?.pid ?? 0);
      await waitForService(serviceStatus, true);
    } catch (e) {
      setRestartError(String(e));
    }
    setPhase("done");
    onFixed?.();
  };
  return { status, phase, fix, restartError, allow };
}

/** What the state means, for the row's or the notice's text. */
export function sandboxDetail(status: SandboxStatus | null): string {
  if (status === null) return t("desktop.checking");
  switch (status.state) {
    case "ready":
      return t("desktop.sandbox.ready", { bwrap: status.bwrap ?? "" });
    case "no_bwrap":
      return t("desktop.sandbox.no_bwrap");
    case "restricted":
      return t("desktop.sandbox.restricted", { detail: status.detail ?? "" });
    default:
      return t("desktop.sandbox.broken", { detail: status.detail ?? "" });
  }
}

/** The fix's outcome: restarting, done, or why it failed with the commands to run by hand. */
export function SandboxFixResult({ phase, fix, restartError }: Pick<ReturnType<typeof useSandboxFix>, "phase" | "fix" | "restartError">) {
  return (
    <>
      {phase === "restarting" && <p className="muted">{t("desktop.sandbox.restarting")}</p>}
      {phase === "done" && !restartError && <p className="muted">{t("desktop.sandbox.fixed")}</p>}
      {restartError && <p className="error-text">{t("desktop.sandbox.restart_failed", { error: restartError })}</p>}
      {fix?.error && (
        <div className="stack">
          <p className="error-text">{t("desktop.sandbox.failed", { error: fix.error })}</p>
          {fix.commands && (
            <div className="code-block">
              <pre className="mono t-body-sm">{fix.commands}</pre>
            </div>
          )}
        </div>
      )}
    </>
  );
}

/**
 * Above a workspace that didn't open because the sandbox is required and
 * isn't working: why, and Allow bubblewrap when AppArmor is the cause.
 * onFixed reloads the workspace once the service is back.
 */
export function SandboxNotice({ error, onFixed }: { error: string; onFixed: () => void }) {
  const { status, phase, fix, restartError, allow } = useSandboxFix(onFixed);
  const busy = phase === "fixing" || phase === "restarting";
  return (
    <div className="card error sandbox-notice" role="alert">
      <div className="row">
        <Icon path={mdiShieldLockOutline} />
        <strong className="spacer">{t("desktop.sandbox.blocked")}</strong>
        {status?.state === "restricted" && phase !== "done" && (
          <Button small variant="filled" disabled={busy} onClick={allow}>
            {t("desktop.sandbox.allow")}
          </Button>
        )}
      </div>
      <p className="t-body-sm">{status ? sandboxDetail(status) : error}</p>
      <SandboxFixResult phase={phase} fix={fix} restartError={restartError} />
      {status === undefined && (
        <p className="t-body-sm row">
          <Icon path={mdiAlertCircleOutline} size="sm" /> {error}
        </p>
      )}
    </div>
  );
}
