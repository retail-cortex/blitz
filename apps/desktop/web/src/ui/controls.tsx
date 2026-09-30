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

// Material 3 controls, drawn with the tokens in m3.css. Small and
// dependency-free: each is the few behaviours the app needs.
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type CSSProperties,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { mdiClose } from "@mdi/js";
import { t } from "../i18n";

/** A Material Design icon, given its SVG path (from @mdi/js). */
export function Icon({ path, size, spin, className = "" }: { path: string; size?: "sm" | "lg"; spin?: boolean; className?: string }) {
  return (
    <svg className={`icon ${size ?? ""} ${spin ? "spin" : ""} ${className}`} viewBox="0 0 24 24" aria-hidden="true">
      <path d={path} />
    </svg>
  );
}

type Variant = "filled" | "tonal" | "outlined" | "text";

/** A Material 3 button: text (the default), tonal, filled or outlined, with an optional icon. */
export function Button({
  variant = "text",
  icon,
  danger,
  small,
  className = "",
  children,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; icon?: string; danger?: boolean; small?: boolean }) {
  return (
    <button type="button" className={`btn ${variant} ${icon ? "has-icon" : ""} ${danger ? "danger" : ""} ${small ? "small" : ""} ${className}`} {...rest}>
      {icon && <Icon path={icon} size="sm" />}
      {children}
    </button>
  );
}

/** A button with only an icon; label is its tooltip and accessible name. */
export function IconButton({
  icon,
  label,
  variant,
  selected,
  small,
  className = "",
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { icon: string; label: string; variant?: "filled" | "tonal"; selected?: boolean; small?: boolean }) {
  return (
    <button
      type="button"
      className={`icon-btn ${variant ?? ""} ${selected ? "selected" : ""} ${small ? "small" : ""} ${className}`}
      aria-label={label}
      title={label}
      aria-pressed={selected}
      {...rest}
    >
      <Icon path={icon} />
    </button>
  );
}

/** A Material 3 chip: a small labelled button or status, optionally selected or toned. */
export function Chip({
  icon,
  selected,
  tone,
  className = "",
  children,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { icon?: string; selected?: boolean; tone?: "warn" | "danger" }) {
  return (
    <button type="button" className={`chip ${selected ? "selected" : ""} ${tone ?? ""} ${className}`} {...rest}>
      {icon && <Icon path={icon} />}
      {children}
    </button>
  );
}

/** A segmented button: one of a few options, as radio buttons. */
export function Segmented<T extends string>({
  value,
  options,
  onChange,
  small,
  label,
}: {
  value: T;
  options: { value: T; label: string; icon?: string }[];
  onChange: (v: T) => void;
  small?: boolean;
  label: string;
}) {
  return (
    <div className={`seg ${small ? "small" : ""}`} role="radiogroup" aria-label={label}>
      {options.map((o) => (
        <button key={o.value} type="button" role="radio" aria-checked={o.value === value} className={o.value === value ? "on" : ""} onClick={() => onChange(o.value)}>
          {o.icon && <Icon path={o.icon} size="sm" />}
          <span className="seg-label">{o.label}</span>
        </button>
      ))}
    </div>
  );
}

/** A labelled field around an input, select or text area. */
export function Field({ label, supporting, error, children }: { label: string; supporting?: ReactNode; error?: string; children: (id: string) => ReactNode }) {
  const id = useId();
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      {children(id)}
      {error ? <span className="supporting error-text">{error}</span> : supporting && <span className="supporting">{supporting}</span>}
    </div>
  );
}

/** An on/off switch; label is its accessible name. */
export function Switch({ checked, onChange, label, disabled }: { checked: boolean; onChange: (v: boolean) => void; label: string; disabled?: boolean }) {
  return (
    <span className="switch">
      <input type="checkbox" role="switch" aria-label={label} checked={checked} disabled={disabled} onChange={(e) => onChange(e.target.checked)} />
      <span />
    </span>
  );
}

/** Closes on a click outside ref or on Escape. */
export function useDismiss(open: boolean, ref: React.RefObject<HTMLElement | null> | React.RefObject<HTMLElement | null>[], close: () => void) {
  useEffect(() => {
    if (!open) return;
    const refs = Array.isArray(ref) ? ref : [ref];
    const down = (e: MouseEvent) => {
      if (refs.some((r) => r.current) && !refs.some((r) => r.current?.contains(e.target as Node))) close();
    };
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        close();
      }
    };
    document.addEventListener("mousedown", down);
    document.addEventListener("keydown", key, true);
    return () => {
      document.removeEventListener("mousedown", down);
      document.removeEventListener("keydown", key, true);
    };
  }, [open, ref, close]);
}

