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

// A ! command in the chat: the command, its output as it comes (a code
// block's look), and how it ended, with Stop while it runs and Copy.
import { useEffect, useRef, useState } from "react";
import { mdiCheck, mdiCheckCircleOutline, mdiCloseCircleOutline, mdiConsoleLine, mdiContentCopy, mdiLoading, mdiStop } from "@mdi/js";
import type { Entry } from "./turns";
import { t } from "./i18n";
import { Icon, IconButton } from "./ui/controls";

/** One ! command's card. */
export function ShellCard({ entry, onStop }: { entry: Extract<Entry, { kind: "shell" }>; onStop: (id: string) => void }) {
  const [copied, setCopied] = useState(false);
  const out = useRef<HTMLPreElement>(null);
  // Follows the output as it comes, as a terminal does.
  useEffect(() => {
    if (entry.running && out.current) out.current.scrollTop = out.current.scrollHeight;
  }, [entry.output, entry.running]);
  const secs = ((entry.elapsedMs ?? 0) / 1000).toFixed(1);
  const status = entry.running
    ? t("desktop.shell.running")
    : entry.error
      ? t("desktop.shell.failed", { error: entry.error })
      : entry.code === -1
        ? t("desktop.shell.stopped")
        : entry.code
          ? t("desktop.shell.exit", { code: entry.code, secs })
          : t("desktop.shell.done", { secs });
  const ok = !entry.running && !entry.error && entry.code === 0;
  // Stopped is the user's doing: said plainly, not as a failure.
  const quiet = ok || entry.running || (!entry.error && entry.code === -1);
  return (
    <div className="code-block shell-card" aria-busy={entry.running}>
      <div className="code-head">
        <Icon path={mdiConsoleLine} size="sm" className="shell-text" />
        <code className="shell-text ellipsis spacer" title={entry.command}>
          !{entry.command}
        </code>
        <span className={`row t-body-sm ${quiet ? "muted" : "error-text"}`} style={{ gap: 4 }}>
          <Icon path={entry.running ? mdiLoading : ok ? mdiCheckCircleOutline : quiet ? mdiStop : mdiCloseCircleOutline} spin={entry.running} size="sm" />
          {status}
        </span>
        {entry.running ? (
          <IconButton icon={mdiStop} label={t("desktop.shell.stop")} small onClick={() => onStop(entry.id)} />
        ) : (
          entry.output && (
            <IconButton
              icon={copied ? mdiCheck : mdiContentCopy}
              label={copied ? t("desktop.code.copied") : t("desktop.shell.copy")}
              small
              onClick={() =>
                navigator.clipboard?.writeText(entry.output).then(() => {
                  setCopied(true);
                  setTimeout(() => setCopied(false), 1500);
                })
              }
            />
          )
        )}
      </div>
      {(entry.output || entry.running) && (
        <pre ref={out} className="shell-output">
          <code>{entry.output || " "}</code>
        </pre>
      )}
      {entry.truncated && <p className="t-body-sm muted shell-note">{t("desktop.shell.truncated")}</p>}
    </div>
  );
}
