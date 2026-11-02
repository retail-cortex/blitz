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

// The organization's branding (spec_053_enterprise EN-50 to EN-53): the
// service keeps a signed copy on this computer, so the window wears it
// from the start, offline too; it's asked of the organization's service
// at start, every few minutes and when the settings change. Its name
// replaces Blitz's in the window's title and the top bar, its logo the
// mark, and each theme's accent Blitz's, where it reads well enough on
// that theme. While the organization's terms wait to be accepted, they
// show before anything else.
import { mdiAccountAlertOutline, mdiCloudOffOutline, mdiFileDocumentCheckOutline } from "@mdi/js";
import { useCallback, useEffect, useState, useSyncExternalStore } from "react";
import { config } from "./api";
import { message } from "./errors";
import { configChanged, configChangedEvent } from "./events";
import type { Branding, EnterpriseState, OrgDocument, OrgWorkspace } from "./gen/blitz/v1/config_pb";
import { setAppName, t } from "./i18n";
import { Markdown } from "./Markdown";
import { openURL } from "./desktop";
import { Button, Dialog, Icon } from "@blitz/webui";

let current: Branding | null = null;
const listeners = new Set<() => void>();

/** The organization's branding, null for Blitz's own look. */
export function useOrgBranding(): Branding | null {
  return useSyncExternalStore(
    (f) => {
      listeners.add(f);
      return () => listeners.delete(f);
    },
    () => current,
  );
}

function set(b: Branding | null) {
  current = b;
  applyBranding(b);
  for (const f of listeners) f();
}

/** Asks the service for the branding: the kept copy, or refreshed first. */
export async function loadBranding(refresh: boolean) {
  try {
    const r = await config.getBranding({ refresh });
    set(r.branding ?? null);
  } catch {
    // The service isn't up yet: the look stays as it is.
  }
}

/** How often the branding is asked of the organization's service. */
const refreshEvery = 10 * 60_000;

/**
 * Starts keeping the branding current: the kept copy at once, then the
 * service's, every few minutes, and whenever the settings change (a
 * connection, a sign-in). Returns the stop.
 */
export function startBranding(): () => void {
  void loadBranding(false).then(() => loadBranding(true));
  const timer = setInterval(() => void loadBranding(true), refreshEvery);
  const onChange = () => void loadBranding(true);
  window.addEventListener(configChangedEvent, onChange);
  return () => {
    clearInterval(timer);
    window.removeEventListener(configChangedEvent, onChange);
  };
}

/** Whether the page shows its dark theme (html[data-theme]), kept current. */
export function useDarkTheme(): boolean {
  return useSyncExternalStore(
    (f) => {
      const o = new MutationObserver(f);
      o.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
      return () => o.disconnect();
    },
    () => document.documentElement.dataset.theme === "dark",
  );
}

/** The app's name: the organization's, else Blitz's. */
export function appName(b: Branding | null): string {
  return b?.name || t("desktop.brand");
}

/** The logo for the theme, a data URL, or "" for Blitz's mark. */
export function logoFor(b: Branding | null, dark: boolean): string {
  if (!b) return "";
  return (dark && b.images["logo-dark.svg"]) || b.images["logo.svg"] || b.images["logo.png"] || b.images["icon.png"] || "";
}

// WCAG's relative luminance and contrast ratio, for "#rrggbb".
function luminance(hex: string): number {
  const v = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255).map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
  return 0.2126 * v[0] + 0.7152 * v[1] + 0.0722 * v[2];
}

/** The contrast ratio of two "#rrggbb" colours, 1 to 21. */
export function contrast(a: string, b: string): number {
  const [x, y] = [luminance(a), luminance(b)].sort((p, q) => q - p);
  return (x + 0.05) / (y + 0.05);
}

const hexColour = /^#[0-9a-fA-F]{6}$/;
/** The themes' backgrounds (m3.css's --md-surface). */
const backgrounds = { light: "#ffffff", dark: "#131314" };

/**
 * An accent's rules for a theme, or "" when it isn't a colour or reads
 * below WCAG AA (4.5:1) on the theme's background: Blitz's stays (EN-51).
 */
export function accentRules(accent: string, theme: "light" | "dark"): string {
  if (!hexColour.test(accent) || contrast(accent, backgrounds[theme]) < 4.5) return "";
  const on = contrast(accent, "#ffffff") >= contrast(accent, "#000000") ? "#ffffff" : "#000000";
  // Doubled, so it wins over a colour palette's own primary.
  return `:root:root[data-theme="${theme}"] { --md-primary: ${accent}; --md-on-primary: ${on}; --accent-a: ${accent}; --accent-b: ${accent}; --on-accent: ${on}; }`;
}

function applyBranding(b: Branding | null) {
  setAppName(b?.name ?? "");
  document.title = appName(b);
  let style = document.getElementById("org-accent");
  const css = b ? [accentRules(b.accent, "light"), accentRules(b.accentDark || b.accent, "dark")].filter(Boolean).join("\n") : "";
  if (!css) {
    style?.remove();
    return;
  }
  if (!style) {
    style = document.createElement("style");
    style.id = "org-accent";
    document.head.appendChild(style);
  }
  style.textContent = css;
}

/**
 * The organization's terms, before anything else while a version waits to
 * be accepted (EN-53): Accept records it on the service; Decline signs out
 * of the organization.
 */
