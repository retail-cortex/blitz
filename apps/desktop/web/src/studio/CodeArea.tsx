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

import { useEffect, useRef } from "react";
import { EditorView } from "@codemirror/view";
import { fileState, languageSupport } from "../files/codemirror";
import { secretCompletion } from "../files/secretCompletion";

/**
 * A file's text in a small editor (a skill's reference or script): its
 * language's highlighting from path, line numbers, {secrets:} completion
 * for the workspace, ⌘S (onSave). A new path or revision starts it over
 * with value; edits go to onChange.
 */
export function CodeArea({ path, value, revision, workspace, onChange, onSave, label }: { path: string; value: string; revision: number; workspace: string; onChange: (text: string) => void; onSave: () => void; label: string }) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView | null>(null);
  const live = useRef({ onChange, onSave });
  live.current = { onChange, onSave };

  useEffect(() => {
    let gone = false;
    void languageSupport(path).then((lang) => {
      if (gone || !host.current) return;
      const state = fileState(value, lang, { save: () => live.current.onSave(), changed: (s) => live.current.onChange(s.doc.toString()) }, true, [secretCompletion(workspace), EditorView.contentAttributes.of({ "aria-label": label })]);
      if (view.current) view.current.setState(state);
      else view.current = new EditorView({ parent: host.current, state });
    });
    return () => {
      gone = true;
    };
    // value is read when the file (path, revision) changes, not as it's typed.
  }, [path, revision, workspace, label]);
  useEffect(() => () => view.current?.destroy(), []);

  return <div ref={host} className="code-area" />;
}
