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
import { mdiCheck, mdiCogOutline, mdiWeb } from "@mdi/js";
import { config } from "../api";
import { message } from "../errors";
import { configChanged, openSettings } from "../events";
import type { BrowserProfile } from "../gen/blitz/v1/config_pb";
import { t } from "../i18n";
import { Button, Chip, Icon, IconButton, useSnackbar } from "@blitz/webui";

/**
 * Studio › Resources › Browser (spec_049_browser_profiles BP-03b): the
 * browser profiles, which are every workspace's (made and changed in
 * Settings › Resources › Browser), listed read-only, with the one this
 * workspace's agents browse with marked and Use here to choose another:
 * the workspace's own choice, as a connector's switch is.
 */
export function BrowsersPane({ workspace }: { workspace: string }) {
  const snack = useSnackbar();
  const [profiles, setProfiles] = useState<BrowserProfile[]>([]);
  const [current, setCurrent] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const load = useCallback(async () => {
    try {
      const r = await config.listBrowserProfiles({ workspace });
      setProfiles(r.profiles);
      setCurrent(r.workspaceProfile);
      setError("");
    } catch (e) {
      setError(message(e));
    }
  }, [workspace]);
  useEffect(() => {
    void load();
  }, [load]);
  const use = async (name: string) => {
    setBusy(true);
    try {
      await config.setWorkspaceBrowserProfile({ workspace, name });
      setCurrent(name);
      configChanged({ dir: workspace });
      snack(name ? t("desktop.rs.browser_set", { name }) : t("desktop.rs.browser_cleared"));
    } catch (e) {
      snack(message(e), { error: true });
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="studio-browsers stack" style={{ gap: 8 }}>
      <div className="row side-head">
        <span className="t-title-sm spacer">{t("desktop.settings.browser")}</span>
        <IconButton icon={mdiCogOutline} label={t("desktop.studio.browsers_edit")} small onClick={() => openSettings({ section: "browser" })} />
      </div>
      <p className="t-body-sm muted">{t("desktop.studio.browsers_intro")}</p>
      {error && <p className="error-text">{error}</p>}
      {profiles.length === 0 && !error ? (
        <p className="t-body-sm muted">{t("desktop.studio.browsers_none")}</p>
      ) : (
        <div className="list">
          {profiles.map((p) => (
            <div key={p.name} className="list-item static-item">
              <Icon path={mdiWeb} />
              <span className="lines">
                <span className="ellipsis">{p.name}</span>
                <small className="ellipsis">{p.allowDomains.length ? p.allowDomains.join(", ") : t("desktop.studio.browsers_no_sites")}</small>
              </span>
              <span className="trailing">
                {p.name === current ? (
                  <>
                    <Chip className="static" icon={mdiCheck} selected>
                      {t("desktop.studio.browsers_used")}
                    </Chip>
                    <Button small disabled={busy} onClick={() => void use("")}>
                      {t("desktop.studio.browsers_stop")}
                    </Button>
                  </>
                ) : (
                  <Button small variant="tonal" disabled={busy} onClick={() => void use(p.name)}>
                    {t("desktop.studio.browsers_use")}
                  </Button>
                )}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
