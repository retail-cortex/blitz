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


// A workspace's language servers (spec_visual_editor_037 VE-45): each
// language's server, how it is, and Restart; in Settings › Workspaces.
import { useCallback, useEffect, useState } from "react";
import { mdiRestart } from "@mdi/js";
import { language as languageApi } from "./api";
import { message } from "./errors";
import { ServerState, type GetLanguageStatusResponse } from "./gen/blitz/v1/language_pb";
import { t } from "./i18n";
import { IconButton, useSnackbar } from "./ui/controls";

const stateKeys: Partial<Record<ServerState, string>> = {
  [ServerState.READY]: "desktop.lsp.ready",
  [ServerState.STARTING]: "desktop.lsp.starting",
  [ServerState.MISSING]: "desktop.lsp.missing",
  [ServerState.FAILED]: "desktop.lsp.failed",
  [ServerState.IDLE]: "desktop.lsp.idle",
};

/** The servers and their states, asked again every 5 s while shown. */
export function LanguageServers({ dir }: { dir: string }) {
  const snack = useSnackbar();
  const [status, setStatus] = useState<GetLanguageStatusResponse>();
  const [error, setError] = useState("");
  const load = useCallback(async () => {
    try {
      setStatus(await languageApi.getLanguageStatus({ workspace: dir }));
      setError("");
    } catch (e) {
      setError(message(e));
    }
  }, [dir]);
  useEffect(() => {
    void load();
    const every = setInterval(() => void load(), 5000);
    return () => clearInterval(every);
  }, [load]);
  const restart = async (lang: string) => {
    try {
      await languageApi.restartLanguageServer({ workspace: dir, language: lang });
      snack(t("desktop.lsp.restarted", { language: lang }));
      void load();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  if (error) return <p className="error-text t-body-sm">{error}</p>;
  if (!status) return null;
  return (
    <div className="lsp-servers">
      <p className="t-body-sm muted">{status.untrusted ? t("desktop.lsp.untrusted") : t("desktop.lsp.help")}</p>
      <ul className="lsp-list">
        {status.servers.map((s) => (
          <li key={s.language} className="lsp-row">
            <span className={`lsp-dot lsp-${ServerState[s.state]?.toLowerCase() ?? "idle"}`} aria-hidden />
            <span className="lsp-name t-body-md">{s.language}</span>
            <code className="lsp-command t-body-sm ellipsis" title={s.command.join(" ")}>
              {s.command.join(" ")}
            </code>
            <span className="lsp-state t-body-sm muted ellipsis" title={s.error || s.install}>
              {t(stateKeys[s.state] ?? "desktop.lsp.idle")}
              {s.state === ServerState.MISSING && s.install ? ` · ${s.install}` : ""}
              {s.state === ServerState.FAILED && s.error ? ` · ${s.error}` : ""}
            </span>
            {(s.state === ServerState.READY || s.state === ServerState.FAILED) && (
              <IconButton small icon={mdiRestart} label={t("desktop.lsp.restart", { language: s.language })} onClick={() => void restart(s.language)} />
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
