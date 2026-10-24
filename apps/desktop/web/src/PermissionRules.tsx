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
import { mdiDeleteOutline, mdiPlus } from "@mdi/js";
import { t } from "./i18n";
import { Button, IconButton } from "./ui/controls";
import { parseRules, ruleExamples, ruleKinds, rulesText, type Rule, type RuleKind } from "./workerForm";

/**
 * What a job may do without asking, as rows (spec_workers_023 WK-71): a
 * kind (run commands, write files, …) and the pattern it covers, each
 * saying what it allows. value is the form's kind:pattern lines, so the
 * job's file is written as before. Kinds the host's policy doesn't allow
 * (allow; undefined: not known yet) are offered disabled, with why.
 */
export function PermissionRules({ value, onChange, allow }: { value: string; onChange: (text: string) => void; allow?: readonly string[] }) {
  // The rows as typed (a pattern's trailing space kept): read again from
  // value only when it changes from outside, such as a rewrite.
  const [shown, setShown] = useState<Rule[]>(() => parseRules(value));
  const sent = useRef(value);
  useEffect(() => {
    if (value !== sent.current) {
      sent.current = value;
      setShown(parseRules(value));
    }
  }, [value]);
  const update = (rows: Rule[]) => {
    setShown(rows);
    sent.current = rulesText(rows);
    onChange(sent.current);
  };
  const set = (i: number, r: Rule) => update(shown.map((x, j) => (j === i ? r : x)));
  const remove = (i: number) => update(shown.filter((_, j) => j !== i));
  const allowed = (k: RuleKind) => !allow || allow.includes(k);
  const firstAllowed = ruleKinds.find(allowed) ?? "write";
  return (
    <div className="stack permission-rules" style={{ gap: 6 }}>
      {shown.map((r, i) => (
        <div key={i} className="stack" style={{ gap: 2 }}>
          <div className="rule-add-row">
            <select className="select rule-kind" value={r.kind} aria-label={t("desktop.newworker.rule_kind")} onChange={(e) => set(i, { ...r, kind: e.target.value as RuleKind })}>
              {r.kind === "" && <option value="">{t("desktop.newworker.rule_kind.other")}</option>}
              {ruleKinds.map((k) => (
                <option key={k} value={k} disabled={!allowed(k)}>
                  {t(`desktop.newworker.rule_kind.${k}`)}
                  {allowed(k) ? "" : ` (${t("desktop.newworker.rule_not_allowed")})`}
                </option>
              ))}
            </select>
            <input
              className="input mono"
              value={r.pattern}
              spellCheck={false}
              aria-label={t("desktop.newworker.rule_pattern")}
              placeholder={r.kind ? ruleExamples[r.kind][0] : ""}
              onChange={(e) => set(i, { ...r, pattern: e.target.value })}
            />
            <IconButton icon={mdiDeleteOutline} label={t("desktop.newworker.rule_remove")} small onClick={() => remove(i)} />
          </div>
          <span className={`t-body-sm ${r.kind && !allowed(r.kind) ? "error-text" : "muted"}`}>
            {r.kind && !allowed(r.kind)
              ? t("desktop.newworker.rule_refused")
              : r.kind && r.pattern.trim()
                ? t(`desktop.newworker.rule_says.${r.kind}`, { pattern: r.pattern.trim() })
                : r.kind
                  ? t("desktop.newworker.rule_examples", { examples: ruleExamples[r.kind].join(", ") })
                  : t("desktop.newworker.rule_other")}
          </span>
        </div>
      ))}
      {shown.length === 0 && <p className="t-body-sm muted">{t("desktop.newworker.rules_none")}</p>}
      <span>
        <Button small icon={mdiPlus} onClick={() => update([...shown, { kind: firstAllowed, pattern: "" }])}>
          {t("desktop.newworker.rule_add")}
        </Button>
      </span>
    </div>
  );
}
