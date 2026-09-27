import { useCallback, useEffect, useState } from "react";
import { mdiCalendarClock, mdiChatOutline, mdiClose, mdiDotsVertical, mdiFileCompare, mdiPencilOutline, mdiTuneVariant } from "@mdi/js";
import { workspaces } from "./api";
import { Changes } from "./Changes";
import { Conversation } from "./Conversation";
import { viewEvent, type ViewDetail } from "./events";
import { message } from "./errors";
import type { GetSettingsResponse } from "./gen/blitz/v1/workspace_pb";
import { workspaceColor } from "./palette";
import { displayName, type WorkspacePrefs } from "./prefs";
import { RunSettings } from "./RunSettings";
import { useApp } from "./state";
import { IconButton, Menu, Segmented } from "./ui/controls";
import { Workers } from "./Workers";

type View = "chat" | "changes" | "workers";

/**
 * One open workspace: its top bar, the chat, changes or workers view, and
 * the run settings panel. Kept mounted while another is shown, so a turn
 * keeps streaming.
 */
export function Workspace({ ws, visible, onEdit, onClose }: { ws: WorkspacePrefs; visible: boolean; onEdit: () => void; onClose: () => void }) {
  const { prefs, update, theme } = useApp();
  const [view, setView] = useState<View>("chat");
  const [settings, setSettings] = useState<GetSettingsResponse>();
  const [modelProblem, setModelProblem] = useState("");
  const [settingsError, setSettingsError] = useState("");
  const dir = ws.dir;

  const refreshSettings = useCallback(async () => {
    try {
      const [s, m] = await Promise.all([workspaces.getSettings({ workspace: dir }), workspaces.getModel({ workspace: dir })]);
      setSettings(s);
      setModelProblem(m.unavailable);
      setSettingsError("");
    } catch (e) {
      setSettingsError(message(e));
    }
  }, [dir]);
  useEffect(() => {
    refreshSettings();
  }, [refreshSettings]);

  // The command palette can switch the view.
  useEffect(() => {
    const f = (e: Event) => {
      const d = (e as CustomEvent<ViewDetail>).detail;
      if (d.dir === dir) setView(d.view);
    };
    window.addEventListener(viewEvent, f);
    return () => window.removeEventListener(viewEvent, f);
  }, [dir]);

  const name = displayName(ws);
  return (
    <section className="workspace" hidden={!visible} aria-label={name} style={{ ["--ws-color" as string]: workspaceColor(ws.color, theme) }}>
      <header className="topbar drag-region">
        <span className="ws-dot" />
        <div className="topbar-title">
          <button className="title-button no-drag" onClick={onEdit} title="Edit the workspace's details">
            <span className="t-title-lg ellipsis">{name}</span>
            <IconButtonGlyph />
          </button>
          <span className="t-body-sm muted ellipsis" title={dir}>
            {ws.description || dir}
          </span>
        </div>
        <div className="no-drag">
          <Segmented<View>
            label="View"
            small
            value={view}
            onChange={setView}
            options={[
              { value: "chat", label: "Chat", icon: mdiChatOutline },
              { value: "changes", label: "Changes", icon: mdiFileCompare },
              { value: "workers", label: "Workers", icon: mdiCalendarClock },
            ]}
          />
        </div>
        <div className="topbar-actions no-drag">
          <IconButton icon={mdiTuneVariant} label="Run settings" selected={prefs.run_settings} onClick={() => update((p) => ({ ...p, run_settings: !p.run_settings }))} />
          <Menu
            placement="down end"
            trigger={(p) => <IconButton icon={mdiDotsVertical} label="More" {...p} />}
            items={[
              { label: "Edit details", icon: mdiPencilOutline, onSelect: onEdit },
              { label: "Close workspace", icon: mdiClose, onSelect: onClose },
            ]}
          />
        </div>
      </header>
      <div className="workspace-body">
        <div className="workspace-view">
          <div hidden={view !== "chat"} className="view-fill">
            <Conversation
              dir={dir}
              name={name}
              visible={visible && view === "chat"}
              settings={settings}
              modelProblem={modelProblem}
              onSettingsChanged={refreshSettings}
              onOpenView={setView}
            />
          </div>
          {view === "changes" && <Changes dir={dir} />}
          {view === "workers" && <Workers dir={dir} />}
        </div>
        {prefs.run_settings && <RunSettings dir={dir} settings={settings} error={settingsError} onChanged={refreshSettings} />}
      </div>
    </section>
  );
}

// A small pencil that shows on hover beside the workspace's name.
function IconButtonGlyph() {
  return (
    <svg className="icon sm title-edit" viewBox="0 0 24 24" aria-hidden="true">
      <path d={mdiPencilOutline} />
    </svg>
  );
}
