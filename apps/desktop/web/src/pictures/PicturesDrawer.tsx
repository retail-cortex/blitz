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
import { createPortal } from "react-dom";
import {
  mdiAlertCircleOutline,
  mdiAt,
  mdiBrushVariant,
  mdiChevronDown,
  mdiChevronRight,
  mdiClose,
  mdiContentCopy,
  mdiContentSave,
  mdiContentSaveEditOutline,
  mdiDeleteOutline,
  mdiFolderOpenOutline,
  mdiHistory,
  mdiImagePlusOutline,
  mdiLoading,
  mdiPlus,
  mdiRefresh,
  mdiSourceBranch,
} from "@mdi/js";
import { Button, Dialog, Drawer, Field, Icon, IconButton, Menu, Switch, useSnackbar, type MenuEntry } from "@blitz/webui";
import { files, workspaces } from "../api";
import { copyPicture } from "../clipboard";
import { message } from "../errors";
import { addToContext, filesTouched, openFile } from "../events";
import { readDocDrop } from "../files/docDrop";
import type { PictureFile, PictureModel, PictureSession, PictureTurn } from "../gen/blitz/v1/workspace_pb";
import { t, tn } from "../i18n";
import { cost } from "../workflowRuns";
import { controls, fitSettings, keepSettings, loadSettings, maxReferenceBytes, panoramic, ratioShape, refProblem, referenceTypes, roles, type Ref, type Role, type Settings } from "./pictures";
import "./pictures.css";

/** How Pictures was opened: a first prompt, a picture to start from, a folder to save in (PIC-10). */
export interface PicturesStart {
  prompt?: string;
  reference?: string;
  folder?: string;
}

// Pictures fetched once per session and picture, shown from object URLs.
const pictureURLs = new Map<string, Promise<string>>();

/** The URL showing a session's picture (or kept reference). */
function usePicture(dir: string, session: string, picture: string): string {
  const [url, setURL] = useState("");
  useEffect(() => {
    let live = true;
    const key = `${dir}\x00${session}\x00${picture}`;
    let p = pictureURLs.get(key);
    if (!p) {
      p = workspaces.readPicture({ workspace: dir, session, picture }).then((r) => URL.createObjectURL(new Blob([r.data as BlobPart], { type: r.mime })));
      pictureURLs.set(key, p);
      p.catch(() => pictureURLs.delete(key));
    }
    p.then((u) => live && setURL(u), () => {});
    return () => {
      live = false;
    };
  }, [dir, session, picture]);
  return url;
}

let refKeys = 0;
const nextKey = () => `ref-${++refKeys}`;

/** A file's MIME type from its name. */
function mimeOf(name: string): string {
  const ext = name.toLowerCase().split(".").pop() ?? "";
  return ext === "jpg" || ext === "jpeg" ? "image/jpeg" : ext === "webp" ? "image/webp" : ext === "png" ? "image/png" : "";
}

// Where pictures go, said once per workspace before the first picture is sent (PIC-71).
const toldKey = (dir: string) => `blitz.pictures.told.${dir}`;
function told(dir: string): boolean {
  try {
    return localStorage.getItem(toldKey(dir)) === "1";
  } catch {
    return false;
  }
}
function tell(dir: string) {
  try {
    localStorage.setItem(toldKey(dir), "1");
  } catch {
    // storage refused: asked again next time
  }
}

/**
 * Pictures (spec_058_pictures): a place to make pictures with an image
 * model. Its controls follow what the model can do (the catalog, PIC-01);
 * a session is a conversation for Gemini models (PIC-41); every picture
 * can be saved into the workspace, opened, used as a reference, refined,
 * copied or added to the chat (PIC-53).
 */
