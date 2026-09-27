import { mdiClose, mdiCogOutline, mdiDeleteOutline, mdiLightningBolt, mdiMenu, mdiPencilOutline, mdiPlus } from "@mdi/js";
import { workspaceColor } from "./palette";
import { displayName, forgetWorkspace, openWorkspace, openWorkspaces, recentWorkspaces, type WorkspacePrefs } from "./prefs";
import { useApp } from "./state";
import { t } from "./i18n";
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
    <nav className="drawer" aria-label={t("desktop.drawer.label")}>
      <div className="drawer-top drag-region">
        <IconButton
          icon={mdiMenu}
          label={forced ? t("desktop.drawer.menu_forced") : rail ? t("desktop.drawer.menu_expand") : t("desktop.drawer.menu_collapse")}
          disabled={forced}
          onClick={() => update((p) => ({ ...p, drawer: rail ? "open" : "rail" }))}
        />
        {!rail && (
          <span className="brand">
            <Icon path={mdiLightningBolt} className="brand-mark" />
            {t("desktop.brand")}
          </span>
        )}
      </div>
      <div className="drawer-scroll">
        <button className={`new-workspace ${rail ? "fab" : ""}`} onClick={onOpen} title={t("desktop.workspace.open_one")}>
          <Icon path={mdiPlus} />
          {!rail && <span>{t("desktop.drawer.open")}</span>}
        </button>
        {open.length > 0 && !rail && <div className="drawer-label">{t("desktop.drawer.label")}</div>}
        <div className="list">
          {open.map((w) => (
            <WorkspaceItem key={w.dir} w={w} rail={rail} active={w.dir === prefs.active} onShow={() => show(w.dir)} onClose={() => onClose(w.dir)} onEdit={() => onEdit(w.dir)} />
          ))}
        </div>
        {recent.length > 0 && !rail && (
          <>
            <div className="drawer-label">{t("desktop.recent")}</div>
            <div className="list">
              {recent.map((w) => (
                <div key={w.dir} className="list-item recent">
                  <button className="item-main" onClick={() => show(w.dir)} title={t("desktop.drawer.reopen", { dir: w.dir })}>
                    <span className="lines">
                      <span className="ellipsis">{displayName(w)}</span>
                    </span>
                  </button>
                  <span className="trailing hover-only">
                    <IconButton
                      icon={mdiDeleteOutline}
                      label={t("desktop.forget")}
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
        <button className="list-item" onClick={onSettings} title={t("desktop.settings")}>
          <Icon path={mdiCogOutline} />
          {!rail && <span>{t("desktop.settings")}</span>}
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
  const status = a?.waiting ? t("desktop.drawer.waiting") : a?.running ? t("desktop.drawer.working") : "";
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
              label={t("desktop.edit_details")}
              small
              onClick={onEdit}
            />
            <IconButton
              icon={mdiClose}
              label={t("desktop.drawer.close", { name })}
              small
              onClick={onClose}
            />
          </span>
        </>
      )}
    </div>
  );
}
