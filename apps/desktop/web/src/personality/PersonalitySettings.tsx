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

import { useCallback, useEffect, useState } from "react";
import { mdiCheckCircleOutline, mdiContentCopy, mdiDeleteOutline, mdiEmoticonOutline, mdiPencilOutline, mdiPlus, mdiTestTube } from "@mdi/js";
import { config, workspaces } from "../api";
import { message } from "../errors";
import { configChanged } from "../events";
import type { Personality } from "../gen/blitz/v1/config_pb";
import { t } from "../i18n";
import { useApp } from "../state";
import { Button, Chip, Dialog, Icon, IconButton, useSnackbar } from "@blitz/webui";
import { PersonalityEditor, type Draft } from "./PersonalityEditor";
import { copyName, personalityDescription, personalityLabel, personalitySource } from "./personality";

/** What Try it answered, and for which personality. */
type Tried = { name: string; question: string; answer: string };

/**
 * Settings › Personality (spec_064_personality PER-30): every
 * personality, built-ins first, then the organization's, then the
 * person's; choosing one makes it the person's default ([ui]
 * personality). Each can be tried (a short sample answer) and duplicated;
 * the person's own edited and deleted. voiceOn shows each one's voice
 * (spec_063_voice).
 */
export function PersonalitySettings({ voiceOn = false }: { voiceOn?: boolean }) {
  const snack = useSnackbar();
  const { prefs } = useApp();
  const workspace = prefs.workspaces.find((w) => w.dir === prefs.active && w.open)?.dir ?? "";
  const [list, setList] = useState<Personality[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState<{ draft: Draft; original: string } | null>(null);
  const [deleting, setDeleting] = useState("");
  const [tried, setTried] = useState<Tried | null>(null);
  const [trying, setTrying] = useState("");
  const [triedOnce, setTriedOnce] = useState(false);
  const load = useCallback(async () => {
    try {
      const r = await config.listPersonalities({ workspace });
      setList(r.personalities);
      setError("");
    } catch (e) {
      setError(message(e));
    }
  }, [workspace]);
  useEffect(() => void load(), [load]);
  const act = async (run: () => Promise<unknown>, done: string) => {
    setBusy(true);
    try {
      await run();
      snack(done);
      configChanged({ dir: "" });
      await load();
      return true;
    } catch (e) {
      snack(message(e), { error: true });
      return false;
    } finally {
      setBusy(false);
    }
  };
  const own = list.filter((p) => p.source === "own").map((p) => p.name);
  const choose = (p: Personality) => void act(() => config.setConfigValue({ workspace: "", key: "ui.personality", value: p.name === "default" ? "" : p.name }), t("desktop.personality.chosen", { name: personalityLabel(p) }));
  const open = async (p: Personality, copy: boolean) => {
    try {
      const full = (await config.getPersonality({ name: p.name })).personality;
      const draft: Draft = { name: copy ? copyName(p.name, list.map((x) => x.name)) : p.name, description: copy ? personalityDescription(p) : (full?.description ?? ""), voice: full?.voice ?? "", text: full?.text ?? "" };
      setEditing({ draft, original: copy ? "" : p.name });
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  const tryOne = async (p: Personality) => {
    setTrying(p.name);
    try {
      const text = p.name === "default" ? "" : ((await config.getPersonality({ name: p.name })).personality?.text ?? "");
      const r = await workspaces.tryPersonality({ workspace, text });
      setTried({ name: p.name, question: r.question, answer: r.answer });
      setTriedOnce(true);
    } catch (e) {
      snack(message(e), { error: true });
    } finally {
      setTrying("");
    }
  };
  return (
    <div className="stack personality-settings" style={{ gap: 12 }}>
      <p className="t-body-sm muted">{t("desktop.personality.intro")}</p>
      {error && <p className="error-text">{error}</p>}
      <div className="list provider-list" role="radiogroup" aria-label={t("desktop.settings.personality")}>
        {list.map((p) => (
          <div key={p.name} className={`list-item provider-row ${p.personDefault ? "active" : ""}`}>
            <button type="button" role="radio" aria-checked={p.personDefault} className="row-open" disabled={busy} onClick={() => !p.personDefault && choose(p)}>
              <Icon path={mdiEmoticonOutline} />
              <span className="lines spacer">
                <span className="ellipsis">
                  {personalityLabel(p)}
                  {voiceOn && p.voice && <small className="muted"> · {t("desktop.personality.voice_named", { voice: p.voice })}</small>}
                </span>
                <small className="ellipsis">{personalityDescription(p)}</small>
              </span>
            </button>
            <span className="trailing">
              <Chip className="static">{personalitySource(p)}</Chip>
              {p.personDefault && (
                <Chip className="static" selected icon={mdiCheckCircleOutline}>
                  {t("desktop.personality.default_chip")}
                </Chip>
              )}
              <IconButton icon={mdiTestTube} label={t("desktop.personality.try_named", { name: personalityLabel(p) })} small disabled={!workspace || !!trying} onClick={() => void tryOne(p)} />
              {p.name !== "default" && <IconButton icon={mdiContentCopy} label={t("desktop.personality.duplicate_named", { name: personalityLabel(p) })} small disabled={busy} onClick={() => void open(p, true)} />}
              {p.source === "own" && (
                <>
                  <IconButton icon={mdiPencilOutline} label={t("desktop.personality.edit_named", { name: p.name })} small disabled={busy} onClick={() => void open(p, false)} />
                  <IconButton icon={mdiDeleteOutline} label={t("desktop.personality.delete_named", { name: p.name })} small disabled={busy} onClick={() => setDeleting(p.name)} />
                </>
              )}
            </span>
          </div>
        ))}
      </div>
      {!workspace && <p className="t-body-sm muted">{t("desktop.personality.try_needs_workspace")}</p>}
      {(trying || tried) && (
        <section className="card outlined stack personality-try" aria-live="polite" style={{ gap: 6 }}>
          {trying ? (
            <span className="t-body-sm muted">{t("desktop.personality.trying")}</span>
          ) : (
            tried && (
              <>
                <span className="t-label">{t("desktop.personality.tried", { name: personalityLabel(list.find((p) => p.name === tried.name) ?? { name: tried.name, description: "", voice: "", source: "own" }) })}</span>
                <span className="t-body-sm muted">{tried.question}</span>
                <p className="t-body">{tried.answer}</p>
              </>
            )
          )}
          {!triedOnce && <span className="t-body-sm muted">{t("desktop.personality.try_costs")}</span>}
        </section>
      )}
      <div className="row">
        <Button variant="tonal" icon={mdiPlus} disabled={busy} onClick={() => setEditing({ draft: { name: "", description: "", voice: "", text: "" }, original: "" })}>
          {t("desktop.personality.new")}
        </Button>
      </div>
      {editing && (
        <PersonalityEditor
          draft={editing.draft}
          original={editing.original}
          own={own}
          workspace={workspace}
          voiceOn={voiceOn}
          onClose={() => setEditing(null)}
          onSave={(d) =>
            act(() => config.savePersonality({ personality: { name: d.name.trim(), description: d.description.trim(), voice: d.voice, text: d.text } }), t("desktop.personality.saved", { name: d.name.trim() })).then((ok) => {
              if (ok) setEditing(null);
              return ok;
            })
          }
        />
      )}
      {deleting && (
        <Dialog
          title={t("desktop.personality.delete_title", { name: deleting })}
          icon={mdiDeleteOutline}
          onClose={() => setDeleting("")}
          footer={
            <>
              <Button onClick={() => setDeleting("")}>{t("desktop.cancel")}</Button>
              <Button variant="filled" danger disabled={busy} onClick={() => void act(() => config.deletePersonality({ name: deleting }), t("desktop.personality.deleted", { name: deleting })).then(() => setDeleting(""))}>
                {t("desktop.personality.delete")}
              </Button>
            </>
          }
        >
          <p>{t("desktop.personality.delete_body", { name: deleting })}</p>
        </Dialog>
      )}
    </div>
  );
}
