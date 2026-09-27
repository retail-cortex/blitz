import { useEffect, useState } from "react";
import {
  mdiBrain,
  mdiCheckCircleOutline,
  mdiCogOutline,
  mdiFolderMultipleOutline,
  mdiInformationOutline,
  mdiMonitor,
  mdiPaletteOutline,
  mdiPencilOutline,
  mdiServerNetwork,
  mdiAlertCircleOutline,
  mdiWeatherNight,
  mdiWhiteBalanceSunny,
} from "@mdi/js";
import { installService, serviceStatus, type ServiceStatus } from "./desktop";
import { workspaceColor } from "./palette";
import { displayName, forgetWorkspace } from "./prefs";
import { useApp } from "./state";
import type { ThemePref } from "./theme";
import { Button, Chip, Dialog, Icon, IconButton, Segmented, Switch } from "./ui/controls";
import { WorkspaceDialog } from "./WorkspaceDialog";

type Section = "appearance" | "workspaces" | "service" | "about";

const sections: { id: Section; label: string; icon: string }[] = [
  { id: "appearance", label: "Appearance", icon: mdiPaletteOutline },
  { id: "workspaces", label: "Workspaces", icon: mdiFolderMultipleOutline },
  { id: "service", label: "Service", icon: mdiServerNetwork },
  { id: "about", label: "About", icon: mdiInformationOutline },
];

/** The window's settings. The agent's settings are per workspace, in its run settings panel. */
export function SettingsDialog({ onClose }: { onClose: () => void }) {
  const [section, setSection] = useState<Section>("appearance");
  return (
    <Dialog title="Settings" icon={mdiCogOutline} onClose={onClose} wide footer={<Button onClick={onClose}>Done</Button>}>
      <div className="settings">
        <div className="settings-nav list" role="tablist">
          {sections.map((s) => (
            <button key={s.id} role="tab" aria-selected={s.id === section} className={`list-item ${s.id === section ? "active" : ""}`} onClick={() => setSection(s.id)}>
              <Icon path={s.icon} />
              {s.label}
            </button>
          ))}
        </div>
        <div className="settings-pane">
          {section === "appearance" && <Appearance />}
          {section === "workspaces" && <Workspaces />}
          {section === "service" && <Service />}
          {section === "about" && <About />}
        </div>
      </div>
    </Dialog>
  );
}

function Setting({ title, detail, children }: { title: string; detail?: string; children: React.ReactNode }) {
  return (
    <div className="setting">
      <div className="stack" style={{ gap: 2 }}>
        <span className="t-title-sm">{title}</span>
        {detail && <span className="t-body-sm muted">{detail}</span>}
      </div>
      <div className="setting-control">{children}</div>
    </div>
  );
}

function Appearance() {
  const { prefs, update } = useApp();
  return (
    <div className="stack" style={{ gap: 0 }}>
      <Setting title="Theme" detail="System follows your computer's light or dark setting.">
        <Segmented<ThemePref>
          label="Theme"
          value={prefs.theme}
          onChange={(theme) => update((p) => ({ ...p, theme }))}
          options={[
            { value: "system", label: "System", icon: mdiMonitor },
            { value: "light", label: "Light", icon: mdiWhiteBalanceSunny },
            { value: "dark", label: "Dark", icon: mdiWeatherNight },
          ]}
        />
      </Setting>
      <Setting title="Density" detail="Compact fits more on screen.">
        <Segmented
          label="Density"
          value={prefs.density}
          onChange={(density) => update((p) => ({ ...p, density }))}
          options={[
            { value: "comfortable", label: "Comfortable" },
            { value: "compact", label: "Compact" },
          ]}
        />
      </Setting>
      <Setting title="Show thinking" detail="Show the model's reasoning, folded, above its answer (when the model shares it).">
        <Switch label="Show thinking" checked={prefs.show_thoughts} onChange={(show_thoughts) => update((p) => ({ ...p, show_thoughts }))} />
      </Setting>
      <Setting title="Notifications" detail="Tell me when the agent needs me, or finishes a longer turn, while I'm in another window or workspace.">
        <Switch label="Notifications" checked={prefs.notifications === "on"} onChange={(on) => update((p) => ({ ...p, notifications: on ? "on" : "off" }))} />
      </Setting>
      <Setting title="Run settings panel" detail="Show the agent, model and permission settings beside the conversation.">
        <Switch label="Run settings panel" checked={prefs.run_settings} onChange={(run_settings) => update((p) => ({ ...p, run_settings }))} />
      </Setting>
      <p className="t-body-sm muted" style={{ marginTop: 16 }}>
        <Icon path={mdiBrain} size="sm" /> How hard the model thinks, its permissions and its model are set per workspace, in the run settings panel.
      </p>
    </div>
  );
}

