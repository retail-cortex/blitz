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

// Settings › Organization (spec_053_enterprise EN-10 to EN-13): the
// organization's Blitz service this computer connects to, and the
// sign-in to it, kept by the service (ConfigService.GetEnterprise,
// ConnectEnterprise, EnterpriseSignIn, EnterpriseSignOut). The sign-in
// page opens in the browser; connected, the organization's models are
// the ones Blitz uses.
import { mdiAlertCircleOutline, mdiCheckCircleOutline, mdiLinkVariantOff, mdiLoginVariant, mdiLogoutVariant, mdiOpenInNew } from "@mdi/js";
import { useCallback, useEffect, useRef, useState } from "react";
import { config } from "./api";
import { openURL } from "./desktop";
import { message } from "./errors";
import { configChanged } from "./events";
import type { EnterpriseState } from "./gen/blitz/v1/config_pb";
import { t } from "./i18n";
import { OrgNoticesCard } from "./OrgNotices";
import { Button, Field, Icon, useSnackbar } from "@blitz/webui";

/** The organization's service and the sign-in to it. */
export function OrganizationSettings() {
  const snack = useSnackbar();
  const [state, setState] = useState<EnterpriseState | null>(null);
  const [address, setAddress] = useState("");
  const [busy, setBusy] = useState(false);
  const [signingIn, setSigningIn] = useState(false);
  const [page, setPage] = useState("");
  const [error, setError] = useState("");
  const abort = useRef<AbortController | null>(null);

  const load = useCallback(async () => {
    try {
      const res = await config.getEnterprise({ check: true });
      setState(res.state ?? null);
    } catch (e) {
      setError(message(e));
    }
  }, []);
  useEffect(() => {
    void load();
  }, [load]);
  useEffect(() => () => abort.current?.abort(), []);

  const connect = async (url: string) => {
    setBusy(true);
    setError("");
    try {
      const res = await config.connectEnterprise({ url });
      setState(res.state ?? null);
      setAddress("");
      configChanged({ dir: "" }); // the models are the organization's now, or the person's again
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  };
  const signIn = async () => {
    const ctl = new AbortController();
    abort.current = ctl;
    setSigningIn(true);
    setError("");
    setPage("");
    try {
      for await (const ev of config.enterpriseSignIn({}, { signal: ctl.signal })) {
        if (ev.done) {
          if (ev.state) setState(ev.state);
          snack(t("desktop.org.signed_in_snack", { org: ev.state?.name || ev.state?.url || "" }));
          configChanged({ dir: "" });
        } else if (ev.url) {
          setPage(ev.url);
          void openURL(ev.url);
        }
      }
    } catch (e) {
      if (!ctl.signal.aborted) setError(message(e));
    } finally {
      setSigningIn(false);
      abort.current = null;
    }
  };
  const signOut = async () => {
    setError("");
    try {
      const res = await config.enterpriseSignOut({});
      setState(res.state ?? null);
      configChanged({ dir: "" });
    } catch (e) {
      setError(message(e));
    }
  };

  if (!state && !error) return <p className="muted">{t("desktop.checking")}</p>;
  const name = state?.name || state?.url || "";
  return (
    <div className="stack">
      <p className="t-body-sm muted">{t("desktop.org.about")}</p>
      {!state?.url ? (
        <Field label={t("desktop.org.address")} supporting={t("desktop.org.address_help")}>
          {(id) => (
            <div className="row">
              <input
                id={id}
                className="input mono"
                value={address}
                placeholder={t("desktop.org.address_placeholder")}
                spellCheck={false}
                onChange={(e) => setAddress(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && address.trim() && void connect(address.trim())}
              />
              <Button variant="filled" disabled={busy || !address.trim()} onClick={() => void connect(address.trim())}>
                {t("desktop.org.connect")}
              </Button>
            </div>
          )}
        </Field>
      ) : (
        <div className="signin-card">
          <div className="row signin-head">
            <Icon path={state.signedIn ? mdiCheckCircleOutline : mdiAlertCircleOutline} size="sm" className={state.signedIn ? "signin-ok" : "muted"} />
            <span className="signin-text">
              <span className="t-title-sm">{state.signedIn ? t("desktop.org.signed_in", { org: name, email: state.email }) : t("desktop.org.signed_out", { org: name })}</span>
              <span className="t-body-sm muted ellipsis" title={state.url}>
                {state.checked && !state.reachable ? t("desktop.org.unreachable", { url: state.url }) : state.url}
              </span>
            </span>
            <span className="spacer" />
            {signingIn ? (
              <Button small onClick={() => abort.current?.abort()}>
                {t("desktop.cancel")}
              </Button>
            ) : state.signedIn ? (
              <Button small icon={mdiLogoutVariant} onClick={() => void signOut()}>
                {t("desktop.org.sign_out")}
              </Button>
            ) : state.checked && !state.reachable ? (
              <Button small onClick={() => void load()}>
                {t("desktop.org.retry")}
              </Button>
            ) : (
              <Button small variant="filled" icon={mdiLoginVariant} onClick={() => void signIn()}>
                {t("desktop.org.sign_in")}
              </Button>
            )}
          </div>
          {signingIn && (
            <div className="signin-progress">
              <div className="progress" role="progressbar" aria-label={t("desktop.org.waiting")} />
              <p className="t-body-sm">{t("desktop.org.waiting")}</p>
              {page && (
                <Button small icon={mdiOpenInNew} onClick={() => void openURL(page)}>
                  {t("desktop.signin.open_page")}
                </Button>
              )}
            </div>
          )}
          {state.termsVersion && <p className="t-body-sm signin-note">{t("desktop.org.terms", { version: state.termsVersion })}</p>}
          {state.signedIn && state.roles.length > 0 && <p className="t-body-sm muted signin-note">{t("desktop.org.roles", { roles: state.roles.join(", ") })}</p>}
          {state.managed ? (
            // Device management set it (EN-10): it's not the person's to change.
            <p className="t-body-sm muted signin-note">{t("desktop.org.managed")}</p>
          ) : (
            <div className="row">
              <Button small variant="text" icon={mdiLinkVariantOff} disabled={busy || signingIn} onClick={() => void connect("")}>
                {t("desktop.org.disconnect")}
              </Button>
            </div>
          )}
        </div>
      )}
      {error && <p className="error-text t-body-sm">{error}</p>}
      {state?.signedIn && <OrgNoticesCard key={state.email} />}
    </div>
  );
}
