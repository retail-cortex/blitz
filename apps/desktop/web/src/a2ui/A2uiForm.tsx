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

import { useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { mdiAccountCircleOutline, mdiAlertCircleOutline, mdiCheck, mdiClose, mdiPlus } from "@mdi/js";
import { t } from "../i18n";
import { Button, Chip, Dialog, Field, Icon, Segmented, Tabs } from "../ui/controls";
import { actionOf, answerOf, byId, childrenOf, failing, formProblems, resolve, safeUrl, scoped, setPath, type Component, type Form } from "./model";

/** The basic catalog's icons this renderer draws (others are left out). */
const icons: Record<string, string> = { check: mdiCheck, close: mdiClose, add: mdiPlus, accountCircle: mdiAccountCircleOutline, error: mdiAlertCircleOutline };

/** Text variants as the app's type scale. */
const textClass: Record<string, string> = { h1: "t-headline", h2: "t-headline", h3: "t-title", h4: "t-title-sm", h5: "t-label", caption: "t-body-sm muted", body: "t-body" };

/** Row and column alignment as CSS. */
const justify: Record<string, string> = { start: "flex-start", center: "center", end: "flex-end", spaceBetween: "space-between", spaceAround: "space-around", spaceEvenly: "space-evenly", stretch: "stretch" };

/**
 * An A2UI v0.9 form (spec_workflows_043 WF-18), drawn with the app's own
 * controls: the basic catalog's components, inputs bound to its data, the
 * catalog's checks, and buttons whose actions answer it (onAction with the
 * action's name and context). Nothing in a form is run as code or HTML:
 * text is text, and images only https or inline.
 */
export function A2uiForm({ form, onAction, disabled }: { form: Form; onAction?: (answer: Record<string, unknown>) => void; disabled?: boolean }) {
  const [data, setData] = useState<Record<string, unknown>>(() => structuredClone(form.data ?? {}));
  const [tab, setTab] = useState<Record<string, string>>({});
  const [modal, setModal] = useState("");
  const [touched, setTouched] = useState(false);
  const all = byId(form);
  const problems = formProblems(form, data);
  const set = (path: string, v: unknown) => setData((d) => setPath(d, path, v));

  const draw = (id: string, scope: string, depth: number): ReactNode => {
    const c = all.get(id);
    if (!c || depth > 24) return null;
    const val = (p: unknown) => resolve(p, data, scope);
    const bound = (p: unknown) => (p && typeof p === "object" && typeof (p as { path?: unknown }).path === "string" ? scoped((p as { path: string }).path, scope) : "");
    const kids = () => childrenOf(c, data, scope).map((k, i) => <span key={`${k.id}-${i}`} style={{ display: "contents" }}>{draw(k.id, k.scope, depth + 1)}</span>);
    const label = typeof val(c.label) === "string" ? String(val(c.label)) : "";
    const errors = touched ? failing(c, data, scope) : [];
    switch (c.component) {
      case "Text":
        return <p className={`a2ui-text ${textClass[String(c.variant ?? "body")] ?? "t-body"}`}>{String(val(c.text) ?? "")}</p>;
      case "Row":
      case "Column":
      case "List":
        return (
          <div
            className={c.component === "Row" || c.direction === "horizontal" ? "row wrap a2ui-row" : "stack a2ui-column"}
            style={{ justifyContent: justify[String(c.justify ?? "")], alignItems: justify[String(c.align ?? "")] ?? (c.component === "Row" ? "center" : undefined) }}
          >
            {kids()}
          </div>
        );
      case "Card":
        return <div className="card outlined stack a2ui-card">{typeof c.child === "string" && draw(c.child, scope, depth + 1)}</div>;
      case "Divider":
        return <hr className="a2ui-divider" />;
      case "Image":
        return safeUrl(val(c.url)) ? <img className={`a2ui-image ${String(c.variant ?? "")}`} src={String(val(c.url))} alt={String(val(c.description) ?? "")} style={{ objectFit: (String(c.fit ?? "contain") as "contain") || "contain" }} /> : null;
      case "Icon":
        return icons[String(val(c.name))] ? <Icon path={icons[String(val(c.name))]} /> : null;
      case "Button": {
        const action = actionOf(c, data, scope);
        const blocked = failing(c, data, scope).length > 0 || (action !== null && problems.size > 0 && touched);
        return (
          <Button
            variant={c.variant === "primary" ? "filled" : c.variant === "borderless" ? "text" : "tonal"}
            disabled={disabled || !action || blocked}
            onClick={() => {
              setTouched(true);
              if (!action || formProblems(form, data).size > 0) return;
              onAction?.(answerOf(action));
            }}
          >
            {typeof c.child === "string" ? textOf(all.get(c.child), data, scope) : ""}
          </Button>
        );
      }
      case "TextField": {
        const path = bound(c.value);
        const v = val(c.value);
        const variant = String(c.variant ?? "shortText");
        return (
          <Field label={label} error={errors[0]}>
            {(fid) =>
              variant === "longText" ? (
                <textarea id={fid} className="input" rows={4} value={String(v ?? "")} disabled={disabled || !path} onChange={(e) => set(path, e.target.value)} />
              ) : (
                <input
                  id={fid}
                  className="input"
                  type={variant === "number" ? "number" : variant === "obscured" ? "password" : "text"}
                  value={String(v ?? "")}
                  disabled={disabled || !path}
                  onChange={(e) => set(path, variant === "number" ? (e.target.value === "" ? "" : Number(e.target.value)) : e.target.value)}
                />
              )
            }
          </Field>
        );
      }
      case "CheckBox": {
        const path = bound(c.value);
        return (
          <label className="row a2ui-check">
            <input type="checkbox" checked={val(c.value) === true} disabled={disabled || !path} onChange={(e) => set(path, e.target.checked)} />
            <span>{label}</span>
          </label>
        );
      }
      case "ChoicePicker": {
        const path = bound(c.value);
        const chosen = Array.isArray(val(c.value)) ? (val(c.value) as unknown[]).map(String) : [];
        const options = (Array.isArray(c.options) ? (c.options as { label?: unknown; value?: unknown }[]) : []).map((o) => ({ label: String(val(o.label) ?? o.value), value: String(o.value) }));
        const multiple = c.variant === "multipleSelection";
        const body =
          !multiple && options.length <= 4 && c.displayStyle !== "chips" ? (
            <Segmented label={label} value={chosen[0] ?? ""} options={options} onChange={(v) => set(path, [v])} />
          ) : !multiple && c.displayStyle !== "chips" ? (
            <select className="select" aria-label={label} value={chosen[0] ?? ""} disabled={disabled} onChange={(e) => set(path, [e.target.value])}>
              <option value="" />
              {options.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
          ) : (
            <div className="row wrap" style={{ gap: 6 }}>
              {options.map((o) => {
                const on = chosen.includes(o.value);
                return (
                  <Chip key={o.value} selected={on} aria-pressed={on} disabled={disabled} onClick={() => set(path, multiple ? (on ? chosen.filter((x) => x !== o.value) : [...chosen, o.value]) : [o.value])}>
                    {o.label}
                  </Chip>
                );
              })}
            </div>
          );
        return (
          <div className="stack a2ui-choice" style={{ gap: 4 }}>
            {label && <span className="t-label">{label}</span>}
            {body}
            {errors[0] && <span className="t-body-sm error-text">{errors[0]}</span>}
          </div>
        );
      }
      case "Slider": {
        const path = bound(c.value);
        const v = Number(val(c.value) ?? val(c.min) ?? 0);
        return (
          <Field label={`${label} · ${v}`}>
            {(fid) => <input id={fid} type="range" className="a2ui-slider" min={Number(val(c.min) ?? 0)} max={Number(val(c.max) ?? 100)} value={v} disabled={disabled || !path} onChange={(e) => set(path, Number(e.target.value))} />}
          </Field>
        );
      }
      case "DateTimeInput": {
        const path = bound(c.value);
        const type = c.enableDate === false ? "time" : c.enableTime === true ? "datetime-local" : "date";
        return (
          <Field label={label} error={errors[0]}>
            {(fid) => <input id={fid} className="input" type={type} value={String(val(c.value) ?? "")} min={val(c.min) as string | undefined} max={val(c.max) as string | undefined} disabled={disabled || !path} onChange={(e) => set(path, e.target.value)} />}
          </Field>
        );
      }
      case "Tabs": {
        const tabs = (Array.isArray(c.tabs) ? (c.tabs as { title?: unknown; child?: string }[]) : []).map((x, i) => ({ value: String(i), label: String(val(x.title) ?? i + 1), child: x.child }));
        const cur = tab[c.id] ?? "0";
        const active = tabs.find((x) => x.value === cur);
        return (
          <div className="stack" style={{ gap: 8 }}>
            <Tabs value={cur} label={label || c.id} tabs={tabs} onChange={(v) => setTab((s) => ({ ...s, [c.id]: v }))} />
            {active?.child && draw(active.child, scope, depth + 1)}
          </div>
        );
      }
      case "Modal":
        return (
          <>
            <span onClickCapture={() => setModal(c.id)} style={{ display: "contents" }}>
              {typeof c.trigger === "string" && draw(c.trigger, scope, depth + 1)}
            </span>
            {modal === c.id &&
              createPortal(
                <Dialog title={label} onClose={() => setModal("")} footer={<Button onClick={() => setModal("")}>{t("desktop.close")}</Button>}>
                  {typeof c.content === "string" && draw(c.content, scope, depth + 1)}
                </Dialog>,
                document.body,
              )}
          </>
        );
      default:
        return <p className="t-body-sm muted">{t("desktop.a2ui.unknown", { name: c.component })}</p>;
    }
  };
  return <div className="a2ui-form stack">{all.has("root") ? draw("root", "", 0) : <p className="t-body-sm error-text">{t("desktop.a2ui.no_root")}</p>}</div>;
}

/** A button's label: its child's text. */
function textOf(c: Component | undefined, data: Record<string, unknown>, scope: string): string {
  if (!c) return "";
  return String(resolve(c.text, data, scope) ?? "");
}
