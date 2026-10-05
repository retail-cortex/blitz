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
  mdiRobotOutline,
  mdiArrowLeft,
  mdiFolderOpenOutline,
  mdiShieldCheckOutline,
  mdiInformationOutline,
  mdiLanguageMarkdownOutline,
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
import { appVersion, cliStatus, installCLI, installService, reinstallCLI, restartService, serviceStatus, setTray, stopService, type CLIStatus, type ServiceStatus } from "./desktop";
import { SandboxFixResult, sandboxDetail, useSandboxFix } from "./SandboxNotice";
import { configChanged, showLicense } from "./events";
import { checkService, settleTimeout, type ServiceCheck, waitForService, withTimeout } from "./serviceVersion";
import { languages, t } from "./i18n";
import { workspaceColor } from "./palette";
import { displayName, editWorkspace, forgetWorkspace, openWorkspace, type WorkspacePrefs } from "./prefs";
import { PermissionSettings } from "./PermissionSettings";
import { AgentEditor } from "./AgentEditor";
import { AgentScope } from "./gen/blitz/v1/workspace_pb";
import { LogViewer } from "./LogViewer";
import { ProviderSettings } from "./ProviderSettings";
import { SettingsFile } from "./settingsfile/SettingsFile";
import { DocumentSettings } from "./files/DocumentSettings";
import { useAdvanced, useApp } from "./state";
import { sectionShown, settingsSections, type SectionId } from "./simpleMode";
import type { ThemePref } from "./theme";
import { Button, Chip, Dialog, Icon, IconButton, Segmented, Switch } from "./ui/controls";
import { LookFields, WorkspaceSettingsForm } from "./WorkspaceSettingsForm";
import { useWorkspaceSettings } from "./workspaceSettings";

/** A section of the Settings dialog (simpleMode.ts lists them, and which a newcomer sees). */
export type Section = SectionId;

const sectionIcons: Record<Section, string> = {
  appearance: mdiPaletteOutline,
  documents: mdiLanguageMarkdownOutline,
  providers: mdiKeyChainVariant,
  permissions: mdiShieldCheckOutline,
  agents: mdiRobotOutline,
  file: mdiFileDocumentEditOutline,
  workspaces: mdiFolderMultipleOutline,
  service: mdiServerNetwork,
  logs: mdiTextBoxSearchOutline,
  about: mdiInformationOutline,
};

/**
 * The window's settings, opening on a section (initial) and, for
 * Workspaces and the settings file, on a workspace (scope): a workspace's
 * own settings are in Workspaces (its pencil in the Workspaces menu, and
 * the status bar's model, open them there).
 */
export function SettingsDialog({ onClose, initial = "appearance", scope: initialScope = "" }: { onClose: () => void; initial?: Section; scope?: string }) {
  const advanced = useAdvanced();
  const [asked, setSection] = useState<Section>(initial);
  const [scope, setScope] = useState(initialScope);
  // Simple mode shows what a newcomer needs (settingsSections); a section
  // hidden when advanced is turned off falls back to Appearance.
  const sections = settingsSections(advanced);
  const section = sectionShown(asked, advanced);
  return (
    <Dialog title={t("desktop.settings")} icon={mdiCogOutline} onClose={onClose} large footer={<Button onClick={onClose}>{t("desktop.done")}</Button>}>
      <div className="settings">
        <div className="settings-nav list" role="tablist">
          {sections.map((id) => (
            <button key={id} role="tab" aria-selected={id === section} data-autofocus={id === section ? "" : undefined} className={`list-item ${id === section ? "active" : ""}`} onClick={() => setSection(id)}>
              <Icon path={sectionIcons[id]} />
              {t(`desktop.settings.${id}`)}
            </button>
          ))}
        </div>
        <div className="settings-pane">
          {section === "appearance" && <Appearance />}
          {section === "documents" && <DocumentSettings />}
          {section === "providers" && <ProviderSettings workspace="" />}
          {section === "permissions" && <PermissionSettings workspace="" />}
          {section === "agents" && <UserAgents />}
          {section === "file" && <FileSection scope={scope} />}
          {section === "workspaces" && (
            <Workspaces
              initial={initial === "workspaces" ? initialScope : ""}
              onSettingsFile={(dir) => {
                setScope(dir);
                setSection("file");
              }}
            />
          )}
          {section === "service" && <Service />}
          {section === "logs" && <LogViewer />}
          {section === "about" && <About />}
        </div>
      </div>
    </Dialog>
  );
}

