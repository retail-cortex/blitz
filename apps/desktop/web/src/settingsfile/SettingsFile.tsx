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

import { useEffect, useMemo, useRef, useState } from "react";
import { mdiAlertCircleOutline, mdiCheckCircleOutline, mdiCloseCircleOutline } from "@mdi/js";
import type { EditorState } from "@codemirror/state";
import { EditorView, placeholder } from "@codemirror/view";
import { config } from "../api";
import { message } from "../errors";
import { configChanged } from "../events";
import { fileState, goToLine, languageSupport } from "../files/codemirror";
import { t } from "../i18n";
import { Button, Icon, useSnackbar } from "../ui/controls";
import { settingsExtensions, setProblems, type LineProblem } from "./editor";
import { searchSettings, type SettingRef } from "./toml";

/**
 * A settings file, for what the forms don't cover: TOML with highlighting,
 * checked with Validate (and before saving) and its problems marked on
 * their lines; hovering a setting shows the reference, which the panel
 * below lists and searches.
 */
export function SettingsFile({ scopes }: { scopes: { dir: string; name: string }[] }) {
  const snack = useSnackbar();
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView | null>(null);
  const reference = useRef<SettingRef[]>([]);
  const [ref, setRef] = useState<SettingRef[]>([]);
  const [workspace, setWorkspace] = useState("");
  const [path, setPath] = useState("");
  const [text, setText] = useState("");
  const [saved, setSaved] = useState("");
  // Problems from the last check; checked is the text they're about.
  const [problems, setProblemList] = useState<LineProblem[]>();
  const [checked, setChecked] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [query, setQuery] = useState("");
  const saveRef = useRef<() => void>(() => {});

  useEffect(() => {
    config.getSettingsReference({}).then(
      (r) => {
        reference.current = r.settings;
        setRef(r.settings);
      },
      () => {}, // the editor works without it
    );
  }, []);

  // A fresh editor for the scope's file.
  const show = async (doc: string) => {
    const lang = await languageSupport("settings.toml");
    const labels = { type: t("desktop.file.type"), default: t("desktop.file.default") };
    const state = fileState(doc, lang, { save: () => saveRef.current(), changed: (s: EditorState) => setText(s.doc.toString()) }, true, [
      settingsExtensions(() => reference.current, labels),
      placeholder(t("desktop.file.empty")),
    ]);
    if (!view.current) view.current = new EditorView({ parent: host.current!, state });
    else view.current.setState(state);
    setText(doc);
    mark([]);
  };
  useEffect(() => () => view.current?.destroy(), []);

  useEffect(() => {
    let live = true;
    setError("");
    config.getConfigFile({ workspace }).then(
      (f) => {
        if (!live) return;
        setPath(f.path);
        setSaved(f.text);
        show(f.text);
      },
      (e) => live && setError(message(e)),
    );
    return () => {
      live = false;
    };
    // show only reads refs and setters.
  }, [workspace]);

  const mark = (ps: LineProblem[] | undefined, about = "") => {
    setProblemList(ps);
    setChecked(about);
    view.current?.dispatch({ effects: setProblems.of(ps ?? []) });
  };

  /** Checks the text; resolves to whether it may be saved. */
  const validate = async (quiet = false): Promise<boolean> => {
    setError("");
    try {
      const res = await config.checkConfigFile({ text });
      const ps = res.problems.map((p) => ({ line: p.line, error: p.error, message: p.message }));
      mark(ps, text);
      if (!quiet && ps.length === 0) snack(t("desktop.file.valid"));
      const first = ps.find((p) => p.error && p.line > 0);
      if (first && view.current) goToLine(view.current, first.line);
      return !ps.some((p) => p.error);
    } catch (e) {
      setError(message(e));
      return false;
    }
  };

  const save = async () => {
    if (busy || text === saved) return;
    setBusy(true);
    try {
      if (!(await validate(true))) return;
      const res = await config.saveConfigFile({ workspace, text });
      setSaved(text);
      if (res.change?.modelError) snack(t("desktop.keys.model_error", { reason: res.change.modelError }), { error: true });
      else snack(t("desktop.file.saved"));
      configChanged({ dir: workspace });
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  };
  saveRef.current = save;

  const dirty = text !== saved;
  const stale = problems !== undefined && checked !== text;
  const found = useMemo(() => searchSettings(ref, query), [ref, query]);
  return (
    <div className="stack settings-file" style={{ gap: 12 }}>
      <div className="row" style={{ gap: 12 }}>
        <label className="field" style={{ flex: 1 }}>
          <span className="t-label">{t("desktop.file.scope")}</span>
          <select className="select" value={workspace} onChange={(e) => setWorkspace(e.target.value)} disabled={dirty}>
            <option value="">{t("desktop.file.global")}</option>
            {scopes.map((s) => (
              <option key={s.dir} value={s.dir}>
                {s.name}
              </option>
            ))}
          </select>
        </label>
      </div>
      <code className="t-body-sm muted ellipsis">{path}</code>
      {error && <p className="error-text">{error}</p>}
      <div className="settings-editor" ref={host} aria-label={t("desktop.file.text")} />
      {problems !== undefined &&
        (problems.length === 0 ? (
          <p className="row t-body-sm" style={{ gap: 6 }}>
            <Icon path={mdiCheckCircleOutline} size="sm" />
            <span>{t(stale ? "desktop.file.valid_stale" : "desktop.file.valid")}</span>
          </p>
        ) : (
          <div className="stack settings-problems" style={{ gap: 4 }}>
            {stale && <span className="t-body-sm muted">{t("desktop.file.stale")}</span>}
            {problems.map((p, i) => (
              <button key={i} className={`problem row t-body-sm ${p.error ? "error" : "warn"}`} disabled={!p.line} onClick={() => view.current && goToLine(view.current, p.line)}>
                <Icon path={p.error ? mdiCloseCircleOutline : mdiAlertCircleOutline} size="sm" />
                {p.line > 0 && <span className="mono">{t("desktop.file.line", { line: p.line })}</span>}
                <span>{p.message}</span>
              </button>
            ))}
          </div>
        ))}
      <p className="t-body-sm muted">{t("desktop.file.hint")}</p>
      <div className="row" style={{ justifyContent: "flex-end", gap: 8 }}>
        <Button small disabled={busy} onClick={() => validate()}>
          {t("desktop.file.validate")}
        </Button>
        <Button small disabled={!dirty || busy} onClick={() => show(saved)}>
          {t("desktop.file.revert")}
        </Button>
        <Button small variant="filled" disabled={!dirty || busy} onClick={save}>
          {t("desktop.keys.save")}
        </Button>
      </div>
      {ref.length > 0 && (
        <details className="settings-reference">
          <summary className="t-title-sm">{t("desktop.file.reference", { count: ref.length })}</summary>
          <input className="input" type="search" value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t("desktop.file.search")} aria-label={t("desktop.file.search")} />
          <dl>
            {found.map((s) => (
              <div key={s.key} className="ref-setting">
                <dt>
                  <code>{s.key}</code> <span className="muted t-body-sm">{s.type}{s.default && ` · ${t("desktop.file.default")}: ${s.default}`}</span>
                </dt>
                <dd className="t-body-sm">{s.doc}</dd>
              </div>
            ))}
            {found.length === 0 && <p className="muted t-body-sm">{t("desktop.file.no_match")}</p>}
          </dl>
        </details>
      )}
    </div>
  );
}
