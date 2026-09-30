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
import { useOnConfigChanged } from "./workspaceSettings";
import { mdiDeleteOutline, mdiPlus } from "@mdi/js";
import { config, workspaces } from "./api";
import { message } from "./errors";
import { configChanged } from "./events";
import type { CheckPermissionResponse, DescribePermissionsResponse, PermissionEntry } from "./gen/blitz/v1/config_pb";
import type { PermissionRule } from "./gen/blitz/v1/workspace_pb";
import { t } from "./i18n";
import { Button, IconButton, Switch, useSnackbar } from "./ui/controls";

const effects = ["allow", "ask", "deny"];

/** Says in words what a checked rule matches. */
function describe(c: CheckPermissionResponse): string {
  const pattern = c.form === "regex" ? c.pattern.replace(/^re:/, "") : c.pattern;
  return t(`desktop.perm.form.${c.form || "name"}`, { pattern, kind: c.kind });
}

/**
 * A scope's permission rules, kept in its settings file: the global ones
 * (workspace "") or a workspace's own, which add to them. A rule is checked
 * as it's typed, before it can be saved; the built-in read-only rules can
 * be turned off. In a workspace, rules added for the session only (with
 * /permissions) are listed too.
 */
export function PermissionSettings({ workspace, compact, onChanged }: { workspace: string; compact?: boolean; onChanged?: () => void }) {
  const snack = useSnackbar();
  const [desc, setDesc] = useState<DescribePermissionsResponse>();
  const [session, setSession] = useState<PermissionRule[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [effect, setEffect] = useState("allow");
  const [rule, setRule] = useState("");
  const [sample, setSample] = useState("");
  const [check, setCheck] = useState<CheckPermissionResponse>();

  const load = useCallback(async () => {
    try {
      setDesc(await config.describePermissions({ workspace }));
      if (workspace) {
        const all = (await workspaces.listPermissionRules({ workspace })).rules;
        setSession(all.filter((r) => r.source === "session" || r.source === "flag"));
      }
      setError("");
    } catch (e) {
      setError(message(e));
    }
  }, [workspace]);
  useEffect(() => {
    load();
  }, [load]);
  useOnConfigChanged(workspace, load);

  // The rule is checked as it's typed (and tried on the sample).
  useEffect(() => {
    if (!rule.trim()) return setCheck(undefined);
    let live = true;
    const timer = setTimeout(() => {
      config.checkPermission({ effect, rule, sample }).then(
        (c) => live && setCheck(c),
        () => live && setCheck(undefined),
      );
    }, 250);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [effect, rule, sample]);

  const act = async (f: () => Promise<unknown>, done: string) => {
    setBusy(true);
    setError("");
    try {
      await f();
      snack(done);
      configChanged({ dir: workspace });
      onChanged?.();
      await load();
      return true;
    } catch (e) {
      setError(message(e));
      return false;
    } finally {
      setBusy(false);
    }
  };
  const add = async () => {
    if (!check || check.error || busy) return;
    if (await act(() => config.addPermission({ workspace, effect, rule }), t("desktop.perm.added", { rule: check.rule }))) {
      setRule("");
      setSample("");
    }
  };

  if (!desc) return <p className="muted">{error || t("desktop.checking")}</p>;
  const list = (rules: PermissionEntry[], removable: boolean) => (
    <div className="list">
      {rules.map((r) => (
        <div key={`${r.effect}:${r.rule}`} className="rule">
          <span className={`chip static effect-${r.effect}`}>{t(`desktop.rs.effect.${r.effect}`)}</span>
          <code className="ellipsis" title={r.rule}>
            {r.rule}
          </code>
          {removable && (
            <IconButton icon={mdiDeleteOutline} label={t("desktop.rs.remove")} small disabled={busy} onClick={() => act(() => config.removePermission({ workspace, rule: r.rule }), t("desktop.perm.removed", { rule: r.rule }))} />
          )}
        </div>
      ))}
    </div>
  );
  const defaultsState = desc.readOnlyDefaultsOn ? t("desktop.perm.on") : t("desktop.perm.off");
  return (
    <div className="stack permission-settings" style={{ gap: 12 }}>
      <p className="t-body-sm muted">{t(workspace ? "desktop.perm.intro_workspace" : "desktop.perm.intro")}</p>

      {desc.rules.length ? list(desc.rules, true) : <p className="t-body-sm muted">{t("desktop.perm.none")}</p>}

      <section className={`card rule-add ${compact ? "compact" : ""}`}>
        <div className="rule-add-row">
          <select className="select rule-effect" value={effect} disabled={busy} onChange={(e) => setEffect(e.target.value)} aria-label={t("desktop.rs.effect")}>
            {effects.map((e) => (
              <option key={e} value={e}>
                {t(`desktop.rs.effect.${e}`)}
              </option>
            ))}
          </select>
          <input
            className="input mono"
            value={rule}
            disabled={busy}
            onChange={(e) => setRule(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && add()}
            placeholder="shell(npm test)"
            aria-label={t("desktop.rs.rule")}
            spellCheck={false}
          />
        </div>
        <label className="field">
          <span className="t-label">{t("desktop.perm.try")}</span>
          <input className="input mono" value={sample} disabled={busy} onChange={(e) => setSample(e.target.value)} placeholder={t("desktop.perm.try_placeholder")} spellCheck={false} />
        </label>
        {check &&
          (check.error ? (
            <p className="t-body-sm error-text">{check.error}</p>
          ) : (
            <p className="t-body-sm">
              <code>{check.rule}</code> — {describe(check)}
              {check.tested && <strong className={check.matches ? "rule-matches" : "rule-misses"}> {t(check.matches ? "desktop.perm.matches" : "desktop.perm.no_match")}</strong>}
              {check.tested && check.matches && check.redirect && effect === "allow" && <span className="muted"> {t("desktop.perm.still_asks", { file: check.redirect })}</span>}
            </p>
          ))}
        <div className="row" style={{ justifyContent: "space-between", gap: 8 }}>
          <span className="t-body-sm muted">{t("desktop.perm.syntax")}</span>
          <Button small variant="filled" icon={mdiPlus} disabled={busy || !check || !!check.error} onClick={add}>
            {t("desktop.add")}
          </Button>
        </div>
      </section>

      <div className="stack" style={{ gap: 6 }}>
        <div className="row" style={{ gap: 12 }}>
          <span className="t-title-sm spacer">{t("desktop.perm.builtin")}</span>
          {workspace ? (
            <select
              className="select"
              value={desc.readOnlyDefaults}
              disabled={busy}
              aria-label={t("desktop.perm.builtin")}
              onChange={(e) => act(() => config.setReadOnlyDefaults({ workspace, value: e.target.value }), t("desktop.perm.builtin_saved"))}
            >
              <option value="">{t("desktop.perm.builtin_global")}</option>
              <option value="on">{t("desktop.perm.builtin_on")}</option>
              <option value="off">{t("desktop.perm.builtin_off")}</option>
            </select>
          ) : (
            <Switch
              label={t("desktop.perm.builtin")}
              checked={desc.readOnlyDefaultsOn}
              onChange={(on) => act(() => config.setReadOnlyDefaults({ workspace, value: on ? "" : "off" }), t("desktop.perm.builtin_saved"))}
            />
          )}
        </div>
        <p className="t-body-sm muted">
          {desc.readOnlyDefaultsOn
            ? t("desktop.perm.builtin_detail", { commands: desc.readOnlyCommands.join(", "), guards: desc.readOnlyGuards.join(", ") })
            : t("desktop.perm.builtin_none", { state: defaultsState })}
        </p>
      </div>

      {workspace && desc.inherited.length > 0 && (
        <div className="stack" style={{ gap: 6 }}>
          <span className="t-title-sm">{t("desktop.perm.inherited")}</span>
          {list(desc.inherited, false)}
        </div>
      )}
      {workspace && session.length > 0 && (
        <div className="stack" style={{ gap: 6 }}>
          <span className="t-title-sm">{t("desktop.perm.session")}</span>
          <div className="list">
            {session.map((r) => (
              <div key={`${r.effect}:${r.rule}`} className="rule">
                <span className={`chip static effect-${r.effect}`}>{t(`desktop.rs.effect.${r.effect}`)}</span>
                <code className="ellipsis" title={r.rule}>
                  {r.rule}
                </code>
                <IconButton
                  icon={mdiDeleteOutline}
                  label={t("desktop.rs.remove")}
                  small
                  disabled={busy}
                  onClick={() => act(() => workspaces.removePermissionRule({ workspace, rule: r.rule }), t("desktop.perm.removed", { rule: r.rule }))}
                />
              </div>
            ))}
          </div>
        </div>
      )}
      {error && <p className="error-text">{error}</p>}
    </div>
  );
}
