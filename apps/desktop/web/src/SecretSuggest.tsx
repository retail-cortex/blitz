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

import { useEffect, useState, type KeyboardEvent, type RefObject, type TextareaHTMLAttributes } from "react";
import { mdiKeyVariant } from "@mdi/js";
import { t } from "./i18n";
import { insertSecret, missingSecrets, secretNames, secretOptions, secretTokenAt, type SecretName, type SecretOption } from "./secretComplete";
import { Icon } from "./ui/controls";

/** The names a workspace (or "") can name, loaded once a field shows. */
export function useSecretNames(workspace: string): SecretName[] {
  const [names, setNames] = useState<SecretName[]>([]);
  useEffect(() => {
    let live = true;
    void secretNames(workspace).then((n) => live && setNames(n));
    return () => {
      live = false;
    };
  }, [workspace]);
  return names;
}

/**
 * Completing {secrets:…} in a text box: the menu, its keys (onKeyDown
 * returns whether it took the key), and the caret to follow. The composer
 * and SuggestTextarea use it.
 */
export function useSecretSuggest(text: string, setText: (v: string) => void, ref: RefObject<HTMLTextAreaElement | HTMLInputElement | null>, workspace: string) {
  const names = useSecretNames(workspace);
  const [caret, setCaret] = useState(0);
  const [pick, setPick] = useState(0);
  const [dismissed, setDismissed] = useState("");
  const token = secretTokenAt(text, caret);
  const options: SecretOption[] = token ? secretOptions(token.query, names, !!workspace, { global: t("desktop.secrets.scope.global"), workspace: t("desktop.secrets.scope.workspace") }) : [];
  const open = options.length > 0 && dismissed !== text;
  useEffect(() => setPick(0), [token?.query]);
  const choose = (o: SecretOption) => {
    if (!token) return;
    const r = insertSecret(text, token, o.insert);
    setText(r.text);
    setCaret(r.caret);
    requestAnimationFrame(() => ref.current?.setSelectionRange(r.caret, r.caret));
  };
  const onKeyDown = (e: KeyboardEvent): boolean => {
    if (!open || e.nativeEvent.isComposing) return false;
    const n = options.length;
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      setPick((p) => (p + (e.key === "ArrowDown" ? 1 : n - 1)) % n);
      return true;
    }
    if (e.key === "Escape") {
      e.preventDefault();
      setDismissed(text);
      return true;
    }
    if (e.key === "Tab" || (e.key === "Enter" && !e.shiftKey)) {
      e.preventDefault();
      choose(options[Math.min(pick, n - 1)]);
      return true;
    }
    return false;
  };
  const trackCaret = (e: { currentTarget: HTMLTextAreaElement | HTMLInputElement }) => setCaret(e.currentTarget.selectionStart ?? 0);
  const menu = open ? (
    <div className="command-menu" role="listbox" aria-label={t("desktop.secrets.suggest")}>
      {options.map((o, i) => (
        <button
          key={o.label}
          type="button"
          role="option"
          aria-selected={i === pick}
          className={`command-option ${i === pick ? "on" : ""}`}
          onMouseDown={(e) => {
            e.preventDefault(); // keep the focus in the field
            choose(o);
          }}
        >
          <Icon path={mdiKeyVariant} size="sm" />
          <code>{o.label}</code>
          <span className="spacer ellipsis muted t-body-sm">{o.detail}</span>
        </button>
      ))}
      <div className="command-hint t-body-sm muted">
        <kbd>↑</kbd>
        <kbd>↓</kbd> {t("desktop.keys.choose")} · <kbd>{t("desktop.keys.tab")}</kbd> {t("desktop.keys.complete")} · <kbd>{t("desktop.keys.esc")}</kbd> {t("desktop.keys.close")}
      </div>
    </div>
  ) : null;
  return { menu, open, onKeyDown, trackCaret, names };
}

/**
 * A text box (instructions, a rule, an MCP server's environment) that
 * completes {secrets:…} and warns of names that aren't there. workspace
 * is whose secrets it can name ("" the global ones only).
 */
export function SuggestTextarea({
  value,
  onValue,
  workspace,
  ...rest
}: Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, "value" | "onChange"> & { value: string; onValue: (v: string) => void; workspace: string }) {
  const [el, setEl] = useState<HTMLTextAreaElement | null>(null);
  const ref = { current: el };
  const s = useSecretSuggest(value, onValue, ref, workspace);
  const missing = missingSecrets(value, s.names);
  return (
    <div className="suggest-field">
      <textarea
        {...rest}
        ref={setEl}
        value={value}
        onChange={(e) => {
          onValue(e.target.value);
          s.trackCaret(e);
        }}
        onSelect={s.trackCaret}
        onKeyDown={(e) => {
          if (s.onKeyDown(e)) return;
          rest.onKeyDown?.(e);
        }}
      />
      {s.menu}
      {missing.length > 0 && <span className="t-body-sm warning-text">{t("desktop.secrets.missing", { names: missing.join(", ") })}</span>}
    </div>
  );
}