/** An item of a menu: its label, icon and detail, and what choosing it does. */
export interface MenuItem {
  label: string;
  detail?: string;
  icon?: string;
  on?: boolean;
  danger?: boolean;
  disabled?: boolean;
  onSelect: () => void;
}

/** A rectangle in the window, as getBoundingClientRect gives it. */
export interface Box {
  top: number;
  bottom: number;
  left: number;
  right: number;
}

/**
 * Where a menu opens beside its anchor (placement "up" or "down", "start"
 * or "end"), fixed to the window: above or below it (the other side when
 * that one has under 200 px and the other more), aligned with its start or
 * end, and no taller than the room on its side.
 */
export function menuPosition(anchor: Box, placement: string, vw: number, vh: number): CSSProperties {
  const above = anchor.top - 12;
  const below = vh - anchor.bottom - 12;
  let up = placement.includes("up");
  if (up && above < 200 && below > above) up = false;
  else if (!up && below < 200 && above > below) up = true;
  const s: CSSProperties = up ? { bottom: vh - anchor.top + 4, maxHeight: Math.max(120, anchor.top - 12) } : { top: anchor.bottom + 4, maxHeight: Math.max(120, vh - anchor.bottom - 12) };
  if (placement.includes("end")) s.right = vw - anchor.right;
  else s.left = anchor.left;
  return s;
}

/** How far to move a box so it's inside the window, with a margin. */
export function nudge(box: Box, vw: number, vh: number, margin = 8): { x: number; y: number } {
  const x = box.right > vw - margin ? vw - margin - box.right : 0;
  const y = box.bottom > vh - margin ? vh - margin - box.bottom : 0;
  return { x: box.left + x < margin ? margin - box.left : x, y: box.top + y < margin ? margin - box.top : y };
}

// A menu floating over the whole window (in document.body, so no panel
// clips it or makes its position relative), moved inside the window once
// its size is known.
function FloatingMenu({ items, onPick, style, className = "", listRef }: { items: MenuEntry[]; onPick: () => void; style: CSSProperties; className?: string; listRef: React.RefObject<HTMLDivElement | null> }) {
  useLayoutEffect(() => {
    const el = listRef.current;
    if (!el) return;
    const { x, y } = nudge(el.getBoundingClientRect(), window.innerWidth, window.innerHeight);
    if (x || y) el.style.transform = `translate(${x}px, ${y}px)`;
    // Keys go to it: the chosen item, else the first.
    (el.querySelector<HTMLElement>(".menu-item.on:not(:disabled)") ?? menuItems(el)[0])?.focus({ preventScroll: true });
  }, [listRef]);
  return createPortal(<MenuList className={`menu floating ${className}`} items={items} onPick={onPick} style={style} listRef={listRef} />, document.body);
}

