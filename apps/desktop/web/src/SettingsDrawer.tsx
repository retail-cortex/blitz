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
  mdiXml,
  mdiCogOutline,
  mdiFolderMultipleOutline,
  mdiKeyChainVariant,
  mdiKeyVariant,
  mdiProgressClock,
  mdiPuzzleOutline,
  mdiRobotOutline,
  mdiArrowLeft,
  mdiFolderOpenOutline,
  mdiShieldCheckOutline,
  mdiInformationOutline,
  mdiLanguageMarkdownOutline,
  mdiMonitor,
  mdiPaletteOutline,
  mdiPencilOutline,
  mdiPowerPlugOutline,
  mdiConnection,
  mdiScaleBalance,
  mdiServerNetwork,
  mdiTextBoxSearchOutline,
  mdiAlertCircleOutline,
  mdiWeatherNight,
  mdiWhiteBalanceSunny,
  mdiCalendarClock,
} from "@mdi/js";
import { appVersion, canUpdate, checkUpdate, cliStatus, downloadUpdate, installCLI, isLinuxApp, installService, openURL, reinstallCLI, restartService, serviceStatus, setTray, stopService, type CLIStatus, type ServiceStatus, type UpdateStatus } from "./desktop";
import { restart, shouldDownload, updateLine } from "./updates";
import { config } from "./api";
import { message } from "./errors";
import { SandboxFixResult, sandboxDetail, useSandboxFix } from "./SandboxNotice";
import { configChanged, showLicense } from "./events";
import { useStopGuard } from "./stopGuard";
import { checkService, settleTimeout, type ServiceCheck, shownVersion, waitForService, withTimeout } from "./serviceVersion";
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
import { McpSettings } from "./McpSettings";
import { ConnectorsPane } from "./studio/ConnectorsPane";
import { useDiscardGuard } from "./studio/paneParts";
import { SecretsEditor } from "./SecretsEditor";
import { SkillSettings } from "./SkillSettings";
import { WorkersSettings } from "./WorkersSettings";
import { refreshTasks, useTasks, type TaskAlerts } from "./humanTasks";
import { useAdvanced, useApp } from "./state";
import { sectionShown, shownGroups, type SectionId } from "./simpleMode";
import { paletteOf, type ThemePref } from "./theme";
import { darkThemes, lightThemes, type DarkThemeId, type LightThemeId } from "./themes";
import { Button, Chip, Dialog, Icon, IconButton, Segmented, Switch, useSnackbar } from "./ui/controls";
import { Drawer } from "./ui/Drawer";
import { LookFields, WorkspaceSettingsForm } from "./WorkspaceSettingsForm";
import { useWorkspaceSettings } from "./workspaceSettings";

/**
 * What Settings opens on: a section (simpleMode.ts lists them, and which a
 * newcomer sees), or "file", the settings file on the other side of them.
 */
export type Section = SectionId | "file";

const sectionIcons: Record<SectionId, string> = {
  providers: mdiKeyChainVariant,
  secrets: mdiKeyVariant,
  appearance: mdiPaletteOutline,
  agents: mdiRobotOutline,
  skills: mdiPuzzleOutline,
  workers: mdiCalendarClock,
  mcp: mdiPowerPlugOutline,
  connectors: mdiConnection,
  workspaces: mdiFolderMultipleOutline,
  permissions: mdiShieldCheckOutline,
  service: mdiServerNetwork,
  logs: mdiTextBoxSearchOutline,
  about: mdiInformationOutline,
};

/**
 * The window's settings, opening on a section (initial) and, for
 * Workspaces and the settings file, on a workspace (scope): a workspace's
 * own settings are in Workspaces (its pencil in the Workspaces menu, and
 * the status bar's model, open them there). The title bar holds Show
 * advanced settings and, with them, the settings file (</>): Settings turns
 * over to show it, as a page's source, and back.
 */
