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

import { useState } from "react";
import { mdiFolderPlusOutline, mdiFolderSyncOutline } from "@mdi/js";
import { config } from "./api";
import { message } from "./errors";
import type { OrgWorkspace } from "./gen/blitz/v1/config_pb";
import { t } from "./i18n";
import { Button, Dialog, Field, Switch } from "@blitz/webui";

/** A handle as the service makes one from a name: lower case, - between words. */
function handleOf(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

/**
 * Makes a workspace of the organization's (spec_053 EN-60), a team's or
 * the person's own, or shares the folder dir with it where it is (EN-66):
 * its name, Just for me, and for a share what's about to happen. done
 * hears the workspace and what the person should know.
 */
export function OrgWorkspaceDialog({
  org,
  share,
  onClose,
  onDone,
}: {
  org: string;
  /** The folder to share, and its name; none to make a new workspace. */
  share?: { dir: string; name: string };
  onClose: () => void;
  onDone: (w: OrgWorkspace, notes: string[]) => void;
}) {
  const [name, setName] = useState(share?.name ?? "");
  const [personal, setPersonal] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const go = async () => {
    setBusy(true);
    setError("");
    try {
      if (share) {
        const r = await config.shareWithOrganization({ workspace: share.dir, name: name.trim(), personal });
        onDone(r.workspace!, r.notes);
      } else {
        const r = await config.createOrgWorkspace({ name: name.trim(), personal });
        onDone(r.workspace!, []);
      }
    } catch (e) {
      setError(message(e));
      setBusy(false);
    }
  };
  const handle = handleOf(name) || "name";
  return (
    <Dialog
      title={share ? t("desktop.org.share_title", { name: share.name, org }) : t("desktop.org.new_title", { org })}
      icon={share ? mdiFolderSyncOutline : mdiFolderPlusOutline}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose} disabled={busy}>
            {t("desktop.cancel")}
          </Button>
          <Button variant="filled" disabled={busy || !name.trim()} onClick={() => void go()}>
            {busy ? t("desktop.org.working") : share ? t("desktop.org.share_go") : t("desktop.org.create")}
          </Button>
        </>
      }
    >
      <div className="stack" style={{ gap: 12 }}>
        {share && <p className="t-body-sm muted">{t("desktop.org.share_body", { org })}</p>}
        <Field label={t("desktop.org.name")}>
          {(id) => <input id={id} className="input" value={name} disabled={busy} autoFocus onChange={(e) => setName(e.target.value)} onKeyDown={(e) => e.key === "Enter" && name.trim() && void go()} />}
        </Field>
        <label className="row" style={{ gap: 8 }}>
          <Switch checked={personal} disabled={busy} label={t("desktop.org.personal")} onChange={setPersonal} />
          <span className="t-label">{t("desktop.org.personal")}</span>
        </label>
        <p className="t-body-sm muted">{t("desktop.org.personal_hint", { handle })}</p>
        {error && (
          <p className="error-text t-body-sm" role="alert">
            {error}
          </p>
        )}
      </div>
    </Dialog>
  );
}
