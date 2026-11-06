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

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Background, Controls, Handle, Position, ReactFlow, type Connection, type Edge, type Node, type NodeChange, type NodeProps, type ReactFlowInstance } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { mdiAlertCircleOutline, mdiChevronDoubleLeft, mdiCreationOutline, mdiFileDocumentOutline, mdiPlus, mdiRefresh, mdiWrenchOutline } from "@mdi/js";
import { workspaces } from "../api";
import { message, reason } from "../errors";
import { compose, openFile, startSetup } from "../events";
import type { SpecStage } from "../gen/blitz/v1/workspace_pb";
import { t, tn } from "../i18n";
import { Button, Chip, Dialog, Field, Icon, MultiSelect, ResizeHandle, useSnackbar } from "@blitz/webui";
import { DraftButton } from "./Draft";
import { SpecStageInspector, statusMark, type SpecPatch } from "./SpecStageInspector";
import { canDependOn, countSpecs, getStageDraft, hasUnsavedStageDraft, layoutSpecStages, parseSpecStageDraft, setStageDraft, stageProgress, toggleSpecDependency, type SpecStageDraftState, type SpecStageInput } from "./specsGraph";

type StageNodeData = { stage: SpecStageInput; dirty?: boolean };

const sideKey = "blitz.specs.side";
const sideDefault = 380;
const storedWidth = () => {
  try { return Number(localStorage.getItem(sideKey)) || sideDefault; } catch { return sideDefault; }
};
const storeWidth = (w: number) => {
  try { localStorage.setItem(sideKey, String(w)); } catch { /* a private window keeps the default next time */ }
};

function stageInputOf(s: SpecStage): SpecStageInput {
  return {
    name: s.name, path: s.path, number: s.number, title: s.title, status: s.status || "draft", kind: s.kind,
    dependsOn: [...s.dependsOn], context: s.context, goals: [...s.goals], nonGoals: [...s.nonGoals],
    items: s.items.map((it) => ({ id: it.id, section: it.section, text: it.text, done: it.done })),
    doneWhen: [...s.doneWhen], doneWhenDone: [...s.doneWhenDone], openQuestions: [...s.openQuestions],
    skills: [...s.skills], agents: [...s.agents], content: s.content, hash: s.hash,
  };
}

function SpecStageNode({ data, selected }: NodeProps<Node<StageNodeData>>) {
  const st = data.stage;
  const mark = statusMark(st.status);
  const prog = stageProgress(st);
  const checkSummary = prog.totalChecks > 0 ? `${prog.doneChecks}/${prog.totalChecks} · ${tn("desktop.specs.items_n", st.items.length)}` : tn("desktop.specs.items_n", st.items.length);
  const sub = [checkSummary, st.openQuestions.length > 0 ? tn("desktop.specs.open_n", st.openQuestions.length) : ""].filter(Boolean).join(" · ");
  const links = [st.skills.length > 0 ? tn("desktop.specs.skills_n", st.skills.length) : "", st.agents.length > 0 ? tn("desktop.specs.agents_n", st.agents.length) : ""].filter(Boolean).join(" · ");
  return (
    <div className={`flow-node ${selected ? "selected" : ""} ${st.status === "in-progress" ? "is-running" : ""}`}>
      <Handle type="target" position={Position.Left} />
      <Icon path={mark.icon} className={mark.className} />
      <span className="lines spacer">
        <span className="ellipsis">
          {st.number ? `${st.number} · ${st.title}` : st.title}
          {data.dirty && <span className="warn-text" title={t("desktop.specs.ai_applied")}> ●</span>}
        </span>
        <small className={`ellipsis ${st.openQuestions.length > 0 ? "warn-text" : ""}`}>{sub}</small>
        {links && <small className="flow-node-meta">{links}</small>}
      </span>
      <Handle type="source" position={Position.Right} />
    </div>
  );
}

const nodeTypes = { stage: SpecStageNode };

/**
 * The workspace's specification in .agents/specs/ (Studio › Specs,
 * spec_040_studio STU-11, spec_012_memory MEM-46): its numbered stages on a
 * canvas joined by their dependencies, with the chosen stage's inspector beside it.
 */
