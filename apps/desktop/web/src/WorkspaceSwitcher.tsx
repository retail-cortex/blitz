import { useCallback, useRef, useState } from "react";
import { mdiChevronDown, mdiClose, mdiDeleteOutline, mdiFolderOpenOutline, mdiPencilOutline } from "@mdi/js";
import { workspaceColor } from "./palette";
import { displayName, forgetWorkspace, openWorkspace, openWorkspaces, recentWorkspaces, type WorkspacePrefs } from "./prefs";
import { useApp } from "./state";
import { t } from "./i18n";
import { Icon, IconButton, useDismiss } from "./ui/controls";

/**
 * The workspace in view, as a dropdown at the left of the top bar: the
 * open workspaces (with what their agents are doing, and edit and close),
 * the recently closed ones, and opening another. A badge on it says when
 * another workspace is waiting for you.
 */
export function WorkspaceSwitcher({ onOpen, onClose, onEdit }: { onOpen: () => void; onClose: (dir: string) => void; onEdit: (dir: string) => void }) {
  const { prefs, update, theme, activity } = useApp();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const close = useCallback(() => setOpen(false), []);
  useDismiss(open, ref, close);
  const list = openWorkspaces(prefs);
  const recent = recentWorkspaces(prefs);
  const current = list.find((w) => w.dir === prefs.active);
  const name = current ? displayName(current) : t("desktop.switcher.none");
  const othersWaiting = list.some((w) => w.dir !== prefs.active && activity[w.dir]?.waiting);
  const othersWorking = list.some((w) => w.dir !== prefs.active && activity[w.dir]?.running);
  const pick = (dir: string) => {
    setOpen(false);
    update((p) => openWorkspace(p, dir));
  };

  return (
    <div className="switcher no-drag" ref={ref}>
      <button
        className="switcher-button"
        aria-haspopup="menu"
        aria-expanded={open}
        title={current ? `${name}\n${current.description || current.dir}` : undefined}
        onClick={() => setOpen((o) => !o)}
      >
        {current && <Avatar w={current} color={workspaceColor(current.color, theme)} />}
        <span className="t-title-sm ellipsis">{name}</span>
        {othersWaiting ? (
          <span className="badge" title={t("desktop.switcher.others_waiting")}>
            !
          </span>
        ) : othersWorking ? (
          <span className="dot pulse" title={t("desktop.switcher.others_working")} />
        ) : null}
        <Icon path={mdiChevronDown} size="sm" />
      </button>
      {open && (
        <div className="menu switcher-menu" role="menu" aria-label={t("desktop.switcher.label")}>
          {list.length > 0 && <div className="menu-label">{t("desktop.switcher.label")}</div>}
          {list.map((w) => (
            <Row key={w.dir} w={w} active={w.dir === prefs.active} onPick={() => pick(w.dir)} onEdit={() => (setOpen(false), onEdit(w.dir))} onClose={() => (setOpen(false), onClose(w.dir))} />
          ))}
          {recent.length > 0 && (
            <>
              <div className="menu-divider" />
              <div className="menu-label">{t("desktop.recent")}</div>
              {recent.slice(0, 8).map((w) => (
                <div key={w.dir} className="switcher-row">
                  <button role="menuitem" className="menu-item" onClick={() => pick(w.dir)} title={t("desktop.switcher.reopen", { dir: w.dir })}>
                    <Icon path={mdiFolderOpenOutline} />
                    <span>
                      {displayName(w)}
                      <small className="ellipsis">{w.description || w.dir}</small>
                    </span>
                  </button>
                  <span className="switcher-actions">
                    <IconButton icon={mdiDeleteOutline} label={t("desktop.forget")} small onClick={() => update((p) => forgetWorkspace(p, w.dir))} />
                  </span>
                </div>
              ))}
            </>
          )}
          <div className="menu-divider" />
          <button role="menuitem" className="menu-item" onClick={() => (setOpen(false), onOpen())}>
            <Icon path={mdiFolderOpenOutline} />
            <span>{t("desktop.switcher.open")}…</span>
          </button>
        </div>
      )}
    </div>
  );
}

function Avatar({ w, color }: { w: WorkspacePrefs; color: string }) {
  return (
    <span className="avatar small" style={{ background: color }}>
      {displayName(w).slice(0, 1).toUpperCase()}
    </span>
  );
}

function Row({ w, active, onPick, onEdit, onClose }: { w: WorkspacePrefs; active: boolean; onPick: () => void; onEdit: () => void; onClose: () => void }) {
  const { theme, activity } = useApp();
  const a = activity[w.dir];
  const name = displayName(w);
  const status = a?.waiting ? t("desktop.switcher.waiting") : a?.running ? t("desktop.switcher.working") : "";
  return (
    <div className={`switcher-row ${active ? "active" : ""}`}>
      <button role="menuitem" className="menu-item" aria-current={active ? "page" : undefined} onClick={onPick} title={w.dir}>
        <span className="avatar small" style={{ background: workspaceColor(w.color, theme) }}>
          {name.slice(0, 1).toUpperCase()}
          {a?.waiting ? <span className="badge avatar-badge">!</span> : a?.running ? <span className="dot avatar-dot pulse" /> : null}
        </span>
        <span>
          {name}
          <small className="ellipsis">{status || w.description || w.dir}</small>
        </span>
      </button>
      <span className="switcher-actions">
        <IconButton icon={mdiPencilOutline} label={t("desktop.edit_details")} small onClick={onEdit} />
        <IconButton icon={mdiClose} label={t("desktop.switcher.close", { name })} small onClick={onClose} />
      </span>
    </div>
  );
}
