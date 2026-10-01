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
import { mdiRestore, mdiWeatherNight, mdiWhiteBalanceSunny } from "@mdi/js";
import { EditorView } from "@codemirror/view";
import { t } from "../i18n";
import { Markdown } from "../Markdown";
import { maxMarkdownCSS } from "../prefs";
import { useApp } from "../state";
import type { Theme } from "../theme";
import { Button, Segmented } from "../ui/controls";
import { fileState, languageSupport } from "./codemirror";
import { docCssTemplate, docCssToSave } from "./docCss";

const key = (theme: Theme) => (theme === "dark" ? "markdown_css_dark" : "markdown_css_light");

/**
 * Settings → Documents (spec_files_029 FIL-57): CSS for Markdown previews,
 * one stylesheet per theme over the defaults, and a sample that shows it
 * as it's typed.
 */
export function DocumentSettings() {
  const { prefs, update, theme: current } = useApp();
  const [theme, setTheme] = useState<Theme>(current);
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const prefsRef = useRef(prefs);
  prefsRef.current = prefs;

  const save = (th: Theme, text: string) => {
    const css = docCssToSave(th, text).slice(0, maxMarkdownCSS);
    if (prefsRef.current[key(th)] !== css) update((p) => ({ ...p, [key(th)]: css }));
  };

  // The theme's CSS in the editor (its template while it has none); edits
  // save after a pause, and when the theme or the dialog changes.
  const show = async (th: Theme, text: string) => {
    const lang = await languageSupport("theme.css");
    const state = fileState(
      text,
      lang,
      {
        save: () => {
          clearTimeout(timer.current);
          save(th, view.current?.state.doc.toString() ?? "");
        },
        changed: (s) => {
          clearTimeout(timer.current);
          timer.current = setTimeout(() => save(th, s.doc.toString()), 400);
        },
      },
      true,
    );
    if (!host.current) return;
    if (view.current) view.current.setState(state);
    else view.current = new EditorView({ parent: host.current, state });
  };

  useEffect(() => {
    void show(theme, prefsRef.current[key(theme)] || docCssTemplate(theme));
    return () => {
      // Leaving the theme (or the settings) keeps what was typed.
      if (timer.current !== undefined && view.current) {
        clearTimeout(timer.current);
        timer.current = undefined;
        save(theme, view.current.state.doc.toString());
      }
    };
    // show and save read refs; a new theme is a new document.
  }, [theme]);
  useEffect(() => () => view.current?.destroy(), []);

  const reset = () => {
    clearTimeout(timer.current);
    timer.current = undefined;
    update((p) => ({ ...p, [key(theme)]: "" }));
    void show(theme, docCssTemplate(theme));
  };

  return (
    <div className="stack doc-settings">
      <p className="t-body-sm muted">{t("desktop.settings.doc_css.detail")}</p>
      <div className="row doc-settings-bar">
        <Segmented<Theme>
          label={t("desktop.settings.doc_css.theme")}
          value={theme}
          onChange={setTheme}
          options={[
            { value: "light", label: t("desktop.settings.theme.light"), icon: mdiWhiteBalanceSunny },
            { value: "dark", label: t("desktop.settings.theme.dark"), icon: mdiWeatherNight },
          ]}
        />
        <span className="spacer" />
        <Button small variant="text" icon={mdiRestore} disabled={!prefs[key(theme)]} onClick={reset}>
          {t("desktop.settings.doc_css.reset")}
        </Button>
      </div>
      <div className="settings-editor doc-css-editor" ref={host} aria-label={t("desktop.settings.doc_css.editor", { theme: t(`desktop.settings.theme.${theme}`) })} />
      <span className="t-label muted">{theme === current ? t("desktop.settings.doc_css.sample") : t("desktop.settings.doc_css.sample_other", { theme: t(`desktop.settings.theme.${current}`) })}</span>
      <div className="doc-sample preview preview-markdown">
        <Markdown text={t("desktop.settings.doc_css.sample_text")} />
      </div>
    </div>
  );
}
