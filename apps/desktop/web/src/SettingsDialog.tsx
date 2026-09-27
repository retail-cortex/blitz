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

import { useEffect, useState } from "react";
import {
  mdiBrain,
  mdiCheckCircleOutline,
  mdiCogOutline,
  mdiFileDocumentEditOutline,
  mdiFolderMultipleOutline,
  mdiKeyChainVariant,
  mdiInformationOutline,
  mdiMonitor,
  mdiPaletteOutline,
  mdiPencilOutline,
  mdiServerNetwork,
  mdiAlertCircleOutline,
  mdiWeatherNight,
  mdiWhiteBalanceSunny,
} from "@mdi/js";
import { appVersion, installService, restartService, serviceStatus, stopService, type ServiceStatus } from "./desktop";
import { checkService, type ServiceCheck } from "./serviceVersion";
import { languages, t } from "./i18n";
import { workspaceColor } from "./palette";
import { displayName, forgetWorkspace } from "./prefs";
import { ProviderSettings, SettingsFile } from "./ProviderSettings";
import { useApp } from "./state";
import type { ThemePref } from "./theme";
import { Button, Chip, Dialog, Icon, IconButton, Segmented, Switch } from "./ui/controls";
import { WorkspaceDialog } from "./WorkspaceDialog";

type Section = "appearance" | "providers" | "file" | "workspaces" | "service" | "about";

const sectionIcons: Record<Section, string> = {
  appearance: mdiPaletteOutline,
  providers: mdiKeyChainVariant,
  file: mdiFileDocumentEditOutline,
  workspaces: mdiFolderMultipleOutline,
  service: mdiServerNetwork,
  about: mdiInformationOutline,
};

/** The window's settings. The agent's settings are per workspace, in its run settings panel. */
export function SettingsDialog({ onClose }: { onClose: () => void }) {
  const [section, setSection] = useState<Section>("appearance");
  return (
    <Dialog title={t("desktop.settings")} icon={mdiCogOutline} onClose={onClose} wide footer={<Button onClick={onClose}>{t("desktop.done")}</Button>}>
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
          {section === "file" && <FileSection />}
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
      <Setting title={t("desktop.settings.thinking")} detail={t("desktop.settings.thinking.detail")}>
        <Switch label={t("desktop.settings.thinking")} checked={prefs.show_thoughts} onChange={(show_thoughts) => update((p) => ({ ...p, show_thoughts }))} />
      </Setting>
      <Setting title={t("desktop.settings.notifications")} detail={t("desktop.settings.notifications.detail")}>
        <Switch label={t("desktop.settings.notifications")} checked={prefs.notifications === "on"} onChange={(on) => update((p) => ({ ...p, notifications: on ? "on" : "off" }))} />
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
            <IconButton icon={mdiPencilOutline} label={t("desktop.edit_details")} small onClick={() => setEditing(w.dir)} />
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
  const [version, setVersion] = useState<ServiceCheck | null>(null);
  const refresh = () =>
    serviceStatus().then((s) => {
      setStatus(s);
      if (s.running) appVersion().then(checkService).then(setVersion, () => setVersion(null));
    }, (e) => setError(String(e)));
  useEffect(() => {
    refresh();
  }, []);
  // Runs one service action, then shows the new status.
  const act = async (f: () => Promise<void>) => {
    setBusy(true);
    setError("");
    try {
      await f();
      await refresh();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };
  if (!status) return <p className="muted">{error || t("desktop.checking")}</p>;
  return (
    <div className="stack" style={{ gap: 0 }}>
      <Setting title={t("desktop.service.status")} detail={t("desktop.service.status.detail")}>
        {status.running ? (
          <div className="row">
            <Chip className="static" icon={mdiCheckCircleOutline} selected>
              {t("desktop.service.running")}
            </Chip>
            <Button small variant="tonal" disabled={!status.service || busy} onClick={() => act(() => restartService(version?.info?.pid ?? 0))}>
              {t("desktop.service.restart_short")}
            </Button>
            <Button small danger disabled={busy} onClick={() => act(() => stopService(version?.info?.pid ?? 0))}>
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
          onClick={() => act(installService)}
        >
          {status.installed ? t("desktop.service.reinstall") : t("desktop.service.install_short")}
        </Button>
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
      <p className="t-body-sm muted">{t("desktop.about.license")}</p>
    </div>
  );
}
