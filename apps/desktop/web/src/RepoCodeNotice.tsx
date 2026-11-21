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
import { mdiAlertCircleOutline } from "@mdi/js";
import { Button, Icon, useSnackbar } from "@blitz/webui";
import { language as languageApi } from "./api";
import { message } from "./errors";
import { t } from "./i18n";

/**
 * Asks, once for the workspace, whether its language servers may run its
 * own code (spec_037 VE-43a): its Bazel driver, its .venv's Python. Until
 * it's answered they start without it; "Not now" keeps it that way.
 */
export function RepoCodeNotice({ dir }: { dir: string }) {
  const snack = useSnackbar();
  const [files, setFiles] = useState<string[]>();
  const [busy, setBusy] = useState(false);
  const load = useCallback(async () => {
    try {
      const st = await languageApi.getLanguageStatus({ workspace: dir });
      setFiles(st.repoCode === "ask" ? st.repoCodeFiles : undefined);
    } catch {
      setFiles(undefined); // no language servers here, or an older service
    }
  }, [dir]);
  useEffect(() => {
    void load();
  }, [load]);
  if (!files?.length) return null;
  const decide = async (trusted: boolean) => {
    setBusy(true);
    try {
      await languageApi.trustLanguageCode({ workspace: dir, trusted });
      snack(t(trusted ? "desktop.lsp.code_trusted" : "desktop.lsp.code_declined"));
      setFiles(undefined);
    } catch (e) {
      snack(message(e), { error: true });
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="card warn row">
      <Icon path={mdiAlertCircleOutline} />
      <span style={{ flex: 1 }}>{t("desktop.lsp.code_ask", { files: files.join(", ") })}</span>
      <Button small disabled={busy} onClick={() => void decide(false)}>
        {t("desktop.lsp.code_not_now")}
      </Button>
      <Button small variant="tonal" disabled={busy} onClick={() => void decide(true)}>
        {t("desktop.lsp.code_trust")}
      </Button>
    </div>
  );
}
