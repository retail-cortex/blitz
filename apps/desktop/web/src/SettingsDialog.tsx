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
import {
  mdiBrain,
  mdiCheckCircleOutline,
  mdiCogOutline,
  mdiFileDocumentEditOutline,
  mdiFolderMultipleOutline,
  mdiKeyChainVariant,
  mdiProgressClock,
  mdiShieldAlertOutline,
  mdiShieldCheckOutline,
  mdiInformationOutline,
  mdiMonitor,
  mdiPaletteOutline,
  mdiPencilOutline,
  mdiScaleBalance,
  mdiServerNetwork,
  mdiTextBoxSearchOutline,
  mdiAlertCircleOutline,
  mdiWeatherNight,
  mdiWhiteBalanceSunny,
} from "@mdi/js";
import { appVersion, installService, restartService, serviceStatus, setTray, stopService, type ServiceStatus } from "./desktop";
import { showLicense } from "./events";
import { checkService, settleTimeout, type ServiceCheck, waitForService, withTimeout } from "./serviceVersion";
import { languages, t } from "./i18n";
import { workspaceColor } from "./palette";
import { displayName, forgetWorkspace } from "./prefs";
import { PermissionSettings } from "./PermissionSettings";
import { LogViewer } from "./LogViewer";
import { ProviderSettings } from "./ProviderSettings";
import { SettingsFile } from "./settingsfile/SettingsFile";
import { useApp } from "./state";
import type { ThemePref } from "./theme";
import { Button, Chip, Dialog, Icon, IconButton, Segmented, Switch } from "./ui/controls";
import { WorkspaceDialog } from "./WorkspaceDialog";
import { ProjectDialog } from "./ProjectDialog";

type Section = "appearance" | "providers" | "permissions" | "file" | "workspaces" | "service" | "logs" | "about";

const sectionIcons: Record<Section, string> = {
  appearance: mdiPaletteOutline,
  providers: mdiKeyChainVariant,
  permissions: mdiShieldCheckOutline,
  file: mdiFileDocumentEditOutline,
  workspaces: mdiFolderMultipleOutline,
  service: mdiServerNetwork,
  logs: mdiTextBoxSearchOutline,
  about: mdiInformationOutline,
};