function Workspaces() {
  const { prefs, update, theme } = useApp();
  const [editing, setEditing] = useState<string | null>(null);
  const ws = prefs.workspaces.find((w) => w.dir === editing);
  if (prefs.workspaces.length === 0) return <p className="muted">No workspaces yet.</p>;
  return (
    <div className="list">
      {prefs.workspaces.map((w) => (
        <div key={w.dir} className="list-item static-item">
          <span className="avatar" style={{ background: workspaceColor(w.color, theme) }}>
            {displayName(w).slice(0, 1).toUpperCase()}
          </span>
          <span className="lines">
            <span className="ellipsis">{displayName(w)}</span>
            <small className="ellipsis mono">{w.dir}</small>
          </span>
          <span className="trailing">
            {w.open ? <Chip className="static">Open</Chip> : <Button small onClick={() => update((p) => forgetWorkspace(p, w.dir))}>Forget</Button>}
            <IconButton icon={mdiPencilOutline} label="Edit details" small onClick={() => setEditing(w.dir)} />
          </span>
        </div>
      ))}
      {ws && <WorkspaceDialog ws={ws} onClose={() => setEditing(null)} />}
    </div>
  );
}

function Service() {
  const [status, setStatus] = useState<ServiceStatus | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const refresh = () => serviceStatus().then(setStatus, (e) => setError(String(e)));
  useEffect(() => {
    refresh();
  }, []);
  if (!status) return <p className="muted">{error || "Checking…"}</p>;
  return (
    <div className="stack" style={{ gap: 0 }}>
      <Setting title="Status" detail="The service holds workspaces and runs workers, also while this window is closed.">
        {status.running ? (
          <Chip className="static" icon={mdiCheckCircleOutline} selected>
            Running
          </Chip>
        ) : (
          <Chip className="static" icon={mdiAlertCircleOutline} tone="danger">
            Not running
          </Chip>
        )}
      </Setting>
      <Setting title="Starts at login" detail={status.installed ? "Installed as a login item." : "Not installed: it runs only once started."}>
        <Button
          variant="tonal"
          small
          disabled={!status.cli || busy}
          onClick={async () => {
            setBusy(true);
            setError("");
            try {
              await installService();
              await refresh();
            } catch (e) {
              setError(String(e));
            } finally {
              setBusy(false);
            }
          }}
        >
          {status.installed ? "Reinstall" : "Install"}
        </Button>
      </Setting>
      <Setting title="Socket">
        <code className="muted">{status.socket}</code>
      </Setting>
      <Setting title="Command line">
        <code className="muted">{status.cli || "not found"}</code>
      </Setting>
      {error && <p className="error-text">{error}</p>}
    </div>
  );
}

function About() {
  return (
    <div className="stack">
      <span className="t-title-lg">Blitz</span>
      <span className="muted">Desktop app {__APP_VERSION__}</span>
      <p className="muted">
        A window onto the Blitz service on this computer. Your workspaces, sessions and workers live in the service; this window keeps only its own settings, in{" "}
        <code>~/.blitz/desktop.json</code>.
      </p>
      <p className="t-body-sm muted">Apache License 2.0.</p>
    </div>
  );
}
