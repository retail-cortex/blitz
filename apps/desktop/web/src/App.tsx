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
import { InboxButton } from "./RunsInbox";
import { watchWorkerFailures } from "./workerFailures";
import { mdiAlertOutline, mdiCogOutline, mdiFolderOpenOutline, mdiLightningBolt, mdiServerOff } from "@mdi/js";
import { onServiceLost, workspaces as workspaceAPI } from "./api";
import { appVersion, chooseWorkspace, type LicenseText, installService, onDeepLink, onNotificationOpen, restartService, serviceStatus, type ServiceStatus, toggleFullscreen } from "./desktop";
import { checkService, type ServiceCheck } from "./serviceVersion";
import { CommandPalette } from "./CommandPalette";
import { ErrorBoundary } from "./ErrorBoundary";
import { t, useLanguage } from "./i18n";
import { message, reason } from "./errors";
import { compose, showLicenseEvent } from "./events";
import { UnsavedDialog } from "./files/EditorPane";
import { LicenseDialog } from "./LicenseDialog";
import { Brand, WorkspaceSwitcher } from "./WorkspaceSwitcher";
import { unsavedIn } from "./files/unsaved";
import { closeWorkspace, displayName, openWorkspace, openWorkspaces, recentWorkspaces } from "./prefs";
import { SettingsDialog } from "./SettingsDialog";
import { AppStateProvider, useApp } from "./state";
import { Button, Dialog, Icon, IconButton, SnackbarProvider, useSnackbar } from "./ui/controls";
import { Workspace } from "./Workspace";
import { WorkspaceDialog } from "./WorkspaceDialog";

/**
 * The window: its preferences and service check, then the workspaces (or
 * the welcome page), with the dialogs and banners over them.
 */
export function App() {
  return (
    <ErrorBoundary>
      <AppStateProvider>
        <SnackbarProvider>
          <Shell />
        </SnackbarProvider>
      </AppStateProvider>
    </ErrorBoundary>
  );
}

type Service = { state: "checking" } | { state: "down"; status: ServiceStatus } | { state: "up" } | { state: "lost" };

/** Polls the service's status every interval while wanted. */
function useServiceStatus() {
  const [service, setService] = useState<Service>({ state: "checking" });
  const [error, setError] = useState("");
  const check = useCallback(async () => {
    try {
      const s = await serviceStatus();
      setService((cur) => (s.running ? (cur.state === "up" ? cur : { state: "up" }) : { state: "down", status: s }));
    } catch (e) {
      setError(String(e));
    }
  }, []);
  useEffect(() => {
    check();
  }, [check]);
  // A call that finds the service gone: show it, and look again until it's back.
  useEffect(() => onServiceLost(() => setService((cur) => (cur.state === "up" ? { state: "lost" } : cur))), []);
  useEffect(() => {
    if (service.state !== "down" && service.state !== "lost") return;
    const t = setInterval(check, 2500);
    return () => clearInterval(t);
  }, [service.state, check]);
  return { service, error, check, setError };
}

/** Whether the service holding the socket is the one this app expects (DSK-51a). */
function useServiceVersion(up: boolean) {
  const [state, setState] = useState<ServiceCheck>({ app: "", stale: "" });
  const check = useCallback(async () => {
    try {
      const next = await checkService(await appVersion());
      setState(next);
      return next;
    } catch {
      return undefined; // an unreachable service is reported by the status checks
    }
  }, []);
  useEffect(() => {
    if (up) check();
  }, [up, check]);
  return { ...state, check };
}

/** Says the service is stale, and restarts it with the matching version. */
function StaleService({ state, check }: { state: ServiceCheck; check: () => Promise<ServiceCheck | undefined> }) {
  const { stale, info, app } = state;
  const snack = useSnackbar();
  const [bin, setBin] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    serviceStatus().then((s) => setBin(s.service), () => setBin(""));
  }, []);
  const restart = async () => {
    setBusy(true);
    setError("");
    try {
      // Stops the stale service (its login item, else its process) and
      // starts this app's blitzd, at login too.
      await restartService(info?.pid ?? 0);
      for (let i = 0; i < 30; i++) {
        await new Promise((r) => setTimeout(r, 1000));
        const now = await check();
        if (now && !now.stale) {
          snack(t("desktop.service.restarted", { version: now.info?.version ?? "?" }));
          return;
        }
      }
      setError(t("desktop.service.still_stale"));
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="banner">
      <Icon path={mdiServerOff} />
      <span className="spacer">
        {t(`desktop.service.stale.${stale}`, { service: info?.version ?? "?", app, path: info?.executable ?? "" })}
        {bin === "" && <span className="muted"> {t("desktop.service.no_blitzd")}</span>}
        {error && <span className="error-text"> {error}</span>}
      </span>
      {bin ? (
        <Button variant="tonal" small disabled={busy} onClick={restart}>
          {busy ? t("desktop.service.restarting") : t("desktop.service.restart")}
        </Button>
      ) : null}
    </div>
  );
}

