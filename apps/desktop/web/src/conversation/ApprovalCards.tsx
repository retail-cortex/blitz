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

import { useState, type ReactNode } from "react";
import { mdiChevronUp, mdiFileCompare, mdiHelpCircleOutline, mdiInformationOutline, mdiShieldAlertOutline } from "@mdi/js";
import { Button, Icon, IconButton } from "@blitz/webui";
import { DiffView } from "../Changes";
import { Decision, type ApprovalRequest, type Question, type QuestionChoice } from "../gen/blitz/v1/turn_pb";
import { editorConfig, toEditor } from "../host";
import { t } from "../i18n";
import { Markdown } from "../Markdown";
import { useAdvanced } from "../state";
import { parseDiff } from "../turns";

/** An approval request, with its decisions (the canvas shows one too). */
export function ApprovalCard({ req, onDecide, who, autoFocus = true }: { req: ApprovalRequest; onDecide: (d: Decision) => void; who?: string; autoFocus?: boolean }) {
  const files = req.diff ? parseDiff(req.diff) : [];
  const advanced = useAdvanced(); // Always allow: advanced
  // Auto mode's reviewer denied it: its reason, and the person may allow
  // it anyway with a click, Deny focused (spec_005 APR-14d).
  const denied = !!req.reviewerDenied;
  return (
    <div className={`card approval ${who ? "task-request" : ""}`} role="alertdialog" aria-label={t("desktop.approval.label")}>
      <div className="row">
        <Icon path={mdiShieldAlertOutline} size="lg" />
        <div className="stack" style={{ gap: 2 }}>
          {who && <span className="t-label muted">{t("desktop.task.asks", { who })}</span>}
          <span className="t-title">{t("desktop.approval.title")}</span>
          <span className="muted">
            {t("desktop.approval.wants", { tool: req.tool, detail: req.detail })}
          </span>
          {denied && <span className="approval-reviewer">{t("desktop.approval.reviewer_denied", { reason: req.reviewerDenied })}</span>}
        </div>
      </div>
      {files.length > 0 && <DiffView files={files} compact />}
      <div className="row wrap">
        {files.length > 0 && editorConfig() && (
          <Button variant="outlined" icon={mdiFileCompare} onClick={() => toEditor({ type: "diff", diff: req.diff, title: req.detail })}>
            {t("desktop.approval.in_editor")}
          </Button>
        )}
        <Button variant="filled" onClick={() => onDecide(Decision.ONCE)} autoFocus={autoFocus && !denied}>
          {t(denied ? "desktop.approval.anyway" : "desktop.approval.once")}
        </Button>
        {req.scopeLabel && (
          <Button variant="tonal" onClick={() => onDecide(Decision.SESSION)}>
            {t("desktop.approval.session", { scope: req.scopeLabel })}
          </Button>
        )}
        {advanced && req.scopeLabel && <Button onClick={() => onDecide(Decision.ALWAYS)}>{t("desktop.approval.always", { scope: req.scopeLabel })}</Button>}
        <Button variant="outlined" danger onClick={() => onDecide(Decision.DENY)} autoFocus={autoFocus && denied}>
          {t("desktop.approval.deny")}
        </Button>
      </div>
    </div>
  );
}