export function SettingsDrawer({ onClose, initial = "providers", scope: initialScope = "" }: { onClose: () => void; initial?: Section; scope?: string }) {
  const advanced = useAdvanced();
  const { update } = useApp();
  const [asked, setSection] = useState<SectionId>(initial === "file" ? "providers" : initial);
  // Whether a section's form holds unsaved changes (Connectors): closing
  // or picking another section asks first, as Studio does.
  const [dirty, setDirty] = useState(false);
  const { leave, discard } = useDiscardGuard(dirty);
  const pick = (id: SectionId) =>
    leave(() => {
      setDirty(false);
      setSection(id);
    });
  const [scope, setScope] = useState(initialScope);
  const flip = useFlip(initial === "file");
  // Simple mode shows what a newcomer needs (settingsSections), in their
  // groups; a section hidden when advanced is turned off falls back to the
  // first.
  const groups = shownGroups(advanced);
  const section = sectionShown(asked, advanced);
  const showFile = flip.face === "file" && advanced;
  const actions = (
    <>
      <label className="row drawer-switch t-label" title={t("desktop.settings.advanced.detail")}>
        {t("desktop.settings.advanced.short")}
        <Switch label={t("desktop.settings.advanced")} checked={advanced} onChange={(on) => update((p) => ({ ...p, advanced: on }))} />
      </label>
      {advanced && (
        <IconButton
          icon={mdiXml}
          label={t(showFile ? "desktop.settings.file.back" : "desktop.settings.file.show")}
          selected={showFile}
          onClick={() => flip.turn(showFile ? "settings" : "file")}
        />
      )}
    </>
  );
  return (
    <Drawer title={t("desktop.settings")} icon={mdiCogOutline} onClose={onClose} className="settings-drawer" headerActions={actions} dirty={dirty}>
      <div className={`settings-flip ${flip.turning}`} onAnimationEnd={flip.turned}>
        <div className="settings" hidden={showFile}>
          <div className="settings-nav list" role="tablist">
            {groups.flatMap((g) => [
              // Headings with advanced settings, when there are enough sections to need them.
              ...(advanced ? [<span key={`group-${g.id}`} role="presentation" className="settings-nav-group">{t(`desktop.settings.group.${g.id}`)}</span>] : []),
              ...g.sections.map((id) => (
              <button
                key={id}
                role="tab"
                aria-selected={id === section}
                data-autofocus={id === section && !showFile ? "" : undefined}
                className={`list-item ${id === section ? "active" : ""}`}
                title={t(`desktop.settings.${id}`)}
                onClick={() => (id === section ? undefined : pick(id))}
              >
                <Icon path={sectionIcons[id]} />
                <span className="settings-nav-label">{t(`desktop.settings.${id}`)}</span>
              </button>
            )),
            ])}
          </div>
          <div className="settings-pane">
            {section === "providers" && <ProviderSettings workspace="" />}
            {section === "secrets" && <SecretsEditor />}
            {section === "appearance" && <Appearance />}
            {section === "agents" && <UserAgents />}
            {section === "skills" && <SkillSettings />}
            {section === "workers" && <WorkersSettings onLeave={onClose} worker={scope} />}
            {section === "mcp" && <McpSettings />}
            {section === "connectors" && (
              <div className="slide-box">
                <ConnectorsPane onDirty={setDirty} />
              </div>
            )}
            {section === "workspaces" && (
              <Workspaces
                initial={initial === "workspaces" ? initialScope : ""}
                onSettingsFile={(dir) => {
                  setScope(dir);
                  flip.turn("file");
                }}
              />
            )}
            {section === "permissions" && <PermissionSettings workspace="" />}
            {section === "service" && <Service />}
            {section === "logs" && <LogViewer />}
            {section === "about" && <About />}
          </div>
        </div>
        {/* Kept once shown, so turning back and forth keeps what's typed. */}
        {advanced && flip.shown && (
          <div className="settings-file-side" hidden={!showFile}>
            <FileSection key={scope} scope={scope} />
          </div>
        )}
      </div>
      {discard(t(`desktop.settings.${section}`))}
    </Drawer>
  );
}

