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
import { mdiDeleteOutline, mdiKeyVariant, mdiPencilOutline, mdiPlus } from "@mdi/js";
import { config } from "./api";
import { message } from "./errors";
import type { Secret } from "./gen/blitz/v1/config_pb";
import { t } from "./i18n";
import { Button, Dialog, Icon, IconButton, useSnackbar } from "./ui/controls";

/** Whether key can name a secret: letters, digits, _, . and -, up to 64 (secrets.ValidKey). */
export const validSecretKey = (key: string) => /^[A-Za-z0-9_.-]{1,64}$/.test(key);

/** How a secret is named in prompts, instructions and settings. */
export const secretPlaceholder = (key: string) => `{secrets:${key}}`;

/**
 * The user's secrets in a scope (spec_secrets_039): the global ones
 * (Settings › Secrets, workspace "") or a workspace's own (its settings).
 * Names are listed; a value is only ever written (added, or replaced),
 * kept in the system's keychain, and named as {secrets:key}.
 */
export function SecretsEditor({ workspace = "" }: { workspace?: string }) {
  const snack = useSnackbar();
  const [list, setList] = useState<Secret[]>([]);
  const [store, setStore] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [key, setKey] = useState("");
  const [value, setValue] = useState("");
  // The secret whose value is being replaced, and the one to delete.
  const [replacing, setReplacing] = useState("");
  const [deleting, setDeleting] = useState("");

  const load = useCallback(async () => {
    try {
      const r = await config.listSecrets({ workspace });
      setList(r.secrets);
      setStore(r.store);
      setError("");
    } catch (e) {
      setError(message(e));
    }
  }, [workspace]);
  useEffect(() => void load(), [load]);

  const act = async (run: () => Promise<string>) => {
    setBusy(true);
    try {
      snack(await run());
      await load();
      return true;
    } catch (e) {
      snack(message(e), { error: true });
      return false;
    } finally {
      setBusy(false);
    }
  };
  const set = (k: string, v: string) => act(async () => (await config.setSecret({ workspace, key: k, value: v }), t("desktop.secrets.saved", { key: k })));

  const keyProblem = key && !validSecretKey(key) ? t("desktop.secrets.bad_key") : "";
  return (
    <div className="stack secrets-editor" style={{ gap: 12 }}>
      <p className="t-body-sm muted">
        {t(workspace ? "desktop.secrets.intro_workspace" : "desktop.secrets.intro", { example: secretPlaceholder("github_token") })}
        {store && ` ${t("desktop.secrets.store", { store })}`}
      </p>
      {error && <p className="error-text">{error}</p>}
      {list.length === 0 ? (
        <p className="t-body-sm muted">{t("desktop.secrets.none")}</p>
      ) : (
        <div className="list">
          {list.map((s) => (
            <div key={s.key} className="rule">
              <Icon path={mdiKeyVariant} className="muted" />
              <div className="stack spacer" style={{ gap: 2, minWidth: 0 }}>
                <code>{secretPlaceholder(s.key)}</code>
                {s.updated && <span className="t-body-sm muted">{t("desktop.secrets.updated", { when: new Date(Number(s.updated.seconds) * 1000).toLocaleString() })}</span>}
              </div>
              <IconButton icon={mdiPencilOutline} label={t("desktop.secrets.replace", { key: s.key })} small disabled={busy} onClick={() => setReplacing(s.key)} />
              <IconButton icon={mdiDeleteOutline} label={t("desktop.secrets.delete_named", { key: s.key })} small disabled={busy} onClick={() => setDeleting(s.key)} />
            </div>
          ))}
        </div>
      )}
      <form
        className="row secrets-add"
        style={{ gap: 8, alignItems: "flex-end" }}
        onSubmit={(e) => {
          e.preventDefault();
          if (!key || !value || keyProblem) return;
          void set(key, value).then((ok) => ok && (setKey(""), setValue("")));
        }}
      >
        <label className="field" style={{ flex: 1 }}>
          <span className="t-label">{t("desktop.secrets.key")}</span>
          <input className={`input mono ${keyProblem ? "invalid" : ""}`} value={key} placeholder={t("desktop.secrets.key_placeholder")} spellCheck={false} autoComplete="off" disabled={busy} onChange={(e) => setKey(e.target.value.trim())} />
        </label>
        <label className="field" style={{ flex: 2 }}>
          <span className="t-label">{t("desktop.secrets.value")}</span>
          <input className="input mono" type="password" value={value} autoComplete="new-password" disabled={busy} onChange={(e) => setValue(e.target.value)} />
        </label>
        <Button variant="tonal" icon={mdiPlus} type="submit" disabled={busy || !key || !value || !!keyProblem}>
          {t("desktop.secrets.add")}
        </Button>
      </form>
      {keyProblem && <p className="t-body-sm error-text">{keyProblem}</p>}
      {replacing && <ReplaceDialog name={replacing} busy={busy} onClose={() => setReplacing("")} onSave={(v) => void set(replacing, v).then((ok) => ok && setReplacing(""))} />}
      {deleting && (
        <Dialog
          title={t("desktop.secrets.delete_title", { key: deleting })}
          icon={mdiDeleteOutline}
          onClose={() => setDeleting("")}
          footer={
            <>
              <Button onClick={() => setDeleting("")}>{t("desktop.cancel")}</Button>
              <Button
                variant="filled"
                danger
                onClick={() => {
                  const k = deleting;
                  setDeleting("");
                  void act(async () => (await config.deleteSecret({ workspace, key: k }), t("desktop.secrets.deleted", { key: k })));
                }}
              >
                {t("desktop.secrets.delete")}
              </Button>
            </>
          }
        >
          <p>{t("desktop.secrets.delete_body", { placeholder: secretPlaceholder(deleting) })}</p>
        </Dialog>
      )}
    </div>
  );
}

// A new value for a secret, never showing the old one.
function ReplaceDialog({ name, busy, onClose, onSave }: { name: string; busy: boolean; onClose: () => void; onSave: (value: string) => void }) {
  const [v, setV] = useState("");
  return (
    <Dialog
      title={t("desktop.secrets.replace", { key: name })}
      icon={mdiKeyVariant}
      onClose={onClose}
      footer={
        <>
          <Button onClick={onClose}>{t("desktop.cancel")}</Button>
          <Button variant="filled" disabled={busy || !v} onClick={() => onSave(v)}>
            {t("desktop.secrets.save")}
          </Button>
        </>
      }
    >
      <label className="field">
        <span className="t-label">{t("desktop.secrets.new_value")}</span>
        <input className="input mono" type="password" autoFocus value={v} autoComplete="new-password" onChange={(e) => setV(e.target.value)} onKeyDown={(e) => e.key === "Enter" && v && onSave(v)} />
      </label>
    </Dialog>
  );
}
