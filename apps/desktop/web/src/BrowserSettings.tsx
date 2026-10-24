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

import { useCallback, useEffect, useState } from "react";
import { mdiAccountKeyOutline, mdiDeleteOutline, mdiGoogleChrome, mdiLogin, mdiLogoutVariant, mdiPlus } from "@mdi/js";
import { config } from "./api";
import { canCall, signInWindow } from "./desktop";
import { message } from "./errors";
import type { BrowserProfile } from "./gen/blitz/v1/config_pb";
import { t } from "./i18n";
import { Button, Dialog, Icon, IconButton, useSnackbar } from "./ui/controls";

/** Whether name can name a profile: lowercase letters, digits, - and _, up to 32. */
export const validProfileName = (name: string) => /^[a-z0-9][a-z0-9_-]{0,31}$/.test(name);

/** A profile's sites as the field shows them, and back: one a line, or commas. */
export const domainsText = (d: readonly string[]) => d.join("\n");
/** The sites typed in the field: lowercased, once each. */
export const parseDomains = (text: string) => [...new Set(text.split(/[\s,]+/).map((d) => d.trim().toLowerCase()).filter(Boolean))];

/**
 * Settings › Browser (spec_browser_profiles_049): the browser profiles
 * the agents browse with, so they can use the sign-ins (OAuth, SSO, a
 * site's own) the person made in them. Each: Sign in… (a window of the
 * app's on the profile, for the browsing the agent does in the app),
 * Sign in with Chrome… (for jobs and the CLI), the sites it browses
 * without asking, Sign out of everything, Delete. A workspace chooses one
 * in its settings.
 */
export function BrowserSettings() {
  const snack = useSnackbar();
  const [profiles, setProfiles] = useState<BrowserProfile[]>([]);
  const [error, setError] = useState("");
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [deleting, setDeleting] = useState("");
  const [show, setShow] = useState("ask");
  const load = useCallback(async () => {
    try {
      const r = await config.listBrowserProfiles({});
      setProfiles(r.profiles);
      setShow(r.show || "ask");
      setError("");
    } catch (e) {
      setError(message(e));
    }
  }, []);
  useEffect(() => void load(), [load]);
  const act = async (run: () => Promise<unknown>, done: string) => {
    setBusy(true);
    try {
      await run();
      snack(done);
      await load();
      return true;
    } catch (e) {
      snack(message(e), { error: true });
      return false;
    } finally {
      setBusy(false);
    }
  };
  const problem = name && !validProfileName(name) ? t("desktop.browser.bad_name") : "";
  return (
    <div className="stack browser-settings" style={{ gap: 12 }}>
      {/* How the agent browses while the app is open (spec_agent_browser_050 AB-02). */}
      <label className="field">
        <span className="t-label">{t("desktop.browser.show")}</span>
        <select className="select" value={show} disabled={busy} onChange={(e) => void act(() => config.setConfigValue({ workspace: "", key: "browser.show", value: e.target.value }), t("desktop.browser.show_saved"))}>
          {["ask", "watch", "silent"].map((v) => (
            <option key={v} value={v}>
              {t(`desktop.browser.show.${v}`)}
            </option>
          ))}
        </select>
        <span className="t-body-sm muted">{t("desktop.browser.show_hint")}</span>
      </label>
      <p className="t-body-sm muted">{t("desktop.browser.intro")}</p>
      {error && <p className="error-text">{error}</p>}
      {profiles.length === 0 ? (
        <p className="t-body-sm muted">{t("desktop.browser.none")}</p>
      ) : (
        profiles.map((p) => <ProfileCard key={p.name} profile={p} busy={busy} act={act} onDelete={() => setDeleting(p.name)} />)
      )}
      <form
        className="row"
        style={{ gap: 8, alignItems: "flex-end" }}
        onSubmit={(e) => {
          e.preventDefault();
          if (!name || problem) return;
          void act(() => config.createBrowserProfile({ name }), t("desktop.browser.created", { name })).then((ok) => ok && setName(""));
        }}
      >
        <label className="field" style={{ flex: 1 }}>
          <span className="t-label">{t("desktop.browser.new")}</span>
          <input className={`input mono ${problem ? "invalid" : ""}`} value={name} placeholder={t("desktop.browser.name_placeholder")} spellCheck={false} disabled={busy} onChange={(e) => setName(e.target.value.trim())} />
        </label>
        <Button variant="tonal" icon={mdiPlus} type="submit" disabled={busy || !name || !!problem}>
          {t("desktop.browser.add")}
        </Button>
      </form>
      {problem && <p className="t-body-sm error-text">{problem}</p>}
      {deleting && (
        <Dialog
          title={t("desktop.browser.delete_title", { name: deleting })}
          icon={mdiDeleteOutline}
          onClose={() => setDeleting("")}
          footer={
            <>
              <Button onClick={() => setDeleting("")}>{t("desktop.cancel")}</Button>
              <Button variant="filled" danger disabled={busy} onClick={() => void act(() => config.deleteBrowserProfile({ name: deleting }), t("desktop.browser.deleted", { name: deleting })).then(() => setDeleting(""))}>
                {t("desktop.browser.delete")}
              </Button>
            </>
          }
        >
          <p>{t("desktop.browser.delete_body", { name: deleting })}</p>
        </Dialog>
      )}
    </div>
  );
}

