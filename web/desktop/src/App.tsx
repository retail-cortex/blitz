import { useCallback, useEffect, useState } from "react";
import { mdiAlertOutline, mdiFolderOpenOutline, mdiLightningBolt, mdiServerOff } from "@mdi/js";
import { onServiceLost, workspaces as workspaceAPI } from "./api";
import { chooseWorkspace, installService, onNotificationOpen, serviceStatus, type ServiceStatus } from "./desktop";
import { CommandPalette } from "./CommandPalette";
import { Drawer } from "./Drawer";
import { ErrorBoundary } from "./ErrorBoundary";
import { t, useLanguage } from "./i18n";
import { message, reason } from "./errors";
import { closeWorkspace, displayName, openWorkspace, openWorkspaces, recentWorkspaces } from "./prefs";
import { SettingsDialog } from "./SettingsDialog";
import { AppStateProvider, useApp } from "./state";
import { Button, Dialog, Icon, SnackbarProvider, useSnackbar } from "./ui/controls";
import { Workspace } from "./Workspace";
import { WorkspaceDialog } from "./WorkspaceDialog";

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

function Shell() {
  const { prefs, loaded, update, prefsError, activity, stop, drawer } = useApp();
  useLanguage(); // the whole window follows a language change
  const [confirmClose, setConfirmClose] = useState<string | null>(null);
  const snack = useSnackbar();
  const { service, error, check, setError } = useServiceStatus();
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  // Cmd/Ctrl+K opens the command palette.
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPaletteOpen((o) => !o);
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
  // A turn running here is stopped first, once the user agrees.
  const close = useCallback((dir: string) => (activity[dir]?.running ? setConfirmClose(dir) : doClose(dir)), [activity, doClose]);

  if (!loaded || service.state === "checking") return <Splash text={error || t("desktop.app.starting")} />;
  if (service.state === "down") return <ServiceDown status={service.status} onStarted={check} error={error} setError={setError} />;

  const open_ = openWorkspaces(prefs);
  const editingWs = prefs.workspaces.find((w) => w.dir === editing);
  return (
    <div className={`shell drawer-${drawer}`}>
      <Drawer onOpen={open} onClose={close} onSettings={() => setSettingsOpen(true)} onEdit={setEditing} />
      <main className="main">
        {service.state === "lost" && (
          <div className="banner">
            <Icon path={mdiServerOff} />
            <span>{t("desktop.app.lost")}</span>
            <div className="progress spacer" />
          </div>
        )}
        {open_.length === 0 && <Welcome onOpen={open} />}
        {open_.map((w) => (
          <Workspace key={`${w.dir}#${generation}`} ws={w} visible={w.dir === prefs.active} onEdit={() => setEditing(w.dir)} onClose={() => close(w.dir)} />
        ))}
      </main>
      {settingsOpen && <SettingsDialog onClose={() => setSettingsOpen(false)} />}
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
        {status.cli ? (
          <Button variant="filled" onClick={install} disabled={busy}>
            {busy ? t("desktop.app.starting") : status.installed ? t("desktop.service.start") : t("desktop.service.install")}
          </Button>
        ) : (
          <p className="error-text">{t("desktop.service.no_cli", { command: "blitz service install" })}</p>
        )}
        <p className="t-body-sm muted">{t("desktop.service.waiting", { socket: status.socket })}</p>
        {error && <p className="error-text">{error}</p>}
      </div>
    </div>
  );
}

function Welcome({ onOpen }: { onOpen: () => void }) {
  const { prefs, update } = useApp();
  const recent = recentWorkspaces(prefs).slice(0, 6);
  return (
    <div className="welcome">
      <div className="drag-region" />
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