/** A button that opens a menu of items (or dividers and labels); clicking it again closes it. */
export function Menu({
  trigger,
  items,
  placement = "down start",
  className,
}: {
  trigger: (props: { onClick: () => void; "aria-expanded": boolean; "aria-haspopup": "menu" }) => ReactNode;
  items: MenuEntry[];
  placement?: string;
  /** A class for the menu (its width, say). */
  className?: string;
}) {
  const [at, setAt] = useState<CSSProperties | null>(null);
  const ref = useRef<HTMLSpanElement>(null);
  const list = useRef<HTMLDivElement>(null);
  // Closing gives the focus back to the button.
  const close = useCallback(() => {
    setAt(null);
    ref.current?.querySelector<HTMLElement>("button, [tabindex]")?.focus({ preventScroll: true });
  }, []);
  useDismiss(!!at, [ref, list], close);
  // Closed by the window changing size: its place would be wrong.
  useEffect(() => {
    if (!at) return;
    window.addEventListener("resize", close);
    return () => window.removeEventListener("resize", close);
  }, [at, close]);
  const toggle = () => setAt((cur) => (cur || !ref.current ? null : menuPosition(ref.current.getBoundingClientRect(), placement, window.innerWidth, window.innerHeight)));
  return (
    <span className="menu-anchor" ref={ref}>
      {trigger({ onClick: toggle, "aria-expanded": !!at, "aria-haspopup": "menu" })}
      {at && <FloatingMenu items={items} onPick={close} style={at} className={className} listRef={list} />}
    </span>
  );
}

/** What a menu lists: items, dividers and headings. */
export type MenuEntry = MenuItem | "divider" | { heading: string };

/** A menu's items that can be chosen. */
function menuItems(el: HTMLElement): HTMLElement[] {
  return [...el.querySelectorAll<HTMLElement>('[role="menuitem"]:not(:disabled)')];
}

// Arrow keys move through a menu's items (wrapping), Home and End to its ends.
function menuKeys(e: React.KeyboardEvent<HTMLDivElement>) {
  const items = menuItems(e.currentTarget);
  if (!items.length) return;
  const at = items.indexOf(document.activeElement as HTMLElement);
  const to = { ArrowDown: at + 1, ArrowUp: at < 0 ? items.length - 1 : at - 1, Home: 0, End: items.length - 1 }[e.key];
  if (to === undefined) return;
  e.preventDefault();
  items[(to + items.length) % items.length].focus();
}

function MenuList({ className, items, onPick, style, listRef }: { className: string; items: MenuEntry[]; onPick: () => void; style?: React.CSSProperties; listRef?: React.RefObject<HTMLDivElement | null> }) {
  return (
    <div className={className} role="menu" style={style} ref={listRef} onKeyDown={menuKeys}>
      {items.map((it, i) =>
        it === "divider" ? (
          <div key={i} className="menu-divider" />
        ) : "heading" in it ? (
          <div key={i} className="menu-label">
            {it.heading}
          </div>
        ) : (
          <button
            key={i}
            type="button"
            role="menuitem"
            className={`menu-item ${it.on ? "on" : ""}`}
            disabled={it.disabled}
            style={it.danger ? { color: "var(--md-error)" } : undefined}
            onClick={() => {
              onPick();
              it.onSelect();
            }}
          >
            {it.icon && <Icon path={it.icon} />}
            <span>
              {it.label}
              {it.detail && <small>{it.detail}</small>}
            </span>
          </button>
        ),
      )}
    </div>
  );
}

/** A menu at a point (a right click); a click elsewhere or Escape closes it. */
export function ContextMenu({ x, y, items, onClose }: { x: number; y: number; items: MenuEntry[]; onClose: () => void }) {
  const list = useRef<HTMLDivElement>(null);
  useDismiss(true, list, onClose);
  // Kept inside the window once its size is known (FloatingMenu).
  return <FloatingMenu items={items} onPick={onClose} style={{ left: x, top: y, maxHeight: window.innerHeight - 16 }} className="context-menu" listRef={list} />;
}

/** The elements Tab reaches inside el, in order. */
function focusables(el: HTMLElement): HTMLElement[] {
  return [...el.querySelectorAll<HTMLElement>('a[href], button, input, select, textarea, [tabindex]:not([tabindex="-1"])')].filter(
    (x) => !x.hasAttribute("disabled") && x.tabIndex >= 0 && x.getClientRects().length > 0,
  );
}

