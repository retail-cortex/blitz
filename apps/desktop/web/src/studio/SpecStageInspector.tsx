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

import { useEffect, useState } from "react";
import { mdiCheckCircleOutline, mdiChevronDoubleRight, mdiCircleOutline, mdiClose, mdiCreationOutline, mdiOpenInNew, mdiPlay, mdiPlus, mdiProgressClock, mdiPuzzleOutline, mdiRobotOutline } from "@mdi/js";
import { workspaces } from "../api";
import { message } from "../errors";
import type { UpdateSpecRequest } from "../gen/blitz/v1/workspace_pb";
import { t, tn } from "../i18n";
import { Button, Chip, Field, Icon, IconButton, MultiSelect, Segmented, useSnackbar } from "@blitz/webui";
import { DraftButton } from "./Draft";
import { buildStagePrompt, canDependOn, getStageDraft, parseSpecStageDraft, resolveQuestionsDraftRequest, resolveQuestionsPrompt, reviewStagePrompt, setStageDraft, specStatuses, stageProgress, type SpecStageDraftState, type SpecStageInput, type SpecStatus } from "./specsGraph";

/** An atomic update to a specification stage or creation of a new one. */
export type SpecPatch = Partial<Omit<UpdateSpecRequest, "$typeName" | "$unknown" | "workspace">> & { name: string };

/** The icon and color class for a stage's status ("draft", "in-progress", "done"). */
export function statusMark(status: string): { icon: string; className: string } {
  if (status === "done") return { icon: mdiCheckCircleOutline, className: "ok-text" };
  if (status === "in-progress") return { icon: mdiProgressClock, className: "warn-text" };
  return { icon: mdiCircleOutline, className: "muted" };
}

/**
 * The stage inspector beside the Studio › Specs canvas (spec_040_studio STU-11):
 * edits a stage's status, dependencies, persistent requirement and done-when
 * checklists, open-question answers (applied in-place with AI or sent to /spec),
 * and inline stage redrafts.
 */
