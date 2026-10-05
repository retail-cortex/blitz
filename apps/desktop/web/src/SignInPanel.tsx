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

// A provider's sign-in with its vendor's tool, run by the service
// (ConfigService.GetSignIn, SignIn, SignOut): whether it's signed in, a
// Sign in button that streams the tool's progress (the sign-in page's
// URL, a device code), and Sign out. It sits beside the API key choice:
// signing in adds a way, it never replaces the key.
import { mdiAlertCircleOutline, mdiCheckCircleOutline, mdiLoginVariant, mdiLogoutVariant, mdiOpenInNew } from "@mdi/js";
import { useCallback, useEffect, useRef, useState } from "react";
import { config } from "./api";
import { openURL } from "./desktop";
import { message } from "./errors";
import { configChanged } from "./events";
import type { SignInStatus } from "./gen/blitz/v1/config_pb";
import { t } from "./i18n";
import { Button, Icon, useSnackbar } from "./ui/controls";

/** A provider's sign-in status and buttons (provider: google, anthropic, aws or azure). */
export function SignInPanel({ workspace, provider, profile = "" }: { workspace: string; provider: string; profile?: string }) {
  const snack = useSnackbar();
  const [status, setStatus] = useState<SignInStatus | null>(null);
  const [running, setRunning] = useState(false);
  const [progress, setProgress] = useState<{ line: string; url: string; code: string }>({ line: "", url: "", code: "" });
  const [error, setError] = useState("");
  const abort = useRef<AbortController | null>(null);

  const load = useCallback(async () => {
    try {
      const res = await config.getSignIn({ workspace, provider, profile });
      setStatus(res.statuses[0] ?? null);
    } catch (e) {
      setError(message(e));
    }
  }, [workspace, provider, profile]);
  useEffect(() => {
    setStatus(null);
    setError("");
    void load();
  }, [load]);
  useEffect(() => () => abort.current?.abort(), []);

  const name = t(`desktop.signin.provider.${provider}`);
  const signIn = async () => {
    const ctl = new AbortController();
    abort.current = ctl;
    setRunning(true);
    setError("");
    setProgress({ line: "", url: "", code: "" });
    try {
      for await (const ev of config.signIn({ workspace, provider, profile }, { signal: ctl.signal })) {
        if (ev.done) {
          if (ev.status) setStatus(ev.status);
          snack(t("desktop.signin.done", { provider: name }));
          configChanged({ dir: workspace }); // models retried with the new credentials
        } else {
          setProgress((p) => ({ line: ev.line || p.line, url: ev.url || p.url, code: ev.code || p.code }));
        }
      }
    } catch (e) {
      if (!ctl.signal.aborted) setError(message(e));
    } finally {
      setRunning(false);
      abort.current = null;
    }
  };
  const signOut = async () => {
    setError("");
    try {
      const res = await config.signOut({ workspace, provider, profile });
      if (res.status) setStatus(res.status);
      configChanged({ dir: workspace });
    } catch (e) {
      setError(message(e));
    }
  };

  if (!status && !error) return <div className="signin-card muted t-body-sm">{t("desktop.checking")}</div>;
  return (
    <div className="signin-card">
      {status && (
        <div className="row signin-head">
          <Icon path={status.signedIn ? mdiCheckCircleOutline : mdiAlertCircleOutline} size="sm" className={status.signedIn ? "signin-ok" : "muted"} />
          <span className="signin-text">
            <span className="t-title-sm">{status.signedIn ? t("desktop.signin.signed_in", { provider: name }) : t("desktop.signin.signed_out", { provider: name })}</span>
            {status.detail && <span className="t-body-sm muted ellipsis" title={status.detail}>{status.detail}</span>}
          </span>
          <span className="spacer" />
          {running ? (
            <Button small onClick={() => abort.current?.abort()}>
              {t("desktop.cancel")}
            </Button>
          ) : status.signedIn ? (
            <Button small icon={mdiLogoutVariant} onClick={() => void signOut()} disabled={!status.toolFound}>
              {t("desktop.signin.sign_out")}
            </Button>
          ) : (
            <Button small variant="filled" icon={mdiLoginVariant} onClick={() => void signIn()} disabled={!status.toolFound}>
              {t("desktop.signin.sign_in")}
            </Button>
          )}
        </div>
      )}
      {status && !status.toolFound && (
        <p className="t-body-sm signin-note">
          {t("desktop.signin.no_tool", { tool: status.tool })}{" "}
          <button type="button" className="link-button" onClick={() => void openURL(status.install)}>
            {t("desktop.signin.get_tool", { tool: status.tool })}
          </button>
        </p>
      )}
      {running && (
        <div className="signin-progress">
          <div className="progress" role="progressbar" aria-label={t("desktop.signin.waiting")} />
          <p className="t-body-sm">{t("desktop.signin.waiting")}</p>
          {progress.code && (
            <p className="signin-code" aria-label={t("desktop.signin.code")}>
              {progress.code}
            </p>
          )}
          {progress.url && (
            <Button small icon={mdiOpenInNew} onClick={() => void openURL(progress.url)}>
              {t("desktop.signin.open_page")}
            </Button>
          )}
          {progress.line && !progress.url && <p className="t-body-sm muted mono ellipsis">{progress.line}</p>}
        </div>
      )}
      {error && <p className="error-text t-body-sm">{error}</p>}
    </div>
  );
}