// The user's agents (~/.blitz/agents), offered the tools and models of
// the workspace shown, if one is.
function UserAgents() {
  const { prefs } = useApp();
  const active = prefs.workspaces.find((w) => w.dir === prefs.active && w.open)?.dir ?? "";
  return <AgentEditor workspace="" scope={AgentScope.USER} context={active} />;
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

// The window's look and behaviour. Simple mode shows the switch for
// advanced settings, theme, language and notifications; the rest have
// sensible defaults and show with advanced settings.
function Appearance() {
  const { prefs, update } = useApp();
  return (
    <div className="stack" style={{ gap: 0 }}>
      <Setting title={t("desktop.settings.advanced")} detail={t("desktop.settings.advanced.detail")}>
        <Switch label={t("desktop.settings.advanced")} checked={prefs.advanced} onChange={(advanced) => update((p) => ({ ...p, advanced }))} />
      </Setting>
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
      <Setting title={t("desktop.settings.notifications")} detail={t("desktop.settings.notifications.detail")}>
        <Switch label={t("desktop.settings.notifications")} checked={prefs.notifications === "on"} onChange={(on) => update((p) => ({ ...p, notifications: on ? "on" : "off" }))} />
      </Setting>
      {prefs.advanced && (
        <>
        <Setting title={t("desktop.settings.composer")} detail={t("desktop.settings.composer.detail")}>
          <Segmented
            label={t("desktop.settings.composer")}
            value={prefs.composer}
            onChange={(composer) => update((p) => ({ ...p, composer }))}
            options={[
              { value: "pinned", label: t("desktop.settings.composer.pinned") },
              { value: "panel", label: t("desktop.settings.composer.panel") },
            ]}
          />
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
        <Setting title={t("desktop.settings.task_continue")} detail={t("desktop.settings.task_continue.detail")}>
          <Switch label={t("desktop.settings.task_continue")} checked={prefs.task_continue} onChange={(task_continue) => update((p) => ({ ...p, task_continue }))} />
        </Setting>
        <p className="t-body-sm muted" style={{ marginTop: 16 }}>
          <Icon path={mdiBrain} size="sm" /> {t("desktop.settings.per_workspace")}
        </p>
        </>
      )}
    </div>
  );
}

// The settings file of the global scope or of an open or known workspace.
function FileSection({ scope }: { scope: string }) {
  const { prefs } = useApp();
  return <SettingsFile scopes={prefs.workspaces.map((w) => ({ dir: w.dir, name: displayName(w) }))} initial={scope} />;
}

// The workspaces the window knows. The pencil opens one's settings here
// (the Workspaces menu's and the status bar's open them on one): for an
// open workspace every setting; for a closed one how it's shown, and Open.
function Workspaces({ initial, onSettingsFile }: { initial: string; onSettingsFile: (dir: string) => void }) {
  const { prefs, update, theme } = useApp();
  const [editing, setEditing] = useState<string | null>(initial || null);
  const ws = prefs.workspaces.find((w) => w.dir === editing);
  if (ws) return <WorkspaceEdit ws={ws} onBack={() => setEditing(null)} onSettingsFile={onSettingsFile} />;
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
            <IconButton icon={mdiPencilOutline} label={t("desktop.menu.workspaces.edit", { name: displayName(w) })} small onClick={() => setEditing(w.dir)} />
          </span>
        </div>
      ))}
    </div>
  );
}