export function SpecStageInspector({
  workspace, stage, stages, busy, onPatch, onHide, onOpenFile, onCompose,
}: {
  workspace: string; stage: SpecStageInput; stages: readonly SpecStageInput[]; busy: boolean;
  onPatch: (patch: SpecPatch) => Promise<boolean>; onHide: () => void;
  onOpenFile: (path: string) => void; onCompose: (text: string, run?: boolean) => void;
}) {
  const snack = useSnackbar();
  const [draft, setDraft] = useState<SpecStageDraftState>(() => getStageDraft(workspace, stage.name));
  const [resolving, setResolving] = useState(false);

  useEffect(() => {
    setDraft(getStageDraft(workspace, stage.name));
  }, [workspace, stage.name]);

  const updateDraft = (patch: Partial<SpecStageDraftState>) => {
    setDraft(setStageDraft(workspace, stage.name, patch));
  };

  const viewStage = draft.stagedContent ? parseSpecStageDraft(stage, draft.stagedContent) : stage;
  const progress = stageProgress(viewStage);
  const mark = statusMark(viewStage.status);
  const hasTypedAnswers = Object.values(draft.answers).some((v) => v.trim().length > 0);
  const isCustomStatus = Boolean(viewStage.status && !specStatuses.includes(viewStage.status as SpecStatus));

  const addReq = async () => {
    const v = draft.newReq.trim();
    if (!v || busy) return;
    if (await onPatch({ name: stage.name, hash: stage.hash, addRequirement: v })) {
      updateDraft({ newReq: "" });
    }
  };

  const addCriterion = async () => {
    const v = draft.newDoneWhen.trim();
    if (!v || busy) return;
    if (await onPatch({ name: stage.name, hash: stage.hash, addDoneWhen: v })) {
      updateDraft({ newDoneWhen: "" });
    }
  };

  const addQuestion = async () => {
    const v = draft.newQuestion.trim();
    if (!v || busy) return;
    if (await onPatch({ name: stage.name, hash: stage.hash, addOpenQuestion: v })) {
      updateDraft({ newQuestion: "" });
    }
  };

  const saveStagedDraft = async () => {
    if (!draft.stagedContent.trim() || busy) return;
    if (await onPatch({ name: stage.name, hash: draft.baseHash || stage.hash, content: draft.stagedContent })) {
      updateDraft({ stagedContent: "", changes: [], baseHash: "", rawMode: false, answers: {} });
    }
  };

  const revertStagedDraft = () => {
    updateDraft({ stagedContent: "", changes: [], baseHash: "", rawMode: false });
  };

  const applyAnswersWithAI = async () => {
    const reqText = resolveQuestionsDraftRequest(viewStage, draft.answers);
    if (!reqText || resolving || busy) return;
    setResolving(true);
    try {
      const r = await workspaces.draftManifest({
        workspace,
        kind: "spec_stage",
        name: stage.name,
        description: viewStage.title,
        current: draft.stagedContent || stage.content || "",
        request: reqText,
        language: "",
      });
      if (r.problem) {
        snack(r.problem, { error: true });
        return;
      }
      updateDraft({ stagedContent: r.text, changes: r.changes, baseHash: draft.baseHash || stage.hash || "" });
    } catch (e) {
      snack(message(e), { error: true });
    } finally {
      setResolving(false);
    }
  };

  return (
    <>
      <div className="row flow-side-head">
        <Icon path={mark.icon} className={mark.className} />
        <span className="t-title-sm spacer ellipsis">{viewStage.number ? `${viewStage.number} · ${viewStage.title}` : viewStage.title}</span>
        <DraftButton
          workspace={workspace}
          kind="spec_stage"
          name={stage.name}
          description={viewStage.title}
          current={draft.stagedContent || stage.content || ""}
          disabled={busy || resolving}
          onUse={(md, changes) => updateDraft({ stagedContent: md, changes, baseHash: draft.baseHash || stage.hash || "" })}
        />
        <IconButton icon={mdiOpenInNew} label={t("desktop.specs.open_file")} small onClick={() => onOpenFile(stage.path)} />
        <IconButton icon={mdiChevronDoubleRight} label={t("desktop.flows.side_hide")} small onClick={onHide} />
      </div>

      <div className="flow-side-body stack">
        {draft.stagedContent && (
          <div className="card warn stack" style={{ gap: 6 }}>
            <div className="row" style={{ gap: 6 }}>
              <span className="t-label spacer">{t("desktop.specs.ai_applied")}</span>
              <Button small onClick={() => updateDraft({ rawMode: !draft.rawMode })}>
                {t(draft.rawMode ? "desktop.files.preview" : "desktop.files.source")}
              </Button>
              <Button small onClick={revertStagedDraft} disabled={busy}>{t("desktop.file.revert")}</Button>
              <Button small variant="filled" onClick={() => void saveStagedDraft()} disabled={busy}>{t("desktop.save")}</Button>
            </div>
            {draft.changes.length > 0 && (
              <ul className="t-body-sm muted" style={{ margin: 0, paddingInlineStart: 18 }}>
                {draft.changes.map((c) => <li key={c}>{c}</li>)}
              </ul>
            )}
            {draft.rawMode && (
              <textarea
                className="input mono draft-text"
                rows={10}
                value={draft.stagedContent}
                onChange={(e) => updateDraft({ stagedContent: e.target.value })}
              />
            )}
          </div>
        )}

        <Field label={t("desktop.specs.status")}>
          {() => (
            <div className="row" style={{ gap: 6 }}>
              {isCustomStatus && <Chip>{viewStage.status}</Chip>}
              <Segmented<SpecStatus>
                small
                equal
                label={t("desktop.specs.status")}
                value={(specStatuses.includes(viewStage.status as SpecStatus) ? viewStage.status : "") as SpecStatus}
                options={specStatuses.map((s) => ({ value: s, label: t(`desktop.specs.status_${s}`) }))}
                onChange={(next) => void onPatch({ name: stage.name, hash: stage.hash, status: next })}
              />
            </div>
          )}
        </Field>

        {stages.length > 1 && (
          <Field label={t("desktop.specs.depends_on")}>
            {(id) => (
              <MultiSelect
                id={id}
                label={t("desktop.specs.depends_on")}
                none={t("desktop.specs.depends_none")}
                chosen={viewStage.dependsOn}
                options={stages
                  .filter((other) => other.name !== stage.name && (viewStage.dependsOn.includes(other.name) || canDependOn(stages, stage.name, other.name)))
                  .map((other) => ({ value: other.name, detail: other.title }))}
                onChange={(next) => void onPatch({ name: stage.name, hash: stage.hash, setDependsOn: true, dependsOn: next })}
              />
            )}
          </Field>
        )}

        {viewStage.context && (
          <div className="stack" style={{ gap: 4 }}>
            <span className="t-label">{t("desktop.specs.context")}</span>
            <p className="t-body-sm muted" style={{ margin: 0 }}>{viewStage.context}</p>
          </div>
        )}

        <div className="stack" style={{ gap: 6 }}>
          <div className="row">
            <span className="t-label spacer">{tn("desktop.specs.items_n", viewStage.items.length)}</span>
            {viewStage.items.length > 0 && (
              <span className="t-body-sm muted">{`${progress.itemsDone}/${progress.itemsTotal}`}</span>
            )}
          </div>
          {viewStage.items.map((it) => (
            <div key={it.id} className="row" style={{ gap: 6, alignItems: "flex-start" }}>
              <label className="row a2ui-check t-body-sm spacer" style={{ alignItems: "flex-start" }}>
                <input
                  type="checkbox"
                  checked={Boolean(it.done)}
                  disabled={busy}
                  onChange={() => void onPatch({ name: stage.name, hash: stage.hash, toggleItemId: it.id, itemDone: !it.done })}
                />
                <span className={it.done ? "muted" : ""}>
                  <b className="mono">{it.id}</b> {it.text}
                </span>
              </label>
              <IconButton
                icon={mdiPlay}
                label={t("desktop.specs.build_one", { id: it.id })}
                small
                onClick={() => onCompose(buildStagePrompt(viewStage, [it.id]))}
              />
            </div>
          ))}
          <div className="row" style={{ gap: 6 }}>
            <input
              className="input spacer"
              value={draft.newReq}
              placeholder={t("desktop.specs.add_requirement_ph")}
              onChange={(e) => updateDraft({ newReq: e.target.value })}
              onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); void addReq(); } }}
            />
            <IconButton icon={mdiPlus} label={t("desktop.specs.add_requirement")} small disabled={!draft.newReq.trim() || busy} onClick={() => void addReq()} />
          </div>
        </div>

        <div className="stack" style={{ gap: 6 }}>
          <div className="row">
            <span className="t-label spacer">{t("desktop.specs.done_when")}</span>
            {viewStage.doneWhen.length > 0 && (
              <span className="t-body-sm muted">{`${progress.doneWhenDone}/${progress.doneWhenTotal}`}</span>
            )}
          </div>
          {viewStage.doneWhen.map((d, idx) => {
            const done = Boolean(viewStage.doneWhenDone?.[idx]);
            return (
              <label key={`${idx}-${d}`} className="row a2ui-check t-body-sm" style={{ alignItems: "flex-start" }}>
                <input
                  type="checkbox"
                  checked={done}
                  disabled={busy}
                  onChange={() => void onPatch({ name: stage.name, hash: stage.hash, toggleDoneWhenIndex: idx + 1, doneWhenDone: !done })}
                />
                <span className={done ? "muted" : ""}>{d}</span>
              </label>
            );
          })}
          <div className="row" style={{ gap: 6 }}>
            <input
              className="input spacer"
              value={draft.newDoneWhen}
              placeholder={t("desktop.specs.add_done_when_ph")}
              onChange={(e) => updateDraft({ newDoneWhen: e.target.value })}
              onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); void addCriterion(); } }}
            />
            <IconButton icon={mdiPlus} label={t("desktop.specs.add_done_when")} small disabled={!draft.newDoneWhen.trim() || busy} onClick={() => void addCriterion()} />
          </div>
        </div>

        <div className={`stack ${viewStage.openQuestions.length > 0 ? "card warn" : ""}`} style={{ gap: 8 }}>
          <div className="row">
            <span className="t-label spacer">
              {viewStage.openQuestions.length > 0 ? tn("desktop.specs.open_n", viewStage.openQuestions.length) : t("desktop.specs.open_questions")}
            </span>
            {viewStage.openQuestions.length > 0 && (
              <>
                <Button
                  small
                  variant="filled"
                  icon={mdiCreationOutline}
                  disabled={busy || resolving || !hasTypedAnswers}
                  onClick={() => void applyAnswersWithAI()}
                >
                  {resolving ? t("desktop.draft.drafting") : t("desktop.specs.apply_answers")}
                </Button>
                <Button small onClick={() => onCompose(resolveQuestionsPrompt(viewStage, draft.answers), true)}>
                  {t("desktop.specs.resolve")}
                </Button>
              </>
            )}
          </div>
          {viewStage.openQuestions.map((q, idx) => (
            <div key={`${idx}-${q}`} className="stack" style={{ gap: 4 }}>
              <div className="row" style={{ gap: 6, alignItems: "flex-start" }}>
                <span className="t-body-sm spacer">{q}</span>
                <IconButton
                  icon={mdiClose}
                  label={t("desktop.specs.dismiss_question")}
                  small
                  disabled={busy}
                  onClick={() => void onPatch({ name: stage.name, hash: stage.hash, removeOpenQuestionIndex: idx + 1 })}
                />
              </div>
              <input
                className="input"
                value={draft.answers[idx] ?? ""}
                placeholder={t("desktop.specs.answer_ph")}
                onChange={(e) => updateDraft({ answers: { ...draft.answers, [idx]: e.target.value } })}
                onKeyDown={(e) => { if (e.key === "Enter" && (draft.answers[idx] ?? "").trim()) { e.preventDefault(); void applyAnswersWithAI(); } }}
              />
            </div>
          ))}
          <div className="row" style={{ gap: 6 }}>
            <input
              className="input spacer"
              value={draft.newQuestion}
              placeholder={t("desktop.specs.add_question_ph")}
              onChange={(e) => updateDraft({ newQuestion: e.target.value })}
              onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); void addQuestion(); } }}
            />
            <IconButton icon={mdiPlus} label={t("desktop.specs.add_question")} small disabled={!draft.newQuestion.trim() || busy} onClick={() => void addQuestion()} />
          </div>
        </div>

        {(stage.skills.length > 0 || stage.agents.length > 0) && (
          <div className="stack" style={{ gap: 6 }}>
            <span className="t-label">{t("desktop.specs.harness_links")}</span>
            <div className="row" style={{ flexWrap: "wrap", gap: 6 }}>
              {stage.skills.map((sk) => (
                <Chip key={`skill-${sk}`} icon={mdiPuzzleOutline} onClick={() => onOpenFile(`.agents/skills/${sk}/SKILL.md`)}>{sk}</Chip>
              ))}
              {stage.agents.map((ag) => (
                <Chip key={`agent-${ag}`} icon={mdiRobotOutline} onClick={() => onOpenFile(`.agents/agents/${ag}.md`)}>{ag}</Chip>
              ))}
            </div>
          </div>
        )}

        <div className="row" style={{ gap: 8, paddingTop: 6 }}>
          <Button small onClick={() => onCompose(reviewStagePrompt(stage), true)}>{t("desktop.specs.review")}</Button>
          <Button
            small
            variant="filled"
            icon={mdiPlay}
            onClick={() => onCompose(buildStagePrompt(stage, progress.remainingItemIds))}
          >
            {progress.remainingItemIds.length > 0 && progress.remainingItemIds.length < stage.items.length
              ? tn("desktop.specs.build_selected", progress.remainingItemIds.length)
              : t("desktop.specs.build_stage")}
          </Button>
        </div>
      </div>
    </>
  );
}
