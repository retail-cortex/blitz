import { mdiClose, mdiCogOutline, mdiDeleteOutline, mdiLightningBolt, mdiMenu, mdiPencilOutline, mdiPlus } from "@mdi/js";
import { workspaceColor } from "./palette";
import { displayName, forgetWorkspace, openWorkspace, openWorkspaces, recentWorkspaces, type WorkspacePrefs } from "./prefs";
import { useApp } from "./state";
import { Icon, IconButton } from "./ui/controls";

/**
 * The navigation drawer: the open workspaces (closable), the recently
 * closed ones, and settings. It folds into a rail of avatars.
 */
export function Drawer({
  onOpen,
  onClose,
  onSettings,
  onEdit,
}: {
  onOpen: () => void;
  onClose: (dir: string) => void;
  onSettings: () => void;
  onEdit: (dir: string) => void;
}) {
  const { prefs, update, drawer } = useApp();
  const rail = drawer === "rail";
  const forced = rail && prefs.drawer === "open"; // a narrow window
  const open = openWorkspaces(prefs);
  const recent = recentWorkspaces(prefs);
  const show = (dir: string) => update((p) => openWorkspace(p, dir));

  return (
    <nav className="drawer" aria-label="Workspaces">
      <div className="drawer-top drag-region">
        <IconButton
          icon={mdiMenu}
          label={forced ? "Widen the window to expand the menu" : rail ? "Expand the menu" : "Collapse the menu"}
          disabled={forced}
          onClick={() => update((p) => ({ ...p, drawer: rail ? "open" : "rail" }))}
        />
        {!rail && (
          <span className="brand">
            <Icon path={mdiLightningBolt} className="brand-mark" />
            Blitz
          </span>
        )}
      </div>
      <div className="drawer-scroll">
        <button className={`new-workspace ${rail ? "fab" : ""}`} onClick={onOpen} title="Open a workspace">
          <Icon path={mdiPlus} />
          {!rail && <span>Open workspace</span>}
        </button>
        {open.length > 0 && !rail && <div className="drawer-label">Workspaces</div>}
        <div className="list">
          {open.map((w) => (
            <WorkspaceItem key={w.dir} w={w} rail={rail} active={w.dir === prefs.active} onShow={() => show(w.dir)} onClose={() => onClose(w.dir)} onEdit={() => onEdit(w.dir)} />
          ))}
        </div>
        {recent.length > 0 && !rail && (
          <>
            <div className="drawer-label">Recent</div>
            <div className="list">
              {recent.map((w) => (
                <div key={w.dir} className="list-item recent">
                  <button className="item-main" onClick={() => show(w.dir)} title={`Reopen ${w.dir}`}>
                    <span className="lines">
                      <span className="ellipsis">{displayName(w)}</span>
                    </span>
                  </button>
                  <span className="trailing hover-only">
                    <IconButton
                      icon={mdiDeleteOutline}
                      label="Forget"
                      small
                      onClick={() => update((p) => forgetWorkspace(p, w.dir))}
                    />
                  </span>
                </div>
              ))}
            </div>
          </>
        )}
      </div>
      <div className="drawer-bottom">
        <button className="list-item" onClick={onSettings} title="Settings">
          <Icon path={mdiCogOutline} />
          {!rail && <span>Settings</span>}
        </button>
      </div>
    </nav>
  );
}

function WorkspaceItem({
  w,
  rail,
  active,
  onShow,
  onClose,
  onEdit,
}: {
  w: WorkspacePrefs;
  rail: boolean;
  active: boolean;
  onShow: () => void;
  onClose: () => void;
  onEdit: () => void;
}) {
  const { theme, activity } = useApp();
  const a = activity[w.dir];
  const color = workspaceColor(w.color, theme);
  const name = displayName(w);
  const status = a?.waiting ? "Waiting for you" : a?.running ? "Working" : "";
  return (
    <div className={`list-item workspace-item ${active ? "active" : ""}`}>
      <button className="item-main" onClick={onShow} aria-current={active ? "page" : undefined} title={`${name}\n${w.dir}${status ? `\n${status}` : ""}`}>
        <span className="avatar" style={{ background: color }}>
          {name.slice(0, 1).toUpperCase()}
          {a?.waiting ? <span className="badge avatar-badge">!</span> : a?.running ? <span className="dot avatar-dot pulse" /> : null}
        </span>
        {!rail && (
          <span className="lines">
            <span className="ellipsis">{name}</span>
            <small className="ellipsis">{status || w.description || w.dir}</small>
          </span>
        )}
      </button>
      {!rail && (
        <>
          <span className="trailing hover-only">
            <IconButton
              icon={mdiPencilOutline}
              label="Edit details"
              small
              onClick={onEdit}
            />
            <IconButton
              icon={mdiClose}
              label={`Close ${name}`}
              small
              onClick={onClose}
            />
          </span>
        </>
      )}
    </div>
  );
}