/** The window's settings. The agent's settings are per workspace, in its run settings panel. */
export function SettingsDialog({ onClose }: { onClose: () => void }) {
  const [section, setSection] = useState<Section>("appearance");
  return (
    <Dialog title={t("desktop.settings")} icon={mdiCogOutline} onClose={onClose} large footer={<Button onClick={onClose}>{t("desktop.done")}</Button>}>
      <div className="settings">
        <div className="settings-nav list" role="tablist">
          {(Object.keys(sectionIcons) as Section[]).map((id) => (
            <button key={id} role="tab" aria-selected={id === section} className={`list-item ${id === section ? "active" : ""}`} onClick={() => setSection(id)}>
              <Icon path={sectionIcons[id]} />
              {t(`desktop.settings.${id}`)}
            </button>
          ))}
        </div>
        <div className="settings-pane">
          {section === "appearance" && <Appearance />}
          {section === "providers" && <ProviderSettings workspace="" />}
          {section === "permissions" && <PermissionSettings workspace="" />}
          {section === "file" && <FileSection />}
          {section === "workspaces" && <Workspaces />}
          {section === "service" && <Service />}
          {section === "logs" && <LogViewer />}
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
      <Setting title={t("desktop.settings.theme")} detail={t("desktop.settings.theme.detail")}>
        <Segmented<ThemePref>
          label={t("desktop.settings.theme")}
          value={prefs.theme}
          onChange={(theme) => update((p) => ({ ...p, theme }))}
          options={[
            { value: "system", label: t("desktop.settings.theme.system"), icon: mdiMonitor },
            { value: "light", label: t("desktop.settings.theme.light"), icon: mdiWhiteBalanceSunny },
            { value: "dark", label: t("desktop.settings.theme.dark"), icon: mdiWeatherNight },
          ]}
        />
      </Setting>
      <Setting title={t("desktop.settings.language")} detail={t("desktop.settings.language.detail")}>
        <select className="select language-select" aria-label={t("desktop.settings.language")} value={prefs.language} onChange={(e) => update((p) => ({ ...p, language: e.target.value }))}>
          <option value="system">{t("desktop.settings.language.system")}</option>
          {languages.map((l) => (
            <option key={l.tag} value={l.tag}>
              {l.name}
            </option>
          ))}
        </select>
      </Setting>
      <Setting title={t("desktop.settings.density")} detail={t("desktop.settings.density.detail")}>
        <Segmented
          label={t("desktop.settings.density")}
          value={prefs.density}
          onChange={(density) => update((p) => ({ ...p, density }))}
          options={[
            { value: "comfortable", label: t("desktop.settings.density.comfortable") },
            { value: "compact", label: t("desktop.settings.density.compact") },
          ]}
        />
      </Setting>
      <Setting title={t("desktop.settings.transparency")} detail={t("desktop.settings.transparency.detail")}>
        <Switch label={t("desktop.settings.transparency")} checked={prefs.transparency === "on"} onChange={(on) => update((p) => ({ ...p, transparency: on ? "on" : "off" }))} />
      </Setting>
      <Setting title={t("desktop.settings.width")} detail={t("desktop.settings.width.detail")}>
        <Segmented
          label={t("desktop.settings.width")}
          value={prefs.width}
          onChange={(width) => update((p) => ({ ...p, width }))}
          options={[
            { value: "full", label: t("desktop.settings.width.full") },
            { value: "readable", label: t("desktop.settings.width.readable") },
          ]}
        />
      </Setting>
      <Setting title={t("desktop.settings.thinking")} detail={t("desktop.settings.thinking.detail")}>
        <Switch label={t("desktop.settings.thinking")} checked={prefs.show_thoughts} onChange={(show_thoughts) => update((p) => ({ ...p, show_thoughts }))} />
      </Setting>
      <Setting title={t("desktop.settings.notifications")} detail={t("desktop.settings.notifications.detail")}>
        <Switch label={t("desktop.settings.notifications")} checked={prefs.notifications === "on"} onChange={(on) => update((p) => ({ ...p, notifications: on ? "on" : "off" }))} />
      </Setting>
      <Setting title={t("desktop.settings.task_continue")} detail={t("desktop.settings.task_continue.detail")}>
        <Switch label={t("desktop.settings.task_continue")} checked={prefs.task_continue} onChange={(task_continue) => update((p) => ({ ...p, task_continue }))} />
      </Setting>
      <Setting title={t("desktop.settings.panel")} detail={t("desktop.settings.panel.detail")}>
        <Switch label={t("desktop.settings.panel")} checked={prefs.run_settings} onChange={(run_settings) => update((p) => ({ ...p, run_settings }))} />
      </Setting>
      <p className="t-body-sm muted" style={{ marginTop: 16 }}>
        <Icon path={mdiBrain} size="sm" /> {t("desktop.settings.per_workspace")}
      </p>
    </div>
  );
}

// The settings file of the global scope or of an open or known workspace.
function FileSection() {
  const { prefs } = useApp();
  return <SettingsFile scopes={prefs.workspaces.map((w) => ({ dir: w.dir, name: displayName(w) }))} />;
}

function Workspaces() {
  const { prefs, update, theme } = useApp();
  const [editing, setEditing] = useState<string | null>(null);
  const [project, setProject] = useState<string | null>(null);
  const ws = prefs.workspaces.find((w) => w.dir === editing);
  if (prefs.workspaces.length === 0) return <p className="muted">{t("desktop.settings.no_workspaces")}</p>;
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
            {w.open ? (
              <Chip className="static">{t("desktop.settings.open")}</Chip>
            ) : (
              <Button small onClick={() => update((p) => forgetWorkspace(p, w.dir))}>
                {t("desktop.forget")}
              </Button>
            )}
            {w.open && <IconButton icon={mdiShieldAlertOutline} label={t("desktop.project.open")} small onClick={() => setProject(w.dir)} />}
            <IconButton icon={mdiPencilOutline} label={t("desktop.edit_details")} small onClick={() => setEditing(w.dir)} />
          </span>
        </div>
      ))}
      {ws && <WorkspaceDialog ws={ws} onClose={() => setEditing(null)} />}
      {project && <ProjectDialog dir={project} onClose={() => setProject(null)} />}
    </div>
  );
}