export function PicturesDrawer({ dir, start, onClose }: { dir: string; start?: PicturesStart; onClose: () => void }) {
  const snack = useSnackbar();
  const [models, setModels] = useState<PictureModel[] | null>(null);
  const [folder, setFolder] = useState(start?.folder ?? "");
  const [settings, setSettings] = useState<Settings>(() => loadSettings(dir));
  const [prompt, setPrompt] = useState(start?.prompt ?? "");
  const [refs, setRefs] = useState<Ref[]>([]);
  const [role, setRole] = useState<Role>("object");
  const [session, setSession] = useState<PictureSession | null>(null);
  const [sessions, setSessions] = useState<PictureSession[]>([]);
  const [startFrom, setStartFrom] = useState("");
  const [busy, setBusy] = useState(false);
  const [estimate, setEstimate] = useState<{ usd: number; priced: boolean } | null>(null);
  // Why the turn as set up won't be taken (the estimate's refusal), and why
  // the last one failed (the model's or the service's answer): the first
  // follows the settings, the second stays until something changes.
  const [problem, setProblem] = useState("");
  const [failure, setFailure] = useState("");
  const [advanced, setAdvanced] = useState(false);
  const [folded, setFolded] = useState(false);
  const [picking, setPicking] = useState(false);
  const [savingAs, setSavingAs] = useState<{ picture: string; path: string } | null>(null);
  const [asking, setAsking] = useState(false);
  // The picture shown full size (its URL), over everything.
  const [zoom, setZoom] = useState("");
  const end = useRef<HTMLDivElement>(null);

  const model = models?.find((m) => m.ref === settings.model);
  // Built once per model: the request (and its estimate) follows what changes, not every render.
  const can = useMemo(() => controls(model), [model]);

  // The models offered, the settings fitted to the chosen one.
  useEffect(() => {
    workspaces.listPictureModels({ workspace: dir }).then(
      (r) => {
        setModels(r.models);
        if (!start?.folder) setFolder(r.folder);
        setSettings((s) => {
          const m = r.models.find((x) => x.ref === s.model) ?? r.models.find((x) => x.default) ?? r.models[0];
          return m ? fitSettings(s, m).settings : s;
        });
      },
      (e: unknown) => {
        setModels([]);
        snack(message(e), { error: true });
      },
    );
  }, [dir, snack, start?.folder]);
  const listSessions = useCallback(() => {
    workspaces.listPictureSessions({ workspace: dir }).then(
      (r) => setSessions(r.sessions),
      () => setSessions([]),
    );
  }, [dir]);
  useEffect(listSessions, [listSessions]);
  useEffect(() => keepSettings(dir, settings), [dir, settings]);
  // A picture from the file tree to start from.
  useEffect(() => {
    if (start?.reference) setRefs([{ key: nextKey(), role: "object", name: start.reference.split("/").pop() ?? start.reference, mime: mimeOf(start.reference), path: start.reference, url: "" }]);
  }, [start?.reference]);
  useEffect(() => {
    end.current?.scrollIntoView({ block: "end" });
  }, [session?.turns.length, busy]);

  const set = <K extends keyof Settings>(k: K, v: Settings[K]) => setSettings((s) => ({ ...s, [k]: v }));
  const chooseModel = (ref: string) => {
    const m = models?.find((x) => x.ref === ref);
    if (!m) return;
    const { settings: next, resets } = fitSettings(settings, m);
    setSettings(next);
    for (const r of resets) snack(t(`desktop.pictures.reset.${r.setting}`, { from: r.from, to: r.to, model: m.name }));
    if (m.maxReferences < refs.length) {
      setRefs((rs) => rs.slice(0, m.maxReferences));
      if (refs.length) snack(tn("desktop.pictures.refs_dropped", refs.length - m.maxReferences, { model: m.name }));
    }
  };

  // The request, as the service takes it.
  const request = useCallback(
    (estimateOnly: boolean) => ({
      workspace: dir,
      session: session?.id ?? "",
      model: settings.model,
      prompt: prompt.trim(),
      references: refs.map((r) => ({ path: r.path ?? "", picture: r.picture ?? "", data: r.data ?? new Uint8Array(), mime: r.mime, name: r.name, role: r.role })),
      count: can.countLocked ? 1 : settings.count,
      aspectRatio: settings.ratio,
      size: can.sizes ? settings.size : "",
      thinking: can.thinking ? settings.thinking : "",
      search: can.search && settings.search,
      searchImages: can.searchImages && settings.search && settings.searchImages,
      avoid: can.avoid ? settings.avoid : "",
      people: can.people ? settings.people : "",
      seed: Number(settings.seed) || 0,
      useSeed: can.seed && settings.seed.trim() !== "",
      enhance: can.enhance && settings.enhance,
      format: can.formats ? settings.format : "",
      quality: settings.format === "image/jpeg" ? settings.quality : 0,
      startFrom,
      estimate: estimateOnly,
    }),
    [dir, session?.id, settings, prompt, refs, can, startFrom],
  );

  // What the turn will cost, about (PIC-70), as it's set up.
  useEffect(() => {
    if (!model || !prompt.trim()) {
      setEstimate(null);
      return;
    }
    setFailure("");
    const timer = setTimeout(() => {
      workspaces.makePicture(request(true)).then(
        (r) => {
          setEstimate({ usd: r.costUsd, priced: r.priced });
          setProblem("");
        },
        (e: unknown) => {
          setEstimate(null);
          setProblem(message(e));
        },
      );
    }, 400);
    return () => clearTimeout(timer);
  }, [model, prompt, request]);

  const make = async () => {
    if (!model || !prompt.trim() || busy) return;
    if ((refs.length > 0 || (session?.turns.length ?? 0) > 0) && !told(dir)) {
      setAsking(true);
      return;
    }
    setBusy(true);
    setFailure("");
    try {
      const r = await workspaces.makePicture(request(false));
      if (r.session) setSession(r.session);
      const last = r.session?.turns.at(-1);
      listSessions();
      if (last?.problem) {
        setFailure(last.problem); // the prompt stays, to try again
        return;
      }
      setPrompt("");
      setStartFrom("");
      if (model.kind === "gemini") setRefs([]);
    } catch (e) {
      setFailure(message(e));
    } finally {
      setBusy(false);
    }
  };

  // References (PIC-23): from the computer, the tree, the workspace, the session.
  const addRef = (r: Omit<Ref, "key" | "role">) => {
    const why = refProblem(model, refs, role);
    if (why) {
      snack(t(`desktop.pictures.ref_${why}`, { model: model?.name ?? "", count: why === "role" ? (role === "character" ? model?.maxCharacters : model?.maxObjects) ?? 0 : (model?.maxReferences ?? 0) }), { error: true });
      return;
    }
    setRefs((rs) => [...rs, { ...r, key: nextKey(), role }]);
  };
  const addFiles = async (list: File[]) => {
    for (const f of list) {
      const mime = f.type || mimeOf(f.name);
      if (!referenceTypes.includes(mime)) {
        snack(t("desktop.pictures.ref_type", { name: f.name }), { error: true });
        continue;
      }
      if (f.size > maxReferenceBytes) {
        snack(t("desktop.pictures.ref_large", { name: f.name }), { error: true });
        continue;
      }
      const data = new Uint8Array(await f.arrayBuffer());
      addRef({ name: f.name, mime, data, url: URL.createObjectURL(f) });
    }
  };
  const drop = (e: React.DragEvent) => {
    const d = readDocDrop(e.dataTransfer);
    if (!d) return;
    e.preventDefault();
    for (const p of d.paths) {
      if (!mimeOf(p)) snack(t("desktop.pictures.ref_type", { name: p }), { error: true });
      else addRef({ name: p.split("/").pop() ?? p, mime: mimeOf(p), path: p, url: "" });
    }
    void addFiles(d.files);
  };
  const picker = useRef<HTMLInputElement>(null);

  const open = async (id: string) => {
    try {
      setSession((await workspaces.getPictureSession({ workspace: dir, id })).session ?? null);
      setStartFrom("");
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const fresh = () => {
    setSession(null);
    setStartFrom("");
    setRefs([]);
  };
  const remove = async (id: string) => {
    try {
      await workspaces.deletePictureSession({ workspace: dir, id });
      if (session?.id === id) fresh();
      listSessions();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };

  const save = async (picture: string, path = "") => {
    if (!session) return "";
    try {
      const r = await workspaces.savePicture({ workspace: dir, session: session.id, picture, path });
      filesTouched({ dir });
      setSession((await workspaces.getPictureSession({ workspace: dir, id: session.id })).session ?? session);
      snack(t("desktop.pictures.saved", { path: r.path }));
      return r.path;
    } catch (e) {
      snack(message(e), { error: true });
      return "";
    }
  };

  const history: MenuEntry[] = [
    ...(sessions.length
      ? sessions.map((s): MenuEntry => ({ label: s.name, detail: tn("desktop.pictures.turns", s.turns.length), icon: mdiImagePlusOutline, on: s.id === session?.id, onSelect: () => void open(s.id) }))
      : [{ label: t("desktop.pictures.no_sessions"), onSelect: () => {} }]),
    ...(session ? (["divider", { label: t("desktop.pictures.delete_session"), icon: mdiDeleteOutline, danger: true, onSelect: () => void remove(session.id) }] as MenuEntry[]) : []),
  ];

  const ratios = useMemo(() => [...(model?.ratios ?? []).filter((r) => !panoramic.has(r)), ...(model?.ratios ?? []).filter((r) => panoramic.has(r))], [model]);
  const startPicture = startFrom && session ? session.turns.flatMap((tn2) => tn2.pictures).find((p) => p.id === startFrom) : undefined;

  return (
    <Drawer
      title={t("desktop.pictures.title")}
      icon={mdiBrushVariant}
      className="pictures-drawer"
      onClose={onClose}
      headerActions={
        <>
          <IconButton icon={mdiPlus} label={t("desktop.pictures.new_session")} onClick={fresh} />
          <Menu trigger={(p) => <IconButton {...p} icon={mdiHistory} label={t("desktop.pictures.history")} />} placement="down end" items={history} />
        </>
      }
    >
      {models === null ? (
        <p className="muted">{t("desktop.checking")}</p>
      ) : models.length === 0 ? (
        <div className="card outlined stack pictures-none" style={{ gap: 8 }}>
          <span className="t-title-sm">{t("desktop.pictures.none_title")}</span>
          <span className="t-body-md">{t("desktop.pictures.none")}</span>
        </div>
      ) : (
        <div className={`pictures ${folded ? "is-folded" : ""}`}>
          <aside className="pictures-controls" aria-label={t("desktop.pictures.controls")}>
            <div className="row pictures-controls-head">
              <IconButton icon={folded ? mdiChevronRight : mdiChevronDown} small label={t(folded ? "desktop.pictures.unfold" : "desktop.pictures.fold")} onClick={() => setFolded((f) => !f)} />
              {!folded && <span className="t-title-sm">{t("desktop.pictures.controls")}</span>}
            </div>
            {!folded && (
              <div className="stack pictures-fields">
                <Field label={t("desktop.pictures.model")} supporting={model?.assumed ? t("desktop.pictures.assumed") : model ? modelPrice(model, settings.size) : undefined}>
                  {(id) => (
                    <select id={id} className="select" value={settings.model} onChange={(e) => chooseModel(e.target.value)}>
                      {(["gemini", "imagen", "openai"] as const).map((kind) => {
                        const of = models.filter((m) => m.kind === kind);
                        return of.length ? (
                          <optgroup key={kind} label={t(`desktop.pictures.kind.${kind}`)}>
                            {of.map((m) => (
                              <option key={m.ref} value={m.ref}>
                                {m.organization ? t("desktop.pictures.org_model", { name: m.name }) : m.name}
                              </option>
                            ))}
                          </optgroup>
                        ) : null;
                      })}
                    </select>
                  )}
                </Field>

                {can.references && (
                  <div className="stack" style={{ gap: 6 }}>
                    <span className="t-label">{t("desktop.pictures.references", { count: refs.length, max: model?.maxReferences ?? 0 })}</span>
                    <div
                      className="pictures-drop"
                      role="button"
                      tabIndex={0}
                      aria-label={t("desktop.pictures.drop")}
                      onClick={(e) => {
                        // A click on the zone (not a thumbnail's role or ✕) picks files from the computer.
                        if (!(e.target as HTMLElement).closest(".pictures-ref")) picker.current?.click();
                      }}
                      onKeyDown={(e) => {
                        if ((e.key === "Enter" || e.key === " ") && e.target === e.currentTarget) {
                          e.preventDefault();
                          picker.current?.click();
                        }
                      }}
                      onDragOver={(e) => e.preventDefault()}
                      onDrop={drop}
                      onPaste={(e) => void addFiles(Array.from(e.clipboardData.files))}
                    >
                      {refs.length === 0 ? (
                        <span className="row t-body-sm muted pictures-drop-hint" style={{ gap: 6 }}>
                          <Icon path={mdiImagePlusOutline} size="sm" /> {t("desktop.pictures.drop")}
                        </span>
                      ) : (
                        <span className="t-body-sm muted">{t("desktop.pictures.drop_more")}</span>
                      )}
                      <div className="pictures-refs">
                        {refs.map((r) => (
                          <RefThumb key={r.key} dir={dir} session={session?.id ?? ""} r={r} onRole={(v) => setRefs((rs) => rs.map((x) => (x.key === r.key ? { ...x, role: v } : x)))} onRemove={() => setRefs((rs) => rs.filter((x) => x.key !== r.key))} />
                        ))}
                      </div>
                    </div>
                    <div className="row wrap" style={{ gap: 6 }}>
                      <select className="select pictures-role" aria-label={t("desktop.pictures.role")} value={role} onChange={(e) => setRole(e.target.value as Role)}>
                        {roles.map((r) => (
                          <option key={r} value={r}>
                            {t(`desktop.pictures.role.${r}`)}
                          </option>
                        ))}
                      </select>
                      <Button small icon={mdiFolderOpenOutline} onClick={() => setPicking(true)}>
                        {t("desktop.pictures.from_workspace")}
                      </Button>
                      <input
                        ref={picker}
                        type="file"
                        accept={referenceTypes.join(",")}
                        multiple
                        hidden
                        onChange={(e) => {
                          void addFiles(Array.from(e.target.files ?? []));
                          e.target.value = "";
                        }}
                      />
                    </div>
                  </div>
                )}

                <div className="stack" style={{ gap: 6 }}>
                  <span className="t-label">{t("desktop.pictures.count")}</span>
                  {can.countLocked ? (
                    <span className="t-body-sm muted" title={t("desktop.pictures.count_locked")}>
                      {t("desktop.pictures.count_one")} · {t("desktop.pictures.count_locked")}
                    </span>
                  ) : (
                    <div className="row pictures-pills" role="radiogroup" aria-label={t("desktop.pictures.count")}>
                      {Array.from({ length: Math.max(model?.maxPictures ?? 1, 1) }, (_, i) => i + 1).map((n) => (
                        <button key={n} type="button" role="radio" aria-checked={settings.count === n} className={`chip ${settings.count === n ? "selected" : ""}`} onClick={() => set("count", n)}>
                          {n}
                        </button>
                      ))}
                    </div>
                  )}
                </div>

                <div className="stack" style={{ gap: 6 }}>
                  <span className="t-label">{t("desktop.pictures.shape")}</span>
                  <div className="pictures-ratios" role="radiogroup" aria-label={t("desktop.pictures.shape")}>
                    {ratios.map((r) => {
                      const [w, h] = ratioShape(r);
                      return (
                        <button key={r} type="button" role="radio" aria-checked={settings.ratio === r} className={`pictures-ratio ${settings.ratio === r ? "selected" : ""}`} title={r} onClick={() => set("ratio", r)}>
                          <span className="pictures-ratio-shape" style={{ width: w, height: h }} />
                          <span className="t-body-sm">{r}</span>
                        </button>
                      );
                    })}
                  </div>
                </div>

                {can.sizes && (
                  <div className="stack" style={{ gap: 6 }}>
                    <span className="t-label">{t("desktop.pictures.size")}</span>
                    <div className="row pictures-pills" role="radiogroup" aria-label={t("desktop.pictures.size")}>
                      {model?.sizes.map((s) => (
                        <button key={s} type="button" role="radio" aria-checked={settings.size === s} className={`chip ${settings.size === s ? "selected" : ""}`} onClick={() => set("size", s)}>
                          {s}
                        </button>
                      ))}
                    </div>
                  </div>
                )}

                {can.thinking && (
                  <div className="stack" style={{ gap: 6 }}>
                    <span className="t-label">{t("desktop.pictures.thinking")}</span>
                    <div className="row pictures-pills" role="radiogroup" aria-label={t("desktop.pictures.thinking")}>
                      {model?.thinking.map((l) => (
                        <button key={l} type="button" role="radio" aria-checked={settings.thinking === l} className={`chip ${settings.thinking === l ? "selected" : ""}`} onClick={() => set("thinking", l)}>
                          {t(`desktop.pictures.level.${l}`)}
                        </button>
                      ))}
                    </div>
                  </div>
                )}

                {can.search && (
                  <div className="stack" style={{ gap: 6 }}>
                    <label className="row" style={{ gap: 8 }}>
                      <Switch checked={settings.search} label={t("desktop.pictures.search")} onChange={(v) => set("search", v)} />
                      <span className="t-body-md">{t("desktop.pictures.search")}</span>
                    </label>
                    {can.searchImages && settings.search && (
                      <label className="row" style={{ gap: 8 }}>
                        <Switch checked={settings.searchImages} label={t("desktop.pictures.search_images")} onChange={(v) => set("searchImages", v)} />
                        <span className="t-body-md">{t("desktop.pictures.search_images")}</span>
                      </label>
                    )}
                  </div>
                )}

                <button type="button" className="row pictures-advanced-toggle t-label" aria-expanded={advanced} onClick={() => setAdvanced((a) => !a)}>
                  <Icon path={advanced ? mdiChevronDown : mdiChevronRight} size="sm" /> {t("desktop.pictures.advanced")}
                </button>
                {advanced && (
                  <div className="stack pictures-advanced" style={{ gap: 10 }}>
                    {can.avoid ? (
                      <Field label={t("desktop.pictures.avoid")}>{(id) => <input id={id} className="input" value={settings.avoid} placeholder={t("desktop.pictures.avoid_placeholder")} onChange={(e) => set("avoid", e.target.value)} />}</Field>
                    ) : (
                      <span className="t-body-sm muted">{t("desktop.pictures.avoid_in_prompt")}</span>
                    )}
                    {can.people && (
                      <Field label={t("desktop.pictures.people")}>
                        {(id) => (
                          <select id={id} className="select" value={settings.people} onChange={(e) => set("people", e.target.value)}>
                            <option value="">{t("desktop.pictures.people_default")}</option>
                            {["DONT_ALLOW", "ALLOW_ADULT", "ALLOW_ALL"].map((p) => (
                              <option key={p} value={p}>
                                {t(`desktop.pictures.people.${p}`)}
                              </option>
                            ))}
                          </select>
                        )}
                      </Field>
                    )}
                    {can.formats && (
                      <Field label={t("desktop.pictures.format")}>
                        {(id) => (
                          <select id={id} className="select" value={settings.format} onChange={(e) => set("format", e.target.value)}>
                            {model?.formats.map((f) => (
                              <option key={f} value={f}>
                                {f.replace("image/", "").toUpperCase()}
                              </option>
                            ))}
                          </select>
                        )}
                      </Field>
                    )}
                    {settings.format === "image/jpeg" && (
                      <label className="stack" style={{ gap: 4 }}>
                        <span className="t-label">{t("desktop.pictures.quality", { quality: settings.quality })}</span>
                        <input type="range" min={10} max={100} value={settings.quality} onChange={(e) => set("quality", Number(e.target.value))} />
                      </label>
                    )}
                    {can.seed && (
                      <Field label={t("desktop.pictures.seed")} supporting={t("desktop.pictures.seed_hint")}>
                        {(id) => <input id={id} className="input mono" inputMode="numeric" value={settings.seed} onChange={(e) => set("seed", e.target.value.replace(/[^0-9]/g, ""))} />}
                      </Field>
                    )}
                    {can.enhance && (
                      <label className="row" style={{ gap: 8 }}>
                        <Switch checked={settings.enhance} label={t("desktop.pictures.enhance")} onChange={(v) => set("enhance", v)} />
                        <span className="t-body-md">{t("desktop.pictures.enhance")}</span>
                      </label>
                    )}
                    <span className="t-body-sm muted">{t("desktop.pictures.synthid")}</span>
                  </div>
                )}
              </div>
            )}
          </aside>

          <section className="pictures-session">
            <div className="pictures-turns">
              {!session || session.turns.length === 0 ? (
                <div className="pictures-empty muted">
                  <Icon path={mdiBrushVariant} />
                  <p className="t-body-md">{t(model?.kind === "imagen" ? "desktop.pictures.empty_imagen" : "desktop.pictures.empty")}</p>
                </div>
              ) : (
                session.turns.map((turn) => (
                  <Turn
                    key={turn.id}
                    dir={dir}
                    session={session.id}
                    turn={turn}
                    onZoom={setZoom}
                    onSave={(id) => void save(id)}
                    onSaveAs={(id) => setSavingAs({ picture: id, path: `${folder}/` })}
                    onReference={(p) => addRef({ name: t("desktop.pictures.made_picture"), mime: p.mime, picture: p.id, url: "" })}
                    onStartFrom={(id) => setStartFrom(id)}
                    onChat={async (p) => {
                      const path = p.saved[0] || (await save(p.id));
                      if (path) addToContext({ dir, path });
                    }}
                    onDelete={async (id) => {
                      try {
                        setSession((await workspaces.deletePicture({ workspace: dir, session: session.id, picture: id })).session ?? session);
                      } catch (e) {
                        snack(message(e), { error: true });
                      }
                    }}
                    onCopy={(p) =>
                      void workspaces
                        .readPicture({ workspace: dir, session: session.id, picture: p.id })
                        .then((r) => copyPicture(r.data, r.mime))
                        .then(
                          () => snack(t("desktop.pictures.copied")),
                          (e: unknown) => snack(message(e), { error: true }),
                        )
                    }
                  />
                ))
              )}
              {busy && (
                <div className="row t-body-sm muted pictures-making" style={{ gap: 6 }}>
                  <Icon path={mdiLoading} spin size="sm" /> {t("desktop.pictures.making")}
                </div>
              )}
              <div ref={end} />
            </div>
            <div className="pictures-composer">
              {startPicture && session && (
                <span className="chip static row" style={{ gap: 6 }}>
                  <Thumb dir={dir} session={session.id} picture={startPicture.id} size={20} />
                  {t("desktop.pictures.refining")}
                  <IconButton icon={mdiClose} small label={t("desktop.pictures.stop_refining")} onClick={() => setStartFrom("")} />
                </span>
              )}
              {failure && (
                <div className="card error row small-card pictures-failure" role="alert" style={{ gap: 8 }}>
                  <Icon path={mdiAlertCircleOutline} size="sm" />
                  <span className="t-body-sm spacer">{failure}</span>
                  <IconButton icon={mdiClose} small label={t("desktop.close")} onClick={() => setFailure("")} />
                </div>
              )}
              <textarea
                className="input pictures-prompt"
                rows={3}
                value={prompt}
                autoFocus
                placeholder={t(session?.turns.length && model?.kind === "gemini" ? "desktop.pictures.prompt_next" : "desktop.pictures.prompt_placeholder")}
                aria-label={t("desktop.pictures.prompt")}
                onChange={(e) => setPrompt(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                    e.preventDefault();
                    void make();
                  }
                }}
              />
              <div className="row" style={{ gap: 8 }}>
                <span className={`t-body-sm spacer ${problem ? "error-text" : "muted"}`}>
                  {problem || (estimate ? (estimate.priced ? t("desktop.pictures.estimate", { cost: cost(estimate.usd) }) : t("desktop.pictures.unpriced")) : model?.kind === "gemini" && session?.turns.length ? t("desktop.pictures.conversation") : "")}
                </span>
                <Button variant="filled" icon={busy ? mdiLoading : mdiBrushVariant} iconSpin={busy} disabled={busy || !prompt.trim() || !!problem} onClick={() => void make()}>
                  {busy ? t("desktop.pictures.making") : t("desktop.pictures.make")}
                </Button>
              </div>
            </div>
          </section>

        </div>
      )}
      {zoom && <Viewer url={zoom} onClose={() => setZoom("")} />}
      {picking && <WorkspacePicker dir={dir} onPick={(p) => addRef({ name: p.split("/").pop() ?? p, mime: mimeOf(p), path: p, url: "" })} onClose={() => setPicking(false)} />}
      {savingAs && (
        <Dialog
          title={t("desktop.pictures.save_as")}
          icon={mdiContentSaveEditOutline}
          onClose={() => setSavingAs(null)}
          footer={
            <>
              <Button onClick={() => setSavingAs(null)}>{t("desktop.cancel")}</Button>
              <Button
                variant="filled"
                disabled={!savingAs.path.trim()}
                onClick={() => {
                  const s = savingAs;
                  setSavingAs(null);
                  void save(s.picture, s.path.trim());
                }}
              >
                {t("desktop.pictures.save")}
              </Button>
            </>
          }
        >
          <Field label={t("desktop.pictures.save_as_path")} supporting={t("desktop.pictures.save_as_hint")}>
            {(id) => <input id={id} className="input mono" autoFocus value={savingAs.path} onChange={(e) => setSavingAs({ ...savingAs, path: e.target.value })} />}
          </Field>
        </Dialog>
      )}
      {asking && (
        <Dialog
          title={t("desktop.pictures.privacy_title")}
          icon={mdiBrushVariant}
          onClose={() => setAsking(false)}
          footer={
            <>
              <Button onClick={() => setAsking(false)}>{t("desktop.cancel")}</Button>
              <Button
                variant="filled"
                onClick={() => {
                  tell(dir);
                  setAsking(false);
                  void make();
                }}
              >
                {t("desktop.pictures.privacy_ok")}
              </Button>
            </>
          }
        >
          <p>{t(model?.organization ? "desktop.pictures.privacy_org" : "desktop.pictures.privacy_google")}</p>
        </Dialog>
      )}
    </Drawer>
  );
}

/** A model's price, as the model field says it (PIC-21). */
function modelPrice(m: PictureModel, size: string): string {
  if (m.kind === "imagen" || m.kind === "openai") return m.perPicture > 0 ? t("desktop.pictures.price_each", { cost: cost(m.perPicture) }) : "";
  const tokens = m.sizeTokens[size] ?? m.sizeTokens["1K"] ?? m.sizeTokens[""] ?? 0;
  return m.outputPerMtok > 0 && tokens > 0 ? t("desktop.pictures.price_about", { cost: cost((tokens * m.outputPerMtok) / 1e6) }) : "";
}

/** A picture of the session, small. */
function Thumb({ dir, session, picture, size }: { dir: string; session: string; picture: string; size: number }) {
  const url = usePicture(dir, session, picture);
  return url ? <img className="pictures-thumb" src={url} alt="" style={{ width: size, height: size }} /> : <span className="pictures-thumb" style={{ width: size, height: size }} />;
}

/** A reference's thumbnail, with its role and ✕ (PIC-23). */
function RefThumb({ dir, session, r, onRole, onRemove }: { dir: string; session: string; r: Ref; onRole: (role: Role) => void; onRemove: () => void }) {
  const [url, setURL] = useState(r.url);
  useEffect(() => {
    if (r.url || !r.path) return;
    let live = true;
    files.readPreview({ workspace: dir, path: r.path }).then(
      (p) => live && setURL(URL.createObjectURL(new Blob([p.data as BlobPart], { type: r.mime }))),
      () => {},
    );
    return () => {
      live = false;
    };
  }, [dir, r.path, r.url, r.mime]);
  return (
    <span className="pictures-ref" title={r.path ?? r.name}>
      {r.picture ? <Thumb dir={dir} session={session} picture={r.picture} size={64} /> : url ? <img className="pictures-thumb" src={url} alt={r.name} width={64} height={64} /> : <span className="pictures-thumb" />}
      <select className="pictures-ref-role" aria-label={t("desktop.pictures.role")} value={r.role} onChange={(e) => onRole(e.target.value as Role)}>
        {roles.map((v) => (
          <option key={v} value={v}>
            {t(`desktop.pictures.role.${v}`)}
          </option>
        ))}
      </select>
      <IconButton icon={mdiClose} small label={t("desktop.pictures.remove_ref", { name: r.name })} onClick={onRemove} />
    </span>
  );
}

/** One turn: what was asked, then what came (PIC-40). */
function Turn({
  dir,
  session,
  turn,
  onSave,
  onSaveAs,
  onReference,
  onStartFrom,
  onChat,
  onDelete,
  onCopy,
  onZoom,
}: {
  dir: string;
  session: string;
  turn: PictureTurn;
  onZoom: (url: string) => void;
  onSave: (id: string) => void;
  onSaveAs: (id: string) => void;
  onReference: (p: PictureFile) => void;
  onStartFrom: (id: string) => void;
  onChat: (p: PictureFile) => void;
  onDelete: (id: string) => void;
  onCopy: (p: PictureFile) => void;
}) {
  const [thoughts, setThoughts] = useState(false);
  const chips = [turn.model.split("/").pop(), turn.aspectRatio, turn.size, turn.count > 1 ? `×${turn.count}` : "", turn.thinking && t(`desktop.pictures.level.${turn.thinking}`), turn.search && t("desktop.pictures.searched")].filter(Boolean);
  return (
    <div className="pictures-turn stack">
      <div className="pictures-ask stack">
        <span className="t-body-md">{turn.prompt}</span>
        {turn.references.length > 0 && (
          <div className="row wrap" style={{ gap: 6 }}>
            {turn.references.map((r) => (
              <span key={r.file} className="pictures-ref static" title={`${t(`desktop.pictures.role.${r.role || "object"}`)} · ${r.path || r.name}`}>
                <Thumb dir={dir} session={session} picture={r.file} size={40} />
              </span>
            ))}
          </div>
        )}
        <span className="t-body-sm muted">{chips.join(" · ")}</span>
      </div>
      {turn.thoughts && (
        <button type="button" className="row t-body-sm muted pictures-thoughts-toggle" aria-expanded={thoughts} onClick={() => setThoughts((v) => !v)}>
          <Icon path={thoughts ? mdiChevronDown : mdiChevronRight} size="sm" /> {t("desktop.pictures.thoughts")}
        </button>
      )}
      {thoughts && <p className="t-body-sm muted pictures-thoughts">{turn.thoughts}</p>}
      {turn.text && <p className="t-body-md">{turn.text}</p>}
      {turn.problem && <div className="card error small-card t-body-sm">{turn.problem}</div>}
      <div className="pictures-results">
        {turn.pictures.map((p) => (
          <Result key={p.id} dir={dir} session={session} p={p} onZoom={onZoom} onSave={() => onSave(p.id)} onSaveAs={() => onSaveAs(p.id)} onReference={() => onReference(p)} onStartFrom={() => onStartFrom(p.id)} onChat={() => onChat(p)} onDelete={() => onDelete(p.id)} onCopy={() => onCopy(p)} gemini={turn.kind === "gemini"} />
        ))}
      </div>
      {turn.sources.length > 0 && (
        <div className="row wrap t-body-sm muted" style={{ gap: 8 }}>
          {t("desktop.pictures.sources")}
          {turn.sources.map((s) => (
            <a key={s.url} href={s.url} target="_blank" rel="noreferrer">
              {s.title || s.url}
            </a>
          ))}
        </div>
      )}
      {turn.costUsd > 0 && <span className="t-body-sm muted">{t("desktop.pictures.cost", { cost: cost(turn.costUsd) })}</span>}
    </div>
  );
}

/** One of a picture's actions: its label shows at once on hover, above it. */
function Action({ icon, label, onClick }: { icon: string; label: string; onClick: () => void }) {
  return (
    <span className="pictures-action" data-tip={label}>
      <IconButton icon={icon} small label={label} title="" onClick={onClick} />
    </span>
  );
}

/** A picture made, with its actions (PIC-53); a click shows it full size. */
function Result({ dir, session, p, gemini, onZoom, onSave, onSaveAs, onReference, onStartFrom, onChat, onDelete, onCopy }: { dir: string; session: string; p: PictureFile; gemini: boolean; onZoom: (url: string) => void; onSave: () => void; onSaveAs: () => void; onReference: () => void; onStartFrom: () => void; onChat: () => void; onDelete: () => void; onCopy: () => void }) {
  const url = usePicture(dir, session, p.id);
  return (
    <figure className="pictures-result">
      {url ? (
        <button type="button" className="pictures-zoom" title={t("desktop.pictures.zoom")} aria-label={t("desktop.pictures.zoom")} onClick={() => onZoom(url)}>
          <img src={url} alt={t("desktop.pictures.made_picture")} />
        </button>
      ) : (
        <span className="pictures-result-wait" />
      )}
      <figcaption className="stack">
        <div className="row pictures-actions">
          <Action icon={mdiContentSave} label={t("desktop.pictures.save")} onClick={onSave} />
          <Action icon={mdiContentSaveEditOutline} label={t("desktop.pictures.save_as")} onClick={onSaveAs} />
          {gemini && <Action icon={mdiSourceBranch} label={t("desktop.pictures.start_from")} onClick={onStartFrom} />}
          <Action icon={mdiRefresh} label={t("desktop.pictures.use_as_reference")} onClick={onReference} />
          <Action icon={mdiContentCopy} label={t("desktop.pictures.copy")} onClick={onCopy} />
          <Action icon={mdiAt} label={t("desktop.pictures.add_to_chat")} onClick={onChat} />
          <Action icon={mdiDeleteOutline} label={t("desktop.pictures.delete")} onClick={onDelete} />
        </div>
        {p.saved.map((s) => (
          <button key={s} type="button" className="t-body-sm pictures-saved" title={t("desktop.pictures.open_saved")} onClick={() => openFile({ dir, path: s })}>
            {t("desktop.pictures.saved_as", { path: s })}
          </button>
        ))}
      </figcaption>
    </figure>
  );
}

/** A picture full size over everything: Esc, ✕ or a click outside it closes it. */
function Viewer({ url, onClose }: { url: string; onClose: () => void }) {
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
      }
    };
    window.addEventListener("keydown", key, true);
    return () => window.removeEventListener("keydown", key, true);
  }, [onClose]);
  return createPortal(
    <div className="pictures-viewer" role="dialog" aria-modal="true" aria-label={t("desktop.pictures.made_picture")} onClick={onClose}>
      <img src={url} alt={t("desktop.pictures.made_picture")} onClick={(e) => e.stopPropagation()} />
      <IconButton icon={mdiClose} label={t("desktop.close")} className="pictures-viewer-close" onClick={onClose} />
    </div>,
    document.body,
  );
}