export function SpecsPane({
  workspace,
  onClose,
  onCanvas,
}: {
  workspace: string;
  onClose: () => void;
  /** Called with whether the stage graph canvas is shown (it wants the room). */
  onCanvas?: (open: boolean) => void;
}) {
  const snack = useSnackbar();
  const [stages, setStages] = useState<SpecStageInput[]>([]);
  const [kind, setKind] = useState("");
  const [overview, setOverview] = useState("");
  const [harnessStale, setHarnessStale] = useState(false);
  const [error, setError] = useState("");
  const [selected, setSelected] = useState("");
  const [busy, setBusy] = useState(false);
  const [sideOpen, setSideOpen] = useState(true);
  const [sideWidth, setSideWidth] = useState(storedWidth);
  const [positions, setPositions] = useState<Record<string, { x: number; y: number }>>({});
  const [newOpen, setNewOpen] = useState(false);
  const [newDraft, setNewDraft] = useState<SpecStageDraftState>(() => getStageDraft(workspace, "__new__"));
  const canvasRef = useRef<HTMLDivElement>(null);
  const flowRef = useRef<ReactFlowInstance<Node<StageNodeData>, Edge> | null>(null);

  const updateNewDraft = (patch: Partial<SpecStageDraftState>) => {
    setNewDraft(setStageDraft(workspace, "__new__", patch));
  };

  const refresh = useCallback(async (selectName?: string) => {
    try {
      const r = await workspaces.listSpecs({ workspace });
      const mapped = r.stages.map(stageInputOf);
      setStages(mapped);
      setKind(r.kind);
      setOverview(r.overview);
      setHarnessStale(r.harnessStale);
      setError("");
      setSelected((cur) => {
        if (selectName && mapped.some((s) => s.name === selectName)) return selectName;
        return cur && mapped.some((s) => s.name === cur) ? cur : (mapped[0]?.name ?? "");
      });
    } catch (e) {
      setError(message(e));
    }
  }, [workspace]);

  useEffect(() => void refresh(), [refresh]);

  const hasStages = stages.length > 0;
  useEffect(() => onCanvas?.(hasStages), [hasStages, onCanvas]);
  useEffect(() => () => onCanvas?.(false), [onCanvas]);

  const layout = useMemo(() => layoutSpecStages(stages), [stages]);
  const counts = useMemo(() => countSpecs(stages), [stages]);
  const stage = stages.find((s) => s.name === selected) ?? stages[0];

  const nodes: Node<StageNodeData>[] = useMemo(
    () =>
      layout.nodes.map((n) => ({
        id: n.stage.name,
        type: "stage",
        position: positions[n.stage.name] ?? { x: n.x, y: n.y },
        data: { stage: n.stage, dirty: hasUnsavedStageDraft(workspace, n.stage.name) },
        selected: n.stage.name === (stage?.name ?? ""),
      })),
    [layout.nodes, positions, stage?.name, workspace],
  );
  const edges: Edge[] = useMemo(() => layout.edges.map((e) => ({ id: e.id, source: e.source, target: e.target, className: "flow-link is-always" })), [layout.edges]);

  const applyPatch = async (patch: SpecPatch): Promise<boolean> => {
    if (busy) return false;
    setBusy(true);
    try {
      const r = await workspaces.updateSpec({ workspace, ...patch });
      await refresh(r.stage?.name);
      return true;
    } catch (e) {
      snack(message(e), { error: true });
      if (reason(e) === "HASH_MISMATCH") await refresh(patch.name || undefined);
      return false;
    } finally {
      setBusy(false);
    }
  };

  const createNewStage = async () => {
    const title = (newDraft.title ?? "").trim();
    if (!title || busy) return;
    const ok = await applyPatch({
      name: "",
      title,
      context: (newDraft.context ?? "").trim(),
      setDependsOn: true,
      dependsOn: newDraft.deps ?? [],
      addRequirement: newDraft.newReq.trim(),
      content: newDraft.stagedContent || undefined,
    });
    if (!ok) return;
    setNewOpen(false);
    updateNewDraft({ title: "", context: "", newReq: "", deps: [], stagedContent: "", changes: [] });
    setSideOpen(true);
  };

  const onNodesChange = (changes: NodeChange<Node<StageNodeData>>[]) => {
    for (const c of changes) {
      if (c.type === "position" && c.position) {
        const pos = c.position;
        setPositions((cur) => ({ ...cur, [c.id]: pos }));
      } else if (c.type === "select" && c.selected) {
        setSelected(c.id);
        setSideOpen(true);
      }
    }
  };

  const onConnect = (c: Connection) => {
    if (!c.source || !c.target) return;
    const dst = stages.find((s) => s.name === c.target);
    if (!dst || !canDependOn(stages, dst.name, c.source)) return;
    void applyPatch({ name: dst.name, hash: dst.hash, setDependsOn: true, dependsOn: toggleSpecDependency(dst.dependsOn, c.source) });
  };

  const openAndClose = (path: string) => {
    openFile({ dir: workspace, path });
    onClose();
  };
  const composeAndClose = (text: string, run = false, fresh = false) => {
    compose({ dir: workspace, text, run, fresh });
    onClose();
  };
  const dark = document.documentElement.dataset.theme === "dark";

  return (
    <div className="workflows-pane stack" style={{ gap: 12, flex: 1, minHeight: 0 }}>
      <div className="row side-head">
        <span className="t-title-sm">{t("desktop.studio.specs")}</span>
        <span className="t-body-sm muted">{t("desktop.specs.in_folder", { dir: ".agents/specs" })}</span>
        {kind && <Chip>{kind}</Chip>}
        {hasStages ? (
          <span className="t-body-sm muted spacer ellipsis">
            {[tn("desktop.specs.stages_n", counts.stages), tn("desktop.specs.items_n", counts.items), counts.openQuestions > 0 ? tn("desktop.specs.open_n", counts.openQuestions) : "", overview].filter(Boolean).join(" · ")}
          </span>
        ) : (
          <span className="spacer" />
        )}
        {hasStages && (
          <Button small icon={mdiFileDocumentOutline} onClick={() => openAndClose(".agents/specs/README.md")}>
            {t("desktop.specs.open_index")}
          </Button>
        )}
        <Button small variant="filled" icon={mdiPlus} onClick={() => { if (!newDraft.deps?.length && stage) updateNewDraft({ deps: [stage.name] }); setNewOpen(true); }}>
          {t("desktop.specs.new_stage")}
        </Button>
        <Button small variant="tonal" icon={mdiCreationOutline} onClick={() => composeAndClose(hasStages ? "/spec " : "/spec", !hasStages, !hasStages)}>
          {t(hasStages ? "desktop.specs.revise" : "desktop.specs.write")}
        </Button>
      </div>

      {error && (
        <div className="card error row small-card" role="alert">
          <Icon path={mdiAlertCircleOutline} size="sm" /> {error}
        </div>
      )}

      {harnessStale && (
        <div className="card warn row small-card">
          <Icon path={mdiAlertCircleOutline} size="sm" />
          <span className="spacer t-body-sm">{t("desktop.specs.harness_stale")}</span>
          <Button small icon={mdiRefresh} onClick={() => composeAndClose("/skills update", true)}>
            {t("desktop.specs.update_harness")}
          </Button>
        </div>
      )}

      {!hasStages ? (
        <div className="card stack" style={{ gap: 10 }}>
          <p className="t-body-sm muted" style={{ margin: 0 }}>{t("desktop.specs.empty")}</p>
          <div className="row" style={{ gap: 8 }}>
            <Button variant="filled" icon={mdiPlus} onClick={() => setNewOpen(true)}>{t("desktop.specs.new_stage")}</Button>
            <Button variant="tonal" icon={mdiCreationOutline} onClick={() => composeAndClose("/spec", true, true)}>{t("desktop.specs.write")}</Button>
            <Button icon={mdiWrenchOutline} onClick={() => { startSetup(workspace); onClose(); }}>{t("desktop.specs.setup")}</Button>
          </div>
        </div>
      ) : (
        <div className="flow-body">
          <div className="flow-main">
            <div className="flow-canvas" ref={canvasRef}>
              <ReactFlow
                nodes={nodes}
                edges={edges}
                nodeTypes={nodeTypes}
                onNodesChange={onNodesChange}
                onConnect={onConnect}
                colorMode={dark ? "dark" : "light"}
                onInit={(flow: ReactFlowInstance<Node<StageNodeData>, Edge>) => {
                  flowRef.current = flow;
                  setTimeout(() => void flow.fitView({ padding: 0.18, maxZoom: 1, minZoom: 0.55 }), 250);
                }}
                proOptions={{ hideAttribution: true }}
              >
                <Background gap={20} />
                <Controls showInteractive={false} position="bottom-right" />
              </ReactFlow>
            </div>
          </div>

          <aside className={`flow-side ${sideOpen ? "" : "is-closed"}`} style={sideOpen ? { width: sideWidth } : undefined} aria-label={t("desktop.specs.inspector")}>
            {!sideOpen ? (
              <button type="button" className="flow-side-rail" onClick={() => setSideOpen(true)} title={t("desktop.flows.side_show")} aria-label={t("desktop.flows.side_show")}>
                <Icon path={mdiChevronDoubleLeft} size="sm" />
                <span>{stage ? `${stage.number} · ${stage.title}` : t("desktop.specs.inspector")}</span>
              </button>
            ) : (
              <>
                <ResizeHandle
                  width={sideWidth}
                  edge="left"
                  min={300}
                  max={() => (canvasRef.current?.parentElement?.parentElement?.getBoundingClientRect().width ?? 1200) * 0.55}
                  label={t("desktop.flows.side_resize")}
                  onResize={(w) => { const v = w || sideDefault; setSideWidth(v); storeWidth(v); }}
                />
                {stage && (
                  <SpecStageInspector
                    workspace={workspace}
                    stage={stage}
                    stages={stages}
                    busy={busy}
                    onPatch={applyPatch}
                    onHide={() => setSideOpen(false)}
                    onOpenFile={openAndClose}
                    onCompose={(text, run) => composeAndClose(text, run)}
                  />
                )}
              </>
            )}
          </aside>
        </div>
      )}

      {newOpen && (
        <Dialog
          title={t("desktop.specs.new_stage")}
          icon={mdiPlus}
          onClose={() => setNewOpen(false)}
          footer={
            <>
              <DraftButton
                workspace={workspace}
                kind="spec_stage"
                name={(newDraft.title ?? "").trim() || "new-stage"}
                description={(newDraft.title ?? "").trim()}
                current={newDraft.stagedContent}
                disabled={busy}
                onUse={(md, changes) => {
                  const parsed = parseSpecStageDraft({ name: "001-new", path: "", number: "", title: newDraft.title ?? "", status: "draft", kind: "", dependsOn: [], context: newDraft.context ?? "", goals: [], nonGoals: [], items: [], doneWhen: [], openQuestions: [], skills: [], agents: [] }, md);
                  updateNewDraft({ title: (newDraft.title ?? "").trim() || parsed.title, context: parsed.context || newDraft.context, stagedContent: md, changes });
                }}
              />
              <Button onClick={() => setNewOpen(false)}>{t("desktop.cancel")}</Button>
              <Button variant="filled" icon={mdiPlus} disabled={!(newDraft.title ?? "").trim() || busy} onClick={() => void createNewStage()}>
                {t("desktop.specs.create_stage")}
              </Button>
            </>
          }
        >
          <div className="stack" style={{ gap: 10 }}>
            <Field label={t("desktop.specs.stage_title")}>
              {(id) => (
                <input id={id} className="input" autoFocus value={newDraft.title ?? ""} placeholder={t("desktop.specs.stage_title_ph")} onChange={(e) => updateNewDraft({ title: e.target.value })} />
              )}
            </Field>
            {stages.length > 0 && (
              <Field label={t("desktop.specs.depends_on")}>
                {(id) => (
                  <MultiSelect
                    id={id}
                    label={t("desktop.specs.depends_on")}
                    none={t("desktop.specs.depends_none")}
                    chosen={newDraft.deps ?? []}
                    options={stages.map((s) => ({ value: s.name, detail: s.title }))}
                    onChange={(deps) => updateNewDraft({ deps })}
                  />
                )}
              </Field>
            )}
            <Field label={t("desktop.specs.context")}>
              {(id) => (
                <textarea id={id} className="input" rows={2} value={newDraft.context ?? ""} placeholder={t("desktop.specs.context_ph")} onChange={(e) => updateNewDraft({ context: e.target.value })} />
              )}
            </Field>
            <Field label={t("desktop.specs.add_requirement")}>
              {(id) => (
                <input id={id} className="input" value={newDraft.newReq} placeholder={t("desktop.specs.add_requirement_ph")} onChange={(e) => updateNewDraft({ newReq: e.target.value })} />
              )}
            </Field>
            {newDraft.stagedContent && (
              <div className="card warn stack" style={{ gap: 6 }}>
                <div className="row" style={{ gap: 6 }}>
                  <span className="t-label spacer">{t("desktop.specs.ai_applied")}</span>
                  <Button small onClick={() => updateNewDraft({ stagedContent: "", changes: [] })}>{t("desktop.file.revert")}</Button>
                </div>
                <textarea className="input mono draft-text" rows={6} value={newDraft.stagedContent} onChange={(e) => updateNewDraft({ stagedContent: e.target.value })} />
              </div>
            )}
          </div>
        </Dialog>
      )}
    </div>
  );
}
