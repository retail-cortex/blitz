import { useState } from "react";
import { mdiCheck, mdiContentCopy, mdiFolderOutline } from "@mdi/js";
import { colorLabel, colorNames, workspaceColor } from "./palette";
import { baseName, editWorkspace, type WorkspacePrefs } from "./prefs";
import { useApp } from "./state";
import { t } from "./i18n";
import { Button, Dialog, Field, Icon, IconButton, useSnackbar } from "./ui/controls";

/** Edits how a workspace is shown: its name, description and colour. */
export function WorkspaceDialog({ ws, onClose, onCloseWorkspace }: { ws: WorkspacePrefs; onClose: () => void; onCloseWorkspace?: () => void }) {
  const { update, theme } = useApp();
  const snack = useSnackbar();
  const [name, setName] = useState(ws.name ?? "");
  const [description, setDescription] = useState(ws.description ?? "");
  const [color, setColor] = useState(ws.color ?? "blue");
  const save = () => {
    update((p) => editWorkspace(p, ws.dir, { name, description, color }));
    onClose();
  };
  return (
    <Dialog
      title={t("desktop.wsd.title")}
      icon={mdiFolderOutline}
      onClose={onClose}
      footer={
        <>
          {onCloseWorkspace && (
            <Button danger onClick={onCloseWorkspace}>
              {t("desktop.ws.close")}
            </Button>
          )}
          <span className="spacer" />
          <Button onClick={onClose}>{t("desktop.cancel")}</Button>
          <Button variant="filled" onClick={save}>
            {t("desktop.save")}
          </Button>
        </>
      }
    >
      <form
        className="stack"
        style={{ gap: 20 }}
        onSubmit={(e) => {
          e.preventDefault();
          save();
        }}
      >
        <Field label={t("desktop.wsd.name")} supporting={t("desktop.wsd.name_help", { name: baseName(ws.dir) })}>
          {(id) => <input id={id} className="input" value={name} placeholder={baseName(ws.dir)} maxLength={60} onChange={(e) => setName(e.target.value)} />}
        </Field>
        <Field label={t("desktop.wsd.description")} supporting={t("desktop.wsd.description_help")}>
          {(id) => <textarea id={id} className="input" rows={3} maxLength={400} value={description} onChange={(e) => setDescription(e.target.value)} />}
        </Field>
        <div className="field">
          <label>{t("desktop.wsd.colour")}</label>
          <div className="swatches" role="radiogroup" aria-label={t("desktop.wsd.colour")}>
            {colorNames.map((c) => (
              <button
                key={c}
                type="button"
                role="radio"
                aria-checked={c === color}
                aria-label={colorLabel(c)}
                title={colorLabel(c)}
                className="swatch"
                style={{ background: workspaceColor(c, theme) }}
                onClick={() => setColor(c)}
              >
                {c === color && <Icon path={mdiCheck} size="sm" />}
              </button>
            ))}
          </div>
        </div>
        <div className="field">
          <label>{t("desktop.wsd.folder")}</label>
          <div className="row">
            <code className="ellipsis muted" title={ws.dir}>
              {ws.dir}
            </code>
            <IconButton
              icon={mdiContentCopy}
              label={t("desktop.wsd.copy_path")}
              small
              onClick={() => navigator.clipboard?.writeText(ws.dir).then(() => snack(t("desktop.wsd.path_copied")))}
            />
          </div>
        </div>
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}