function ProfileCard({ profile: p, busy, act, onDelete }: { profile: BrowserProfile; busy: boolean; act: (run: () => Promise<unknown>, done: string) => Promise<boolean>; onDelete: () => void }) {
  const [site, setSite] = useState("");
  const [domains, setDomains] = useState(domainsText(p.allowDomains));
  useEffect(() => setDomains(domainsText(p.allowDomains)), [p.allowDomains]);
  const inApp = canCall("SignInWindow");
  return (
    <section className="card outlined stack" style={{ gap: 10 }}>
      <div className="row" style={{ gap: 8 }}>
        <Icon path={mdiAccountKeyOutline} />
        <span className="t-title-sm spacer">{p.name}</span>
        <IconButton icon={mdiLogoutVariant} label={t("desktop.browser.clear", { name: p.name })} small disabled={busy} onClick={() => void act(() => config.clearBrowserProfile({ name: p.name }), t("desktop.browser.cleared", { name: p.name }))} />
        <IconButton icon={mdiDeleteOutline} label={t("desktop.browser.delete_named", { name: p.name })} small disabled={busy} onClick={onDelete} />
      </div>
      <div className="row wrap" style={{ gap: 8, alignItems: "flex-end" }}>
        <label className="field" style={{ flex: 1, minWidth: 220 }}>
          <span className="t-label">{t("desktop.browser.site")}</span>
          <input className="input" value={site} placeholder={t("desktop.browser.site_placeholder")} spellCheck={false} onChange={(e) => setSite(e.target.value.trim())} />
        </label>
        {inApp && (
          <Button icon={mdiLogin} disabled={busy} onClick={() => void signInWindow(p.name, site).catch(() => undefined)}>
            {t("desktop.browser.sign_in")}
          </Button>
        )}
        <Button icon={mdiGoogleChrome} disabled={busy} onClick={() => void act(() => config.openBrowserProfile({ name: p.name, url: site }), t("desktop.browser.opened", { name: p.name }))}>
          {t("desktop.browser.sign_in_chrome")}
        </Button>
      </div>
      <span className="t-body-sm muted">{t(inApp ? "desktop.browser.sign_in_hint" : "desktop.browser.sign_in_hint_chrome")}</span>
      <label className="field">
        <span className="t-label">{t("desktop.browser.domains")}</span>
        <textarea
          className="input mono"
          rows={2}
          value={domains}
          placeholder={t("desktop.browser.allow_placeholder")}
          spellCheck={false}
          onChange={(e) => setDomains(e.target.value)}
          onBlur={() => {
            const next = parseDomains(domains);
            if (next.join(",") !== p.allowDomains.join(",")) void act(() => config.setBrowserProfileDomains({ name: p.name, allowDomains: next }), t("desktop.browser.domains_saved", { name: p.name }));
          }}
        />
        <span className="t-body-sm muted">{t("desktop.browser.domains_hint")}</span>
      </label>
    </section>
  );
}