/** Pictures in the workspace to start from (PIC-23). */
function WorkspacePicker({ dir, onPick, onClose }: { dir: string; onPick: (path: string) => void; onClose: () => void }) {
  const [query, setQuery] = useState("");
  const [paths, setPaths] = useState<string[]>([]);
  useEffect(() => {
    const timer = setTimeout(() => {
      files.findFiles({ workspace: dir, query, limit: 40, extensions: ["png", "jpg", "jpeg", "webp"] }).then(
        (r) => setPaths(r.paths),
        () => setPaths([]),
      );
    }, 150);
    return () => clearTimeout(timer);
  }, [dir, query]);
  return (
    <Dialog title={t("desktop.pictures.from_workspace")} icon={mdiFolderOpenOutline} onClose={onClose} footer={<Button onClick={onClose}>{t("desktop.close")}</Button>}>
      <div className="stack" style={{ gap: 8 }}>
        <input className="input" autoFocus value={query} placeholder={t("desktop.pictures.find_placeholder")} aria-label={t("desktop.pictures.find_placeholder")} onChange={(e) => setQuery(e.target.value)} />
        <div className="list pictures-pick">
          {paths.length === 0 && <span className="t-body-sm muted">{t("desktop.pictures.none_found")}</span>}
          {paths.map((p) => (
            <button
              key={p}
              type="button"
              className="list-item"
              onClick={() => {
                onPick(p);
                onClose();
              }}
            >
              <Icon path={mdiImagePlusOutline} />
              <code className="ellipsis">{p}</code>
            </button>
          ))}
        </div>
      </div>
    </Dialog>
  );
}
