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

import { useState } from "react";
import { mdiEmoticonOutline, mdiTestTube } from "@mdi/js";
import { workspaces } from "../api";
import { message } from "../errors";
import { t } from "../i18n";
import { MarkdownField } from "../studio/MarkdownField";
import { Button, Dialog } from "@blitz/webui";
import { maxPersonalityText, personalityProblem } from "./personality";
import { geminiVoices } from "./voices";

/** A personality as the editor has it. */
export type Draft = { name: string; description: string; voice: string; text: string };

const same = (a: Draft, b: Draft) => a.name === b.name && a.description === b.description && a.voice === b.voice && a.text === b.text;

/**
 * The personality editor (PER-31): its name, description and text (a
 * Markdown field, with the count), its voice when voice is on, Try it on
 * the text as it stands, and Save, which says why it can't before it's
 * pressed. original is the name of the person's own being edited ("" for
 * a new one); closing with changes asks first.
 */
export function PersonalityEditor({
  draft: start,
  original,
  own,
  workspace,
  voiceOn,
  onClose,
  onSave,
}: {
  draft: Draft;
  original: string;
  own: readonly string[];
  workspace: string;
  voiceOn: boolean;
  onClose: () => void;
  onSave: (d: Draft) => Promise<boolean>;
}) {
  const [d, setD] = useState(start);
  const [asking, setAsking] = useState(false);
  const [busy, setBusy] = useState(false);
  const [sample, setSample] = useState("");
  const [sampleError, setSampleError] = useState("");
  const problem = personalityProblem(d, own, original);
  const count = [...d.text].length;
  const close = () => (same(d, start) ? onClose() : setAsking(true));
  const tryIt = async () => {
    setBusy(true);
    setSampleError("");
    try {
      setSample((await workspaces.tryPersonality({ workspace, text: d.text })).answer);
    } catch (e) {
      setSampleError(message(e));
    } finally {
      setBusy(false);
    }
  };
  const save = async () => {
    setBusy(true);
    await onSave(d);
    setBusy(false);
  };
  return (
    <Dialog
      title={original ? t("desktop.personality.edit_title", { name: original }) : t("desktop.personality.new_title")}
      icon={mdiEmoticonOutline}
      large
      onClose={close}
      footer={
        <>
          <Button onClick={close}>{t("desktop.cancel")}</Button>
          <Button variant="filled" disabled={busy || !!problem} onClick={() => void save()}>
            {t("desktop.personality.save")}
          </Button>
        </>
      }
    >
      <div className="stack personality-editor" style={{ gap: 12 }}>
        <label className="field">
          <span className="t-label">{t("desktop.personality.name")}</span>
          <input className="input mono" value={d.name} disabled={!!original || busy} spellCheck={false} placeholder={t("desktop.personality.name_placeholder")} onChange={(e) => setD({ ...d, name: e.target.value.trim().toLowerCase() })} />
          <span className="t-body-sm muted">{t("desktop.personality.name_hint")}</span>
        </label>
        <label className="field">
          <span className="t-label">{t("desktop.personality.description")}</span>
          <input className="input" value={d.description} disabled={busy} placeholder={t("desktop.personality.description_placeholder")} onChange={(e) => setD({ ...d, description: e.target.value })} />
        </label>
        <MarkdownField
          label={t("desktop.personality.text")}
          supporting={<span className={count > maxPersonalityText ? "error-text" : ""}>{t("desktop.personality.count", { count, max: maxPersonalityText })}</span>}
          value={d.text}
          onValue={(text) => setD({ ...d, text })}
          dir={workspace}
          path="personality.md"
          workspace={workspace}
          placeholder={t("desktop.personality.text_placeholder")}
          rows={8}
          disabled={busy}
          tools={
            <Button small icon={mdiTestTube} disabled={busy || !workspace || !d.text.trim()} onClick={() => void tryIt()}>
              {t("desktop.personality.try")}
            </Button>
          }
        />
        {(sample || sampleError) && (
          <section className="card outlined stack personality-try" aria-live="polite" style={{ gap: 6 }}>
            {sampleError ? <p className="error-text">{sampleError}</p> : <p className="t-body">{sample}</p>}
          </section>
        )}
        {voiceOn && (
          <label className="field">
            <span className="t-label">{t("desktop.personality.voice")}</span>
            <select className="select" value={d.voice} disabled={busy} onChange={(e) => setD({ ...d, voice: e.target.value })}>
              <option value="">{t("desktop.personality.voice_setting")}</option>
              {geminiVoices.map((v) => (
                <option key={v} value={v}>
                  {v}
                </option>
              ))}
            </select>
            <span className="t-body-sm muted">{t("desktop.personality.voice_hint")}</span>
          </label>
        )}
        {problem && (d.name || d.text) && <p className="t-body-sm error-text">{problem}</p>}
      </div>
      {asking && (
        <Dialog
          title={t("desktop.personality.discard_title")}
          onClose={() => setAsking(false)}
          footer={
            <>
              <Button onClick={() => setAsking(false)}>{t("desktop.personality.keep_editing")}</Button>
              <Button variant="filled" danger onClick={onClose}>
                {t("desktop.personality.discard")}
              </Button>
            </>
          }
        >
          <p>{t("desktop.personality.discard_body")}</p>
        </Dialog>
      )}
    </Dialog>
  );
}