function WorkspaceEdit({ ws, onBack, onSettingsFile }: { ws: WorkspacePrefs; onBack: () => void; onSettingsFile: (dir: string) => void }) {
  const { update, theme } = useApp();
  return (
    <div className="stack ws-edit" style={{ gap: 12 }}>
      <div className="row" style={{ gap: 12 }}>
        <IconButton icon={mdiArrowLeft} label={t("desktop.settings.back_to_workspaces")} onClick={onBack} />
        <span className="avatar" style={{ background: workspaceColor(ws.color, theme) }}>
          {displayName(ws).slice(0, 1).toUpperCase()}
        </span>
        <span className="stack spacer" style={{ gap: 2, minWidth: 0 }}>
          <span className="t-title ellipsis">{displayName(ws)}</span>
          <small className="ellipsis mono muted">{ws.dir}</small>
        </span>
      </div>
      {ws.open ? (
        <OpenWorkspaceSettings dir={ws.dir} onSettingsFile={() => onSettingsFile(ws.dir)} />
      ) : (
        <div className="stack" style={{ gap: 16 }}>
          <LookFields ws={ws} onSave={(look) => update((p) => editWorkspace(p, ws.dir, look))} />
          <div className="card row" style={{ gap: 12 }}>
            <span className="spacer t-body-sm">{t("desktop.settings.closed_workspace")}</span>
            <Button small variant="tonal" icon={mdiFolderOpenOutline} onClick={() => update((p) => openWorkspace(p, ws.dir))}>
              {t("desktop.settings.open_workspace")}
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}

// An open workspace's settings; what changes here is told to the window
// (configChanged), so the status bar and the chat see it.
function OpenWorkspaceSettings({ dir, onSettingsFile }: { dir: string; onSettingsFile: () => void }) {
  const { settings, settingsError, refreshSettings } = useWorkspaceSettings(dir);
  return (
    <div className="ws-settings-embedded">
      <WorkspaceSettingsForm
        dir={dir}
        settings={settings}
        error={settingsError}
        onChanged={() => {
          void refreshSettings();
          configChanged({ dir });
        }}
        onSettingsFile={onSettingsFile}
      />
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
      <CommandLine />
      <OSSandbox />
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

/**
 * The OS sandbox for the agent's commands (Linux): whether bubblewrap runs,
 * and Allow when AppArmor's restriction stops it, which installs a profile
 * for bwrap alone with the system's password dialog. Not shown elsewhere.
 */
function OSSandbox() {
  const { status, phase, fix, restartError, allow } = useSandboxFix();
  if (status === undefined || status?.state === "unsupported") return null;
  return (
    <>
      <Setting title={t("desktop.sandbox")} detail={sandboxDetail(status)}>
        {status?.state === "ready" ? (
          <Chip className="static" icon={mdiCheckCircleOutline} selected>
            {t("desktop.sandbox.on")}
          </Chip>
        ) : status?.state === "restricted" ? (
          <Button small variant="tonal" disabled={phase === "fixing" || phase === "restarting"} onClick={allow}>
            {t("desktop.sandbox.allow")}
          </Button>
        ) : status ? (
          <Chip className="static" icon={mdiAlertCircleOutline} tone="danger">
            {t("desktop.sandbox.off")}
          </Chip>
        ) : null}
      </Setting>
      <SandboxFixResult phase={phase} fix={fix} restartError={restartError} />
    </>
  );
}

/**
 * The blitz command: whether a terminal finds it, and Install, which links
 * the app's onto the user's PATH when no blitz is there, or Reinstall when
 * one is (another build's link, or dead links left on a developer's
 * machine). Not in a browser.
 */
function CommandLine() {
  const [status, setStatus] = useState<CLIStatus | undefined | null>(null); // null: checking
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState("");
  const [error, setError] = useState("");
  useEffect(() => {
    cliStatus().then(setStatus, (e) => setError(String(e)));
  }, []);
  if (status === undefined) return null;
  const install = async (again: boolean) => {
    setBusy(true);
    setError("");
    try {
      const done = again ? await reinstallCLI() : await installCLI();
      const linked = done.profile ? t("desktop.cli.installed_profile", { link: done.link, profile: done.profile }) : t("desktop.cli.installed", { link: done.link });
      setNote(done.removed?.length ? `${linked} ${t("desktop.cli.removed", { links: done.removed.join(", ") })}` : linked);
      setStatus(await cliStatus());
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };
  const detail =
    status === null
      ? t("desktop.checking")
      : !status.cli
        ? t("desktop.cli.none")
        : status.installed
          ? t("desktop.cli.on_path", { path: status.on_path })
          : status.on_path
            ? t("desktop.cli.other", { path: status.on_path })
            : t("desktop.cli.detail");
  return (
    <>
      <Setting title={t("desktop.cli")} detail={detail}>
        {status && status.cli && (
          <Button small variant="tonal" disabled={busy} onClick={() => void install(!!status.on_path)} title={status.on_path ? t("desktop.cli.reinstall_hint") : undefined}>
            {status.on_path ? t("desktop.service.reinstall") : t("desktop.service.install_short")}
          </Button>
        )}
      </Setting>
      {note && <p className="muted">{note}</p>}
      {error && <p className="error-text">{error}</p>}
    </>
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
