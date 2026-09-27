// Material 3 controls, drawn with the tokens in m3.css. Small and
// dependency-free: each is the few behaviours the app needs.
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type ReactNode,
} from "react";
import { mdiClose } from "@mdi/js";

/** A Material Design icon, given its SVG path (from @mdi/js). */
export function Icon({ path, size, spin, className = "" }: { path: string; size?: "sm" | "lg"; spin?: boolean; className?: string }) {
  return (
    <svg className={`icon ${size ?? ""} ${spin ? "spin" : ""} ${className}`} viewBox="0 0 24 24" aria-hidden="true">
      <path d={path} />
    </svg>
  );
}

type Variant = "filled" | "tonal" | "outlined" | "text";

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

export function Switch({ checked, onChange, label }: { checked: boolean; onChange: (v: boolean) => void; label: string }) {
  return (
    <span className="switch">
      <input type="checkbox" role="switch" aria-label={label} checked={checked} onChange={(e) => onChange(e.target.checked)} />
      <span />
    </span>
  );
}

/** Closes on a click outside ref or on Escape. */
function useDismiss(open: boolean, ref: React.RefObject<HTMLElement | null>, close: () => void) {
  useEffect(() => {
    if (!open) return;
    const down = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) close();
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

export interface MenuItem {
  label: string;
  detail?: string;
  icon?: string;
  on?: boolean;
  danger?: boolean;
  disabled?: boolean;
  onSelect: () => void;
}

/** A button that opens a menu of items (or dividers and labels). */
export function Menu({
  trigger,
  items,
  placement = "down start",
}: {
  trigger: (props: { onClick: () => void; "aria-expanded": boolean; "aria-haspopup": "menu" }) => ReactNode;
  items: (MenuItem | "divider" | { heading: string })[];
  placement?: string;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLSpanElement>(null);
  const close = useCallback(() => setOpen(false), []);
  useDismiss(open, ref, close);
  return (
    <span className="menu-anchor" ref={ref}>
      {trigger({ onClick: () => setOpen((o) => !o), "aria-expanded": open, "aria-haspopup": "menu" })}
      {open && (
        <div className={`menu ${placement}`} role="menu">
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
                  setOpen(false);
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
      )}
    </span>
  );
}

/** A modal dialog; Escape or a click on the scrim closes it. */
export function Dialog({
  title,
  icon,
  onClose,
  footer,
  wide,
  children,
}: {
  title: ReactNode;
  icon?: string;
  onClose: () => void;
  footer?: ReactNode;
  wide?: boolean;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const key = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    document.addEventListener("keydown", key);
    // Focus the first field, else the dialog, so keys go to it.
    const first = ref.current?.querySelector<HTMLElement>("input, textarea, select, [autofocus]");
    (first ?? ref.current)?.focus();
    return () => document.removeEventListener("keydown", key);
  }, [onClose]);
  return (
    <div className="scrim" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className={`dialog ${wide ? "wide" : ""}`} role="dialog" aria-modal="true" tabIndex={-1} ref={ref}>
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
          <IconButton icon={mdiClose} label="Dismiss" small onClick={() => setSnack(null)} />
        </div>
      )}
    </SnackContext.Provider>
  );
}

export const useSnackbar = () => useContext(SnackContext);
