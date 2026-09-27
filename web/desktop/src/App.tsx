import { useCallback, useEffect, useState } from "react";
import { mdiAlertOutline, mdiFolderOpenOutline, mdiLightningBolt, mdiServerOff } from "@mdi/js";
import { onServiceLost, workspaces as workspaceAPI } from "./api";
import { chooseWorkspace, installService, onNotificationOpen, serviceStatus, type ServiceStatus } from "./desktop";
import { Drawer } from "./Drawer";
import { ErrorBoundary } from "./ErrorBoundary";
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
  const [confirmClose, setConfirmClose] = useState<string | null>(null);
  const snack = useSnackbar();
  const { service, error, check, setError } = useServiceStatus();
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  // Bumped when the service comes back, so workspaces reload what they show.
  const [generation, setGeneration] = useState(0);
  const [wasLost, setWasLost] = useState(false);

  useEffect(() => {
    if (service.state === "lost") setWasLost(true);
    if (service.state === "up" && wasLost) {
      setWasLost(false);
      setGeneration((g) => g + 1);
      snack("Reconnected to the Blitz service.");
    }
  }, [service.state, wasLost, snack]);
  useEffect(() => {
    if (prefsError) snack(prefsError, { error: true });
  }, [prefsError, snack]);
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
      snack(`Closed ${displayName(prefs.workspaces.find((w) => w.dir === dir) ?? { dir })}.`, {
        action: { label: "Undo", run: () => update((p) => openWorkspace(p, dir)) },
      });
    },
    [update, snack, prefs.workspaces],
  );
  // A turn running here is stopped first, once the user agrees.
  const close = useCallback((dir: string) => (activity[dir]?.running ? setConfirmClose(dir) : doClose(dir)), [activity, doClose]);

  if (!loaded || service.state === "checking") return <Splash text={error || "Starting…"} />;
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
            <span>The Blitz service isn't answering. Reconnecting…</span>
            <div className="progress spacer" />
          </div>
        )}
        {open_.length === 0 && <Welcome onOpen={open} />}
        {open_.map((w) => (
          <Workspace key={`${w.dir}#${generation}`} ws={w} visible={w.dir === prefs.active} onEdit={() => setEditing(w.dir)} onClose={() => close(w.dir)} />
        ))}
      </main>
      {settingsOpen && <SettingsDialog onClose={() => setSettingsOpen(false)} />}
      {confirmClose && (
        <Dialog
          title="Stop the turn and close?"
          icon={mdiAlertOutline}
          onClose={() => setConfirmClose(null)}
          footer={
            <>
              <Button onClick={() => setConfirmClose(null)}>Cancel</Button>
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
                Stop and close
              </Button>
            </>
          }
        >
          <p className="muted">
            The agent is working in {displayName(prefs.workspaces.find((w) => w.dir === confirmClose) ?? { dir: confirmClose })}. Closing stops its turn; what
            it already changed stays.
          </p>
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
        <h1 className="t-headline">Start the Blitz service</h1>
        <p className="muted">
          The service holds your workspaces and runs scheduled workers. It keeps running after this window closes, and this window connects to it.
        </p>
        {status.cli ? (
          <Button variant="filled" onClick={install} disabled={busy}>
            {busy ? "Starting…" : status.installed ? "Start the service" : "Install and start the service"}
          </Button>
        ) : (
          <p className="error-text">
            The blitz command wasn't found. Install Blitz's CLI, then run <code>blitz service install</code>.
          </p>
        )}
        <p className="t-body-sm muted">Waiting for it on {status.socket}…</p>
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
        <h1 className="t-display">Welcome to Blitz</h1>
        <p className="t-title muted">Open a project folder to start working with the agent.</p>
        <Button variant="filled" icon={mdiFolderOpenOutline} onClick={onOpen}>
          Open a workspace
        </Button>
        {recent.length > 0 && (
          <>
            <h2 className="t-title-sm muted welcome-recent">Recent</h2>
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