function Service() {
  const [status, setStatus] = useState<ServiceStatus | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [waiting, setWaiting] = useState(false);
  const [version, setVersion] = useState<ServiceCheck | null>(null);
  // Shows a status, and the running service's version (never an earlier
  // one's); a service still starting may not answer, so the check is short.
  const show = useCallback(async (s: ServiceStatus) => {
    setStatus(s);
    if (!s.running) return setVersion(null);
    try {
      setVersion(await withTimeout(appVersion().then(checkService), 5_000));
    } catch {
      setVersion(null);
    }
  }, []);
  const refresh = useCallback(() => serviceStatus().then(show, (e) => setError(String(e))), [show]);
  useEffect(() => {
    refresh();
  }, [refresh]);
  // Kept current while open: the service can change from elsewhere (the
  // CLI, a crash).
  useEffect(() => {
    if (busy) return;
    const timer = setInterval(refresh, 5_000);
    return () => clearInterval(timer);
  }, [busy, refresh]);
  // Runs one service action, then waits for the service to be running (or
  // stopped): installing or restarting returns before the new one listens.
  const act = async (f: () => Promise<void>, running: boolean) => {
    setBusy(true);
    setError("");
    try {
      await f();
      setWaiting(true);
      const { status: s, settled } = await waitForService(serviceStatus, running);
      await show(s);
      if (!settled) setError(t(running ? "desktop.service.didnt_start" : "desktop.service.didnt_stop", { seconds: settleTimeout / 1000 }));
    } catch (e) {
      setError(String(e));
      await refresh();
    } finally {
      setWaiting(false);
      setBusy(false);
    }
  };
  if (!status) return <p className="muted">{error || t("desktop.checking")}</p>;
  return (
    <div className="stack" style={{ gap: 0 }}>
      <Setting title={t("desktop.service.status")} detail={t("desktop.service.status.detail")}>
        {waiting ? (
          <Chip className="static" icon={mdiProgressClock}>
            {t("desktop.service.waiting")}
          </Chip>
        ) : status.running ? (
          <div className="row">
            <Chip className="static" icon={mdiCheckCircleOutline} selected>
              {t("desktop.service.running")}
            </Chip>
            <Button small variant="tonal" disabled={!status.service || busy} onClick={() => act(() => restartService(version?.info?.pid ?? 0), true)}>
              {t("desktop.service.restart_short")}
            </Button>
            <Button small danger disabled={busy} onClick={() => act(() => stopService(version?.info?.pid ?? 0), false)}>
              {t("desktop.service.stop")}
            </Button>
          </div>
        ) : (
          <Chip className="static" icon={mdiAlertCircleOutline} tone="danger">
            {t("desktop.service.not_running")}
          </Chip>
        )}
      </Setting>
      <Setting title={t("desktop.service.login")} detail={status.installed ? t("desktop.service.login.installed") : t("desktop.service.login.not_installed")}>
        <Button
          variant="tonal"
          small
          disabled={!status.service || busy}
          onClick={() => act(installService, true)}
        >
          {status.installed ? t("desktop.service.reinstall") : t("desktop.service.install_short")}
        </Button>
      </Setting>
      <Setting title={t("desktop.service.tray")} detail={status.tray ? t("desktop.service.tray.detail") : t("desktop.service.tray.none")}>
        <Switch
          label={t("desktop.service.tray")}
          checked={!!status.tray_installed}
          disabled={!status.tray || busy}
          onChange={async (on) => {
            setBusy(true);
            setError("");
            try {
              await setTray(on);
            } catch (e) {
              setError(String(e));
            } finally {
              await refresh();
              setBusy(false);
            }
          }}
        />
      </Setting>
      {version && (
        <Setting title={t("desktop.service.version")} detail={version.stale ? t(`desktop.service.stale.${version.stale}`, { service: version.info?.version ?? "?", app: version.app, path: version.info?.executable ?? "" }) : undefined}>
          <span className={version.stale ? "error-text" : "muted"}>
            {version.info ? version.info.version : t("desktop.service.version_unknown")} · {t("desktop.service.app_version", { version: version.app })}
          </span>
        </Setting>
      )}
      <Setting title={t("desktop.service.socket")}>
        <code className="muted">{status.socket}</code>
      </Setting>
      <Setting title={t("desktop.service.program")} detail={t("desktop.service.program.detail")}>
        <code className="muted">{status.service || t("desktop.service.no_blitzd")}</code>
      </Setting>
      {error && <p className="error-text">{error}</p>}
    </div>
  );
}

function About() {
  const [version, setVersion] = useState("");
  useEffect(() => {
    appVersion().then(setVersion, () => setVersion("?"));
  }, []);
  return (
    <div className="stack">
      <span className="t-title-lg">{t("desktop.brand")}</span>
      <span className="muted">{t("desktop.about.version", { version })}</span>
      <p className="muted">{t("desktop.about.body", { file: "~/.blitz/desktop.json" })}</p>
      <div className="row wrap">
        <span className="t-body-sm muted">{t("desktop.about.license")}</span>
        <Button small icon={mdiScaleBalance} onClick={() => showLicense({ which: "notice" })}>
          {t("desktop.license.show")}
        </Button>
        <Button small onClick={() => showLicense({ which: "third-party" })}>
          {t("desktop.license.third_party")}
        </Button>
      </div>
    </div>
  );
}