/** A modal dialog; Escape or a click on the scrim closes it. */
export function Dialog({
  title,
  icon,
  onClose,
  footer,
  wide,
  large,
  children,
}: {
  title: ReactNode;
  icon?: string;
  onClose: () => void;
  footer?: ReactNode;
  wide?: boolean;
  /** Most of the window (Settings). */
  large?: boolean;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  useEffect(() => {
    // Only the topmost dialog answers keys (one can open over another).
    const top = () => {
      const all = document.querySelectorAll(".dialog");
      return all[all.length - 1] === ref.current;
    };
    const key = (e: KeyboardEvent) => {
      if (!top() || !ref.current) return;
      // Escape closes it, unless something inside used the key (an
      // editor's completion list, a menu).
      if (e.key === "Escape" && !e.defaultPrevented) closeRef.current();
      // Tab moves through everything inside it, buttons too (WebKit skips
      // them by default), and stays inside; an editor may use Tab itself.
      if (e.key === "Tab" && !e.defaultPrevented) {
        const items = focusables(ref.current);
        if (!items.length) return;
        e.preventDefault();
        const at = items.indexOf(document.activeElement as HTMLElement);
        const next = at < 0 ? (e.shiftKey ? items.length - 1 : 0) : (at + (e.shiftKey ? -1 : 1) + items.length) % items.length;
        items[next].focus();
      }
    };
    document.addEventListener("keydown", key);
    // Focus what asks for it, else the first field, else the dialog, so
    // keys go to it; give the focus back when it closes.
    const before = document.activeElement as HTMLElement | null;
    const first = ref.current?.querySelector<HTMLElement>("[data-autofocus]") ?? ref.current?.querySelector<HTMLElement>("input, textarea, select, [autofocus]");
    (first ?? ref.current)?.focus();
    return () => {
      document.removeEventListener("keydown", key);
      if (before?.isConnected) before.focus({ preventScroll: true });
    };
  }, []);
  return (
    <div className="scrim" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className={`dialog ${wide ? "wide" : ""} ${large ? "large" : ""}`} role="dialog" aria-modal="true" tabIndex={-1} ref={ref}>
        <header>
          {icon && <Icon path={icon} size="lg" className="muted" />}
          <h2 className="t-headline">{title}</h2>
        </header>
        <div className="body">{children}</div>
        {footer && <footer>{footer}</footer>}
      </div>
    </div>
  );
}

type Snack = { id: number; text: string; action?: { label: string; run: () => void }; error?: boolean };
const SnackContext = createContext<(text: string, opts?: { action?: Snack["action"]; error?: boolean }) => void>(() => {});

/** Shows short messages at the bottom of the window, one at a time. */
export function SnackbarProvider({ children }: { children: ReactNode }) {
  const [snack, setSnack] = useState<Snack | null>(null);
  const next = useRef(0);
  const show = useCallback((text: string, opts?: { action?: Snack["action"]; error?: boolean }) => {
    setSnack({ id: ++next.current, text, ...opts });
  }, []);
  useEffect(() => {
    if (!snack) return;
    const t = setTimeout(() => setSnack((s) => (s?.id === snack.id ? null : s)), snack.error ? 8000 : 4000);
    return () => clearTimeout(t);
  }, [snack]);
  return (
    <SnackContext.Provider value={show}>
      {children}
      {snack && (
        <div className="snackbar" role="status">
          <span className="spacer">{snack.text}</span>
          {snack.action && (
            <Button
              onClick={() => {
                snack.action!.run();
                setSnack(null);
              }}
            >
              {snack.action.label}
            </Button>
          )}
          <IconButton icon={mdiClose} label={t("desktop.dismiss")} small onClick={() => setSnack(null)} />
        </div>
      )}
    </SnackContext.Provider>
  );
}

/** Shows a short message at the bottom of the window (inside SnackbarProvider). */
export const useSnackbar = () => useContext(SnackContext);