export function TermsGate() {
  const [terms, setTerms] = useState<{ version: string; text: string; url: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const check = useCallback(async () => {
    try {
      const st = (await config.getEnterprise({ check: false })).state;
      if (!st?.signedIn) return setTerms(null);
      const r = await config.getTerms({});
      setTerms(r.version && !r.accepted ? { version: r.version, text: r.text, url: r.url } : null);
    } catch {
      // Not connected, signed out or unreachable: nothing to ask now.
    }
  }, []);
  useEffect(() => {
    void check();
    window.addEventListener(configChangedEvent, check);
    const timer = setInterval(() => void check(), refreshEvery);
    return () => {
      window.removeEventListener(configChangedEvent, check);
      clearInterval(timer);
    };
  }, [check]);
  if (!terms) return null;
  const act = async (f: () => Promise<unknown>) => {
    setBusy(true);
    setError("");
    try {
      await f();
      setTerms(null);
      configChanged({ dir: "" });
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  };
  const name = appName(current);
  return (
    <Dialog
      title={t("desktop.org.terms_title", { org: name })}
      icon={mdiFileDocumentCheckOutline}
      large
      onClose={() => {}}
      footer={
        <>
          <Button disabled={busy} onClick={() => void act(() => config.enterpriseSignOut({}))}>
            {t("desktop.org.terms_decline")}
          </Button>
          <Button variant="filled" disabled={busy} onClick={() => void act(() => config.acceptTerms({ version: terms.version }))}>
            {t("desktop.org.terms_accept")}
          </Button>
        </>
      }
    >
      <p className="t-body-sm muted">{t("desktop.org.terms_body", { org: name, version: terms.version })}</p>
      {terms.text ? (
        <Markdown text={terms.text} />
      ) : (
        terms.url && (
          <Button small onClick={() => void openURL(terms.url)}>
            {t("desktop.org.terms_read")}
          </Button>
        )
      )}
      {error && <p className="error-text t-body-sm">{error}</p>}
    </Dialog>
  );
}

let shownDocument: OrgDocument | null = null;
const documentListeners = new Set<() => void>();

/** Shows one of the organization's documents (its text) over the window. */
export function showOrgDocument(d: OrgDocument | null) {
  shownDocument = d;
  for (const f of documentListeners) f();
}

/** The organization's document showOrgDocument opened, if one is. */
export function OrgDocumentDialog() {
  const d = useSyncExternalStore(
    (f) => {
      documentListeners.add(f);
      return () => documentListeners.delete(f);
    },
    () => shownDocument,
  );
  if (!d) return null;
  return (
    <Dialog
      title={t(d.document === "privacy" ? "desktop.org.privacy" : "desktop.org.terms_doc", { version: d.version })}
      icon={mdiFileDocumentCheckOutline}
      large
      onClose={() => showOrgDocument(null)}
      closeButton
    >
      <Markdown text={d.text} />
    </Dialog>
  );
}

/** How often the banner asks whether the organization's service answers. */
const checkEvery = 2 * 60_000;

/**
 * The window's word on the organization (EN-13): signed out, with Sign in
 * (Settings › Organization), or its service not answering, with Retry.
 * Work that needs the service waits; local work goes on.
 */
export function OrgBanner({ onSignIn }: { onSignIn: () => void }) {
  const [state, setState] = useState<EnterpriseState | null>(null);
  const [checking, setChecking] = useState(false);
  const check = useCallback(async () => {
    setChecking(true);
    try {
      setState((await config.getEnterprise({ check: true })).state ?? null);
    } catch {
      setState(null);
    } finally {
      setChecking(false);
    }
  }, []);
  useEffect(() => {
    void check();
    window.addEventListener(configChangedEvent, check);
    const timer = setInterval(() => void check(), checkEvery);
    return () => {
      window.removeEventListener(configChangedEvent, check);
      clearInterval(timer);
    };
  }, [check]);
  if (!state?.url) return null;
  const name = appName(current) === t("desktop.brand") ? state.name || state.url : appName(current);
  if (state.checked && !state.reachable) {
    return (
      <div className="banner">
        <Icon path={mdiCloudOffOutline} />
        <span className="spacer">{t("desktop.org.banner_unreachable", { org: name })}</span>
        <Button variant="tonal" small disabled={checking} onClick={() => void check()}>
          {t("desktop.org.retry")}
        </Button>
      </div>
    );
  }
  if (!state.signedIn) {
    return (
      <div className="banner">
        <Icon path={mdiAccountAlertOutline} />
        <span className="spacer">{t("desktop.org.banner_signed_out", { org: name })}</span>
        <Button variant="tonal" small onClick={onSignIn}>
          {t("desktop.org.sign_in")}
        </Button>
      </div>
    );
  }
  return null;
}

/** The organization's workspaces the person has a role in, and its name. */
export interface OrgWorkspaces {
  organization: string;
  workspaces: OrgWorkspace[];
}

/**
 * The organization's workspaces (EN-60), asked again when the settings
 * change; null when Blitz isn't connected or signed in.
 */
export function useOrgWorkspaces(): OrgWorkspaces | null {
  const [state, setState] = useState<OrgWorkspaces | null>(null);
  const load = useCallback(async () => {
    try {
      const r = await config.listOrgWorkspaces({});
      setState({ organization: r.organization, workspaces: r.workspaces });
    } catch {
      setState(null);
    }
  }, []);
  useEffect(() => {
    void load();
    window.addEventListener(configChangedEvent, load);
    return () => window.removeEventListener(configChangedEvent, load);
  }, [load]);
  return state;
}