/** A question card presented to the user (choices, multi-select, free text, or secret). */
export function QuestionCard({ q, onAnswer, who, autoFocus = true }: { q: Question; onAnswer: (a: string) => void; who?: string; autoFocus?: boolean }) {
  const [text, setText] = useState("");
  // A multi-select question: the options chosen, sent together (one per line).
  const [chosen, setChosen] = useState<string[]>([]);
  const toggle = (o: string) => setChosen((c) => (c.includes(o) ? c.filter((x) => x !== o) : [...c, o]));
  // A plan review lists "carry it out" first: make that the primary choice.
  return (
    <div className={`card question ${who ? "task-request" : ""}`} role="alertdialog" aria-label={t("desktop.question.title")}>
      <div className="row">
        <Icon path={mdiHelpCircleOutline} size="lg" />
        <div className="stack" style={{ gap: 2 }}>
          {who && <span className="t-label muted">{t("desktop.task.asks", { who })}</span>}
          <span className="t-title">{t("desktop.question.title")}</span>
        </div>
      </div>
      <div className="question-text">
        <Markdown text={q.question} />
      </div>
      {q.order && <span className="t-body-sm muted question-order">{t("desktop.question.order", { why: q.order })}</span>}
      {q.options.length > 0 && q.multiSelect && (
        <div className="stack" role="group" aria-label={t("desktop.question.choose_several")}>
          <span className="t-label muted">{t("desktop.question.choose_several")}</span>
          {q.options.map((o, i) => (
            <ChoiceRow key={o} choice={choiceOf(q, o)} recommended={i === 0 && q.choices.length > 0}>
              <label className="row check choice-tip" data-tip={choiceOf(q, o)?.summary || undefined}>
                <input type="checkbox" checked={chosen.includes(o)} onChange={() => toggle(o)} />
                <span>{o}</span>
              </label>
            </ChoiceRow>
          ))}
          <div className="row">
            <Button variant="filled" disabled={chosen.length === 0} onClick={() => onAnswer(q.options.filter((o) => chosen.includes(o)).join("\n"))}>
              {t("desktop.send")}
            </Button>
          </div>
        </div>
      )}
      {q.options.length > 0 && !q.multiSelect && q.choices.length === 0 && (
        <div className="row wrap answer-options">
          {q.options.map((o, i) => (
            <Button key={o} variant={i === 0 ? "filled" : "tonal"} onClick={() => onAnswer(o)}>
              {o}
            </Button>
          ))}
        </div>
      )}
      {q.options.length > 0 && !q.multiSelect && q.choices.length > 0 && (
        <div className="stack question-choices">
          {q.options.map((o, i) => (
            <ChoiceRow key={o} choice={choiceOf(q, o)} recommended={i === 0}>
              <span className="choice-tip" data-tip={choiceOf(q, o)?.summary || undefined}>
                <Button variant={i === 0 ? "filled" : "tonal"} onClick={() => onAnswer(o)}>
                  {o}
                </Button>
              </span>
            </ChoiceRow>
          ))}
        </div>
      )}
      <form
        className="row"
        onSubmit={(e) => {
          e.preventDefault();
          if (text.trim()) onAnswer(text.trim());
        }}
      >
        {/* A secret's value: typed hidden, sent to Blitz's secret store, never shown or given to the model (spec_060 DRV-05). */}
        <input
          className="input"
          type={q.secret ? "password" : "text"}
          autoComplete={q.secret ? "off" : undefined}
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder={q.secret ? t("desktop.question.secret") : q.options.length ? t("desktop.question.other") : t("desktop.question.answer")}
          autoFocus={autoFocus && q.options.length === 0}
        />
        <Button variant="text" type="submit" disabled={!text.trim()}>
          {t(q.secret ? "desktop.question.secret_save" : "desktop.send")}
        </Button>
      </form>
      {q.secret && <span className="t-body-sm muted">{t("desktop.question.secret_hint")}</span>}
    </div>
  );
}

/** The explanation of a question's option, when the agent gave one (APR-40b). */
const choiceOf = (q: Question, label: string) => q.choices.find((c) => c.label === label);

/**
 * One of a question's options, explained (spec_005_approvals APR-40b): the
 * button (its summary its tooltip), Recommended on the first, and a toggle
 * that opens what choosing it means and why it's ranked there.
 */
function ChoiceRow({ choice, recommended, children }: { choice?: QuestionChoice; recommended: boolean; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const more = !!choice && (!!choice.details || !!choice.why);
  return (
    <div className="question-choice">
      <div className="row" style={{ gap: 8 }}>
        {children}
        {recommended && <span className="chip static">{t("desktop.question.recommended")}</span>}
        {more && <IconButton icon={open ? mdiChevronUp : mdiInformationOutline} label={t("desktop.question.details", { label: choice!.label })} small aria-expanded={open} onClick={() => setOpen((o) => !o)} />}
      </div>
      {open && choice && (
        <div className="question-choice-details t-body-sm">
          {choice.summary && <strong>{choice.summary}</strong>}
          {choice.details && <Markdown text={choice.details} />}
          {choice.why && <span className="muted">{t("desktop.question.why", { why: choice.why })}</span>}
        </div>
      )}
    </div>
  );
}