function Shell() {
  const { prefs, loaded, update, prefsError, activity, stop } = useApp();
  useLanguage(); // the whole window follows a language change
  const [confirmClose, setConfirmClose] = useState<string | null>(null);
  const snack = useSnackbar();
  const { service, error, check, setError } = useServiceStatus();
  // The open workspaces' failed worker runs, for their badges (BL-WK-11).
  const seen = JSON.stringify(openWorkspaces(prefs).map((w) => [w.dir, w.workers_seen ?? 0]));
  useEffect(() => {
    if (service.state === "up") watchWorkerFailures(Object.fromEntries(JSON.parse(seen)));
  }, [seen, service.state]);
  const version = useServiceVersion(service.state === "up");
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  // Cmd/Ctrl+K opens the command palette; F11 (and ⌃⌘F on macOS) makes
  // the window full screen.
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPaletteOpen((o) => !o);
      } else if (e.key === "F11" || (e.metaKey && e.ctrlKey && e.key.toLowerCase() === "f")) {
        e.preventDefault();
        toggleFullscreen().catch(() => {});
      }
    };
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  }, []);
  const [editing, setEditing] = useState<string | null>(null);
  // Bumped when the service comes back, so workspaces reload what they show.
  const [generation, setGeneration] = useState(0);
  const [wasLost, setWasLost] = useState(false);

  useEffect(() => {
    if (service.state === "lost") setWasLost(true);
    if (service.state === "up" && wasLost) {
      setWasLost(false);
      setGeneration((g) => g + 1);
      snack(t("desktop.app.reconnected"));
    }
  }, [service.state, wasLost, snack]);
  useEffect(() => {
    if (prefsError) snack(prefsError, { error: true });
  }, [prefsError, snack]);
  // A file dropped outside the chat mustn't make the web view open it.
  useEffect(() => {
    const stop = (e: DragEvent) => e.preventDefault();
    window.addEventListener("dragover", stop);
    window.addEventListener("drop", stop);
    return () => {
      window.removeEventListener("dragover", stop);
      window.removeEventListener("drop", stop);
    };
  }, []);
  // A click on a notification shows its workspace.
  useEffect(() => onNotificationOpen((dir) => update((p) => openWorkspace(p, dir))), [update]);
  // A blitz://open link opens its folder with the prompt filled in, not sent.
  useEffect(
    () =>
      onDeepLink((link) => {
        update((p) => openWorkspace(p, link.dir));
        if (link.prompt) compose({ dir: link.dir, text: link.prompt });
        snack(t("desktop.link_opened", { dir: displayName({ dir: link.dir, name: "" }) }));
      }),
    [update, snack],
  );

  const open = useCallback(async () => {
    const dir = await chooseWorkspace().catch((e) => (snack(String(e), { error: true }), ""));
    if (dir) update((p) => openWorkspace(p, dir));
  }, [update, snack]);

  const doClose = useCallback(
    async (dir: string) => {
      update((p) => closeWorkspace(p, dir));
      try {
        await workspaceAPI.closeWorkspace({ workspace: dir });
      } catch (e) {
        // Another client's turn keeps it open in the service; the tab goes anyway.
        if (reason(e) !== "TURN_RUNNING") snack(message(e), { error: true });
      }
      snack(t("desktop.app.closed", { name: displayName(prefs.workspaces.find((w) => w.dir === dir) ?? { dir }) }), {
        action: { label: t("desktop.undo"), run: () => update((p) => openWorkspace(p, dir)) },
      });
    },
    [update, snack, prefs.workspaces],
  );
  // Unsaved files are discarded, and a turn running here stopped, only
  // once the user agrees.
  const [unsavedClose, setUnsavedClose] = useState<string | null>(null);
  // The license dialog, from Settings › About or /license.
  const [license, setLicense] = useState<LicenseText | null>(null);
  useEffect(() => {
    const f = (e: Event) => setLicense((e as CustomEvent<{ which: LicenseText }>).detail.which);
    window.addEventListener(showLicenseEvent, f);
    return () => window.removeEventListener(showLicenseEvent, f);
  }, []);
  const close = useCallback(
    (dir: string, unsavedOK = false) => (!unsavedOK && unsavedIn(dir) ? setUnsavedClose(dir) : activity[dir]?.running ? setConfirmClose(dir) : doClose(dir)),
    [activity, doClose],
  );

  if (!loaded || service.state === "checking") return <Splash text={error || t("desktop.app.starting")} />;
  if (service.state === "down") return <ServiceDown status={service.status} onStarted={check} error={error} setError={setError} />;

  const open_ = openWorkspaces(prefs);
  const editingWs = prefs.workspaces.find((w) => w.dir === editing);
  return (
    <div className="shell">
      <main className="main">
        {service.state === "lost" && (
          <div className="banner">
            <Icon path={mdiServerOff} />
            <span>{t("desktop.app.lost")}</span>
            <div className="progress spacer" />
          </div>
        )}
        {service.state === "up" && version.stale && <StaleService state={version} check={version.check} />}
        {open_.length === 0 && <Welcome onOpen={open} onEdit={setEditing} onClose={close} onSettings={() => setSettingsOpen(true)} />}
        {open_.map((w) => (
          <Workspace
            key={`${w.dir}#${generation}`}
            ws={w}
            visible={w.dir === prefs.active}
            onOpenWorkspace={open}
            onEditWorkspace={setEditing}
            onCloseWorkspace={close}
            onSettings={() => setSettingsOpen(true)}
          />
        ))}
      </main>
      {unsavedClose && (
        <UnsavedDialog
          names={[displayName(prefs.workspaces.find((w) => w.dir === unsavedClose) ?? { dir: unsavedClose })]}
          count={unsavedIn(unsavedClose)}
          onDiscard={() => {
            const dir = unsavedClose;
            setUnsavedClose(null);
            close(dir, true);
          }}
          onCancel={() => setUnsavedClose(null)}
        />
      )}
      {settingsOpen && <SettingsDialog onClose={() => setSettingsOpen(false)} />}
      {license && <LicenseDialog initial={license} onClose={() => setLicense(null)} />}
      {paletteOpen && <CommandPalette onClose={() => setPaletteOpen(false)} onOpenWorkspace={open} onSettings={() => setSettingsOpen(true)} />}
      {confirmClose && (
        <Dialog
          title={t("desktop.app.stop_close.title")}
          icon={mdiAlertOutline}
          onClose={() => setConfirmClose(null)}
          footer={
            <>
              <Button onClick={() => setConfirmClose(null)}>{t("desktop.cancel")}</Button>
              <Button
                variant="filled"
                danger
                onClick={async () => {
                  const dir = confirmClose;
                  setConfirmClose(null);
                  await stop(dir);
                  doClose(dir);
                }}
              >
                {t("desktop.app.stop_close")}
              </Button>
            </>
          }
        >
          <p className="muted">{t("desktop.app.stop_close.body", { name: displayName(prefs.workspaces.find((w) => w.dir === confirmClose) ?? { dir: confirmClose }) })}</p>
        </Dialog>
      )}
      {editingWs && <WorkspaceDialog ws={editingWs} onClose={() => setEditing(null)} onCloseWorkspace={editingWs.open ? () => (setEditing(null), close(editingWs.dir)) : undefined} />}
    </div>
  );
}

