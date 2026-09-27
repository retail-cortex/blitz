import { useState } from "react";
import { mdiCheck, mdiContentCopy, mdiFolderOutline } from "@mdi/js";
import { colorNames, palette, workspaceColor } from "./palette";
import { baseName, editWorkspace, type WorkspacePrefs } from "./prefs";
import { useApp } from "./state";
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
      title="Workspace details"
      icon={mdiFolderOutline}
      onClose={onClose}
      footer={
        <>
          {onCloseWorkspace && (
            <Button danger onClick={onCloseWorkspace}>
              Close workspace
            </Button>
          )}
          <span className="spacer" />
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="filled" onClick={save}>
            Save
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
        <Field label="Name" supporting={`Shown in the menu and title bar. Empty: “${baseName(ws.dir)}”.`}>
          {(id) => <input id={id} className="input" value={name} placeholder={baseName(ws.dir)} maxLength={60} onChange={(e) => setName(e.target.value)} />}
        </Field>
        <Field label="Description" supporting="A note for yourself: what this project is, or what you're doing in it.">
          {(id) => <textarea id={id} className="input" rows={3} maxLength={400} value={description} onChange={(e) => setDescription(e.target.value)} />}
        </Field>
        <div className="field">
          <label>Colour</label>
          <div className="swatches" role="radiogroup" aria-label="Colour">
            {colorNames.map((c) => (
              <button
                key={c}
                type="button"
                role="radio"
                aria-checked={c === color}
                aria-label={palette[c].label}
                title={palette[c].label}
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
          <label>Folder</label>
          <div className="row">
            <code className="ellipsis muted" title={ws.dir}>
              {ws.dir}
            </code>
            <IconButton
              icon={mdiContentCopy}
              label="Copy the path"
              small
              onClick={() => navigator.clipboard?.writeText(ws.dir).then(() => snack("Path copied."))}
            />
          </div>
        </div>
        <button type="submit" hidden />
      </form>
    </Dialog>
  );
}
