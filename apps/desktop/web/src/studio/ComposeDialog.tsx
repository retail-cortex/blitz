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

import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { mdiAlertCircleOutline, mdiAutoFix, mdiLoading, mdiRestart, mdiSend, mdiSitemapOutline } from "@mdi/js";
import { workspaces } from "../api";
import { message } from "../errors";
import type { ComposeDraft, ComposeSession } from "../gen/blitz/v1/workspace_pb";
import { t } from "../i18n";
import { Button, Dialog, Icon, useSnackbar } from "../ui/controls";
import { cost } from "../workflowRuns";
import { draftSteps, question } from "./compose";

/**
 * The composer (spec_composer_045): what the user wants, a short guided
 * conversation, then the drafted component, checked, to open in its
 * editor (onDraft). Closing ends the session.
 */
export function ComposeDialog({ workspace, kind, onDraft, onClose }: { workspace: string; kind: "workflow"; onDraft: (d: ComposeDraft) => void; onClose: () => void }) {
  const snack = useSnackbar();
  const [session, setSession] = useState<ComposeSession>();
  const [request, setRequest] = useState("");
  const [answer, setAnswer] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const end = useRef<HTMLDivElement>(null);
  const id = useRef("");
  useEffect(() => () => void (id.current && workspaces.dropCompose({ workspace, id: id.current }).catch(() => {})), [workspace]);
  useEffect(() => end.current?.scrollIntoView({ block: "end" }), [session, busy]);

  const run = async (f: () => Promise<ComposeSession | undefined>) => {
    setBusy(true);
    setError("");
    try {
      const s = await f();
      if (s) {
        id.current = s.id;
        setSession(s);
      }
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  };
  const start = () => request.trim() && void run(async () => (await workspaces.startCompose({ workspace, kind, request: request.trim() })).session);
  const reply = (text: string) => {
    if (!text.trim() || !session) return;
    setAnswer("");
    void run(async () => (await workspaces.answerCompose({ workspace, id: session.id, answer: text.trim() })).session);
  };
  const restart = () => {
    if (id.current) void workspaces.dropCompose({ workspace, id: id.current }).catch(() => {});
    id.current = "";
    setSession(undefined);
    setAnswer("");
    setError("");
  };

  const q = question(session);
  const draft = session?.draft;
  const steps = draftSteps(draft?.workflow);
  return createPortal(
    <Dialog
      title={t(`desktop.compose.title.${kind}`)}
      icon={mdiAutoFix}
      className="wide compose-dialog"
      onClose={onClose}
      footer={
        <>
          {session && <span className="t-body-sm muted spacer">{t("desktop.compose.cost", { cost: cost(session.costUsd) })}</span>}
          {session && (
            <Button icon={mdiRestart} onClick={restart} disabled={busy}>
              {t("desktop.compose.restart")}
            </Button>
          )}
          <Button onClick={onClose}>{t("desktop.cancel")}</Button>
          {draft ? (
            <Button
              variant="filled"
              icon={mdiSitemapOutline}
              onClick={() => {
                onDraft(draft);
                snack(t("desktop.compose.opened"));
              }}
            >
              {t("desktop.compose.open")}
            </Button>
          ) : (
            !session && (
              <Button variant="filled" icon={busy ? mdiLoading : mdiAutoFix} iconSpin={busy} disabled={busy || !request.trim()} onClick={start}>
                {t("desktop.compose.start")}
              </Button>
            )
          )}
        </>
      }
    >
      <div className="stack compose" style={{ gap: 12 }}>
        {!session ? (
          <>
            <p className="t-body-md">{t(`desktop.compose.intro.${kind}`)}</p>
            <textarea
              className="input"
              rows={4}
              autoFocus
              value={request}
              placeholder={t(`desktop.compose.placeholder.${kind}`)}
              aria-label={t("desktop.compose.request")}
              onChange={(e) => setRequest(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && (e.metaKey || e.ctrlKey) && start()}
            />
            {busy && (
              <span className="row t-body-sm muted" style={{ gap: 6 }}>
                <Icon path={mdiLoading} spin size="sm" /> {t("desktop.compose.thinking")}
              </span>
            )}
          </>
        ) : (
          <div className="compose-turns stack" style={{ gap: 8 }}>
            {session.turns.map((turn, i) => (
              <div key={i} className={`compose-turn ${turn.role}`}>
                {turn.text}
              </div>
            ))}
            {q && q.options.length > 0 && (
              <div className="row wrap" style={{ gap: 6 }}>
                {q.options.map((o) => (
                  <Button key={o} small variant="tonal" disabled={busy} onClick={() => reply(o)}>
                    {o}
                  </Button>
                ))}
              </div>
            )}
            {draft && (
              <div className="card compose-draft stack" style={{ gap: 8 }}>
                <span className="t-title-sm">
                  {draft.name} <span className="t-body-sm muted">· {t("desktop.compose.steps", { count: steps.length })}</span>
                </span>
                <ol className="compose-steps t-body-sm">
                  {steps.map((s) => (
                    <li key={s.id}>
                      {s.title} <span className="muted">· {t(`desktop.flows.type.${s.type}`)}</span>
                    </li>
                  ))}
                </ol>
                {draft.problems.length > 0 && (
                  <div className="card warn stack" style={{ gap: 4 }}>
                    <span className="t-body-sm">{t("desktop.compose.problems")}</span>
                    {draft.problems.map((p) => (
                      <span key={p} className="row t-body-sm" style={{ gap: 6 }}>
                        <Icon path={mdiAlertCircleOutline} size="sm" /> {p}
                      </span>
                    ))}
                  </div>
                )}
              </div>
            )}
            {busy && (
              <span className="row t-body-sm muted" style={{ gap: 6 }}>
                <Icon path={mdiLoading} spin size="sm" /> {t(draft ? "desktop.compose.redrafting" : "desktop.compose.thinking")}
              </span>
            )}
            <div className="row" style={{ gap: 8 }}>
              <input
                className="input spacer"
                value={answer}
                disabled={busy}
                placeholder={t(draft ? "desktop.compose.change" : "desktop.compose.answer")}
                aria-label={t(draft ? "desktop.compose.change" : "desktop.compose.answer")}
                onChange={(e) => setAnswer(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && reply(answer)}
              />
              <Button icon={mdiSend} disabled={busy || !answer.trim()} onClick={() => reply(answer)}>
                {t("desktop.compose.send")}
              </Button>
            </div>
            <div ref={end} />
          </div>
        )}
        {error && <p className="error-text t-body-sm">{error}</p>}
      </div>
    </Dialog>,
    document.body,
  );
}