function Splash({ text }: { text: string }) {
  return (
    <div className="splash">
      <div className="drag-region" />
      <Icon path={mdiLightningBolt} size="lg" className="brand-mark" />
      <p className="muted">{text}</p>
    </div>
  );
}

function ServiceDown({ status, onStarted, error, setError }: { status: ServiceStatus; onStarted: () => void; error: string; setError: (s: string) => void }) {
  const [busy, setBusy] = useState(false);
  const install = async () => {
    setError("");
    setBusy(true);
    try {
      await installService();
      onStarted();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="splash">
      <div className="drag-region" />
      <div className="splash-card">
        <Icon path={mdiLightningBolt} size="lg" className="brand-mark" />
        <h1 className="t-headline">{t("desktop.service.start_title")}</h1>
        <p className="muted">{t("desktop.service.explain")}</p>
        {status.service ? (
          <Button variant="filled" onClick={install} disabled={busy}>
            {busy ? t("desktop.app.starting") : status.installed ? t("desktop.service.start") : t("desktop.service.install")}
          </Button>
        ) : (
          <p className="error-text">{t("desktop.service.no_blitzd")}</p>
        )}
        <p className="t-body-sm muted">{t("desktop.service.waiting", { socket: status.socket })}</p>
        {error && <p className="error-text">{error}</p>}
      </div>
    </div>
  );
}

function Welcome({ onOpen, onEdit, onClose, onSettings }: { onOpen: () => void; onEdit: (dir: string) => void; onClose: (dir: string) => void; onSettings: () => void }) {
  const { prefs, update } = useApp();
  const recent = recentWorkspaces(prefs).slice(0, 6);
  return (
    <div className="welcome">
      <header className="topbar drag-region welcome-bar">
        <Brand />
        <WorkspaceSwitcher onOpen={onOpen} onClose={onClose} onEdit={onEdit} />
        <span className="spacer" />
        <div className="topbar-actions no-drag">
          <InboxButton />
          <IconButton icon={mdiCogOutline} label={t("desktop.settings")} onClick={onSettings} />
        </div>
      </header>
      <div className="welcome-body">
        <h1 className="t-display">{t("desktop.welcome.title")}</h1>
        <p className="t-title muted">{t("desktop.welcome.body")}</p>
        <Button variant="filled" icon={mdiFolderOpenOutline} onClick={onOpen}>
          {t("desktop.workspace.open_one")}
        </Button>
        {recent.length > 0 && (
          <>
            <h2 className="t-title-sm muted welcome-recent">{t("desktop.recent")}</h2>
            <div className="recent-grid">
              {recent.map((w) => (
                <button key={w.dir} className="recent-card" onClick={() => update((p) => openWorkspace(p, w.dir))} title={w.dir}>
                  <span className="t-title ellipsis">{displayName(w)}</span>
                  <span className="t-body-sm muted ellipsis">{w.description || w.dir}</span>
                </button>
              ))}
            </div>
          </>
        )}
      </div>
    </div>
  );
}