/**
 * Settings' two sides: the sections and the settings file. Turning goes
 * out (a quarter turn), changes side and comes back in; with reduced
 * motion it changes side at once. shown: the file's side has been shown.
 */
function useFlip(file: boolean) {
  const [face, setFace] = useState<"settings" | "file">(file ? "file" : "settings");
  const [next, setNext] = useState<"settings" | "file" | null>(null);
  const [turning, setTurning] = useState<"" | "flip-out" | "flip-in">("");
  const [shown, setShown] = useState(file);
  const turn = (to: "settings" | "file") => {
    if (to === face && !next) return;
    if (to === "file") setShown(true);
    if (window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) {
      setFace(to);
      return;
    }
    setNext(to);
    setTurning("flip-out");
  };
  const turned = (e: React.AnimationEvent) => {
    if (e.target !== e.currentTarget) return;
    if (turning === "flip-out" && next) {
      setFace(next);
      setNext(null);
      setTurning("flip-in");
    } else setTurning("");
  };
  return { face, turning, shown, turn, turned };
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
/**
 * How workflows' human tasks reach the user ([ui] task_alerts, in the
 * user's settings file, which the tray reads too): a notification, the
 * tasks at once, or nothing.
 */
function TaskAlertsSetting() {
  const { alerts, loaded } = useTasks();
  const snack = useSnackbar();
  const set = async (value: TaskAlerts) => {
    try {
      await config.setConfigValue({ workspace: "", key: "ui.task_alerts", value });
      await refreshTasks();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  return (
    <Setting title={t("desktop.settings.task_alerts")} detail={t("desktop.settings.task_alerts.detail")}>
      <select className="select language-select" aria-label={t("desktop.settings.task_alerts")} value={alerts} disabled={!loaded} onChange={(e) => void set(e.target.value as TaskAlerts)}>
        {(["notify", "open", "off"] as const).map((v) => (
          <option key={v} value={v}>
            {t(`desktop.settings.task_alerts.${v}`)}
          </option>
        ))}
      </select>
    </Setting>
  );
}

function Appearance() {
  const { prefs, update, theme } = useApp();
  const palette = paletteOf(theme, prefs.light_theme, prefs.dark_theme);
  const [documents, setDocuments] = useState(false);
  return (
    <div className="stack" style={{ gap: 0 }}>
      {documents && (
        <Dialog title={t("desktop.settings.documents")} icon={mdiLanguageMarkdownOutline} onClose={() => setDocuments(false)} large closeButton>
          <DocumentSettings />
        </Dialog>
      )}
      <Setting title={t("desktop.settings.theme")} detail={t("desktop.settings.theme.detail")}>
        <Segmented<ThemePref>
          equal
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
      {/* The colour theme for each, as VS Code's preferred light and dark themes. */}
      <Setting title={t("desktop.settings.light_theme")} detail={t("desktop.settings.light_theme.detail")}>
        <select className="select language-select" aria-label={t("desktop.settings.light_theme")} value={prefs.light_theme} onChange={(e) => update((p) => ({ ...p, light_theme: e.target.value as LightThemeId }))}>
          {lightThemes.map((x) => (
            <option key={x.id} value={x.id}>
              {x.name}
            </option>
          ))}
        </select>
      </Setting>
      <Setting title={t("desktop.settings.dark_theme")} detail={t("desktop.settings.dark_theme.detail")}>
        <select className="select language-select" aria-label={t("desktop.settings.dark_theme")} value={prefs.dark_theme} onChange={(e) => update((p) => ({ ...p, dark_theme: e.target.value as DarkThemeId }))}>
          {darkThemes.map((x) => (
            <option key={x.id} value={x.id}>
              {x.name}
            </option>
          ))}
        </select>
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
      <TaskAlertsSetting />
      {/* The Markdown previews' styles, in a dialog of their own. */}
      <Setting title={t("desktop.settings.documents")} detail={t("desktop.settings.documents.detail")}>
        <Button variant="outlined" icon={mdiLanguageMarkdownOutline} onClick={() => setDocuments(true)}>
          {t("desktop.settings.documents.edit")}
        </Button>
      </Setting>
      {prefs.advanced && (
        <>
        <Setting title={t("desktop.settings.composer")} detail={t("desktop.settings.composer.detail")}>
          <Segmented
            equal
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
            equal
            label={t("desktop.settings.density")}
            value={prefs.density}
            onChange={(density) => update((p) => ({ ...p, density }))}
            options={[
              { value: "comfortable", label: t("desktop.settings.density.comfortable") },
              { value: "compact", label: t("desktop.settings.density.compact") },
            ]}
          />
        </Setting>
        <Setting title={t("desktop.settings.transparency")} detail={palette === "blitz" ? t("desktop.settings.transparency.detail") : t("desktop.settings.transparency.blitz_only")}>
          <Switch label={t("desktop.settings.transparency")} disabled={palette !== "blitz"} checked={prefs.transparency === "on" && palette === "blitz"} onChange={(on) => update((p) => ({ ...p, transparency: on ? "on" : "off" }))} />
        </Setting>
        {isLinuxApp() && (
          <Setting title={t("desktop.settings.gpu")} detail={t("desktop.settings.gpu.detail")}>
            <Switch label={t("desktop.settings.gpu")} checked={prefs.gpu === "on"} onChange={(on) => update((p) => ({ ...p, gpu: on ? "on" : "off" }))} />
          </Setting>
        )}
        <Setting title={t("desktop.settings.width")} detail={t("desktop.settings.width.detail")}>
          <Segmented
            equal
            label={t("desktop.settings.width")}
            value={prefs.width}
            onChange={(width) => update((p) => ({ ...p, width }))}
            options={[
              { value: "full", label: t("desktop.settings.width.full") },
              { value: "readable", label: t("desktop.settings.width.readable") },
            ]}
          />
        </Setting>
        <Setting title={t("desktop.settings.doc_width")} detail={t("desktop.settings.doc_width.detail")}>
          <Segmented
            equal
            label={t("desktop.settings.doc_width")}
            value={prefs.doc_width}
            onChange={(doc_width) => update((p) => ({ ...p, doc_width }))}
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
  const { guard, dialog: stopping } = useStopGuard();
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
      {stopping}
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
            <Button small variant="tonal" disabled={!status.service || busy} onClick={() => guard(() => void act(() => restartService(version?.info?.pid ?? 0), true))}>
              {t("desktop.service.restart_short")}
            </Button>
            <Button small danger disabled={busy} onClick={() => guard(() => void act(() => stopService(version?.info?.pid ?? 0), false))}>
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
          onClick={() => guard(() => void act(installService, true))}
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
        <Setting title={t("desktop.service.version")} detail={version.stale ? t(`desktop.service.stale.${version.stale}`, { service: shownVersion(version.info?.version ?? "?"), app: shownVersion(version.app), path: version.info?.executable ?? "" }) : undefined}>
          <span className={version.stale ? "error-text" : "muted"}>
            {version.info ? shownVersion(version.info.version) : t("desktop.service.version_unknown")}
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
/** The folder of a Windows or Unix path. */
const folderOf = (p: string) => p.replace(/[\\/][^\\/]*$/, "");

function CommandLine() {
  const [status, setStatus] = useState<CLIStatus | undefined | null>(null); // null: checking
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState("");
  const [error, setError] = useState("");
  useEffect(() => {
    cliStatus().then(setStatus, (e) => setError(String(e)));
  }, []);
  if (status === undefined) return null;
  const byPath = status?.method === "path";
  const install = async (again: boolean) => {
    setBusy(true);
    setError("");
    try {
      const done = again ? await reinstallCLI() : await installCLI();
      // Windows puts the folder on PATH (no links): its own words.
      const linked = byPath
        ? done.profile
          ? t("desktop.cli.installed_path", { folder: folderOf(done.link) })
          : t("desktop.cli.found_path", { path: done.link })
        : done.profile
          ? t("desktop.cli.installed_profile", { link: done.link, profile: done.profile })
          : t("desktop.cli.installed", { link: done.link });
      const removed = done.removed?.length ? (byPath ? t("desktop.cli.removed_path", { folders: done.removed.join(", ") }) : t("desktop.cli.removed", { links: done.removed.join(", ") })) : "";
      setNote(removed ? `${linked} ${removed}` : linked);
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
            ? t(byPath ? "desktop.cli.other_path" : "desktop.cli.other", { path: status.on_path })
            : t(byPath ? "desktop.cli.detail_path" : "desktop.cli.detail");
  return (
    <>
      <Setting title={t("desktop.cli")} detail={detail}>
        {status && status.cli && (
          <Button small variant="tonal" disabled={busy} onClick={() => void install(!!status.on_path)} title={status.on_path ? t(byPath ? "desktop.cli.reinstall_hint_path" : "desktop.cli.reinstall_hint") : undefined}>
            {status.on_path ? t("desktop.service.reinstall") : t("desktop.service.install_short")}
          </Button>
        )}
      </Setting>
      {note && <p className="muted">{note}</p>}
      {error && <p className="error-text">{error}</p>}
    </>
  );
}

/**
 * The app's update (spec_electron_038 EL-90): where it is, Check now,
 * Download or Restart to update, the release's notes, and whether it's
 * looked for when the app opens.
 */
function Updates() {
  const { prefs, update } = useApp();
  const snack = useSnackbar();
  const [status, setStatus] = useState<UpdateStatus | null>(null);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    checkUpdate(false).then(setStatus, (e) => setStatus({ current: "", latest: "", available: false, can: true, downloading: false, ready: false, error: String(e) }));
  }, []);
  const act = async (f: () => Promise<UpdateStatus>) => {
    setBusy(true);
    try {
      setStatus(await f());
    } catch (e) {
      snack(String(e instanceof Error ? e.message : e), { error: true });
    } finally {
      setBusy(false);
    }
  };
  const line = updateLine(busy && status?.available && !status.ready ? { ...status, downloading: true } : status);
  return (
    <>
      <Setting title={t("desktop.update.title")} detail={t(line.key, line.vars)}>
        <div className="row wrap">
          {status?.notes && status.available && (
            <Button small onClick={() => void openURL(status.notes!)}>
              {t("desktop.update.notes")}
            </Button>
          )}
          {status?.ready ? (
            <Button small variant="filled" onClick={() => void restart((m) => snack(m, { error: true }))}>
              {t("desktop.update.restart")}
            </Button>
          ) : status && shouldDownload(status) ? (
            <Button small variant="tonal" disabled={busy} onClick={() => void act(downloadUpdate)}>
              {t("desktop.update.download")}
            </Button>
          ) : (
            <Button small disabled={busy || !status?.can} onClick={() => void act(() => checkUpdate(true))}>
              {t("desktop.update.check")}
            </Button>
          )}
        </div>
      </Setting>
      {status?.can && (
        <Setting title={t("desktop.update.auto")} detail={t("desktop.update.auto_detail")}>
          <Switch label={t("desktop.update.auto")} checked={prefs.updates !== "off"} onChange={(on) => update((p) => ({ ...p, updates: on ? "auto" : "off" }))} />
        </Setting>
      )}
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
      <span className="muted">{t("desktop.about.version", { version: shownVersion(version) })}</span>
      {canUpdate() && <Updates />}
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
