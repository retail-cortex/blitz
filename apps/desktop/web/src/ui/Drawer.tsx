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

// The drawer: the views (Settings, Docs, Changes, History, Workers, the
// inbox, Licenses) dock at the window's right, beside the chat, rather
// than over it. It isn't modal: the window, the editor and the chat stay
// usable, and only its ✕ or Escape (with the focus in it) closes it.
import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { mdiClose, mdiUndoVariant } from "@mdi/js";
import { t } from "../i18n";
import { Button, Dialog, Icon, IconButton } from "./controls";
import { panelWidth, useWindowWidth } from "./layout";
import { DrawerClaims, type Ask, type Claim } from "./drawerClaims";
import { ResizeHandle } from "./ResizeHandle";

/** The narrowest a drawer gets, and what the workspace keeps beside a
 *  docked one (the Files rail and the chat at their narrowest, a little
 *  of the editor): in a narrower window the drawer floats over the chat. */
export const drawerMin = 360;
const workspaceMin = 640;

/** A drawer's width: the saved one (0: 720 px, or 45% of a smaller
 *  window), kept to what fits the window. */
export function drawerWidth(saved: number, win: number): number {
  return panelWidth(saved, Math.min(720, Math.round(win * 0.45)), drawerMin, win - 48);
}

/** Whether a drawer that wide docks beside the workspace (else it floats
 *  over the chat at the right). */
export function drawerDocks(width: number, win: number): boolean {
  return win - width >= workspaceMin;
}

interface DrawerState {
  slot: HTMLElement | null;
  current: number | null;
  width: number;
  docked: boolean;
  claim: (c: Claim) => () => void;
  requestClose: (c: Claim) => void;
  resize: (w: number) => void;
}

const DrawerContext = createContext<DrawerState | null>(null);

/**
 * Holds the window's one drawer: a drawer that opens closes the one that
 * was open (asking first when it has unsaved changes). width is the saved
 * width (0: the default); onResize saves a new one.
 */
export function DrawerProvider({ width: saved, onResize, children }: { width: number; onResize: (w: number) => void; children: ReactNode }) {
  const [slot, setSlot] = useState<HTMLElement | null>(null);
  const [current, setCurrent] = useState<number | null>(null);
  const [pending, setPending] = useState<Ask | null>(null);
  const [claims] = useState(() => new DrawerClaims(setCurrent, setPending));
  const claim = useCallback((c: Claim) => claims.claim(c), [claims]);
  const requestClose = useCallback((c: Claim) => claims.requestClose(c), [claims]);
  const win = useWindowWidth();
  const width = drawerWidth(saved, win);
  const docked = drawerDocks(width, win);
  const value = useMemo<DrawerState>(() => ({ slot, current, width, docked, claim, requestClose, resize: onResize }), [slot, current, width, docked, claim, requestClose, onResize]);
  return (
    <DrawerContext.Provider value={value}>
      <DrawerSlotContext.Provider value={setSlot}>{children}</DrawerSlotContext.Provider>
      {pending &&
        createPortal(
          <Dialog
            title={t("desktop.agents.discard_title")}
            icon={mdiUndoVariant}
            onClose={() => {
              pending.keep();
              setPending(null);
            }}
            footer={
              <>
                <Button
                  onClick={() => {
                    pending.keep();
                    setPending(null);
                  }}
                >
                  {t("desktop.cancel")}
                </Button>
                <Button
                  variant="filled"
                  danger
                  onClick={() => {
                    pending.discard();
                    setPending(null);
                  }}
                >
                  {t("desktop.agents.discard")}
                </Button>
              </>
            }
          >
            <p>{t("desktop.agents.discard_body", { name: pending.name })}</p>
          </Dialog>,
          document.body,
        )}
    </DrawerContext.Provider>
  );
}

const DrawerSlotContext = createContext<(el: HTMLElement | null) => void>(() => {});

/** Where the drawer docks: the main row's right end. */
export function DrawerSlot() {
  const set = useContext(DrawerSlotContext);
  return <div className="drawer-slot" ref={set} />;
}

/** How much of the window's width a docked drawer takes (0 with none open,
 *  or one floating over the chat), for the workspace's panels to fit. */
export function useDrawerSpace(): number {
  const s = useContext(DrawerContext);
  return s && s.current !== null && s.docked ? s.width : 0;
}

let nextId = 0;

/**
 * A view in the drawer: the counterpart of Dialog for what's browsed or
 * edited for a while. Its header continues the title bar (it drags the
 * window); ✕ or Escape, with the focus inside it and no dialog over it,
 * closes it. A drawer with unsaved changes (dirty) asks before it closes,
 * or before another drawer takes its place.
 */
export function Drawer({
  title,
  icon,
  onClose,
  footer,
  headerActions,
  className = "",
  dirty = false,
  children,
}: {
  title: ReactNode;
  icon?: string;
  onClose: () => void;
  footer?: ReactNode;
  /** Beside the title, before ✕ (a chip, buttons). */
  headerActions?: ReactNode;
  /** A class for the drawer (what's in it, laid out its own way). */
  className?: string;
  /** Unsaved changes: closing asks first. */
  dirty?: boolean;
  children: ReactNode;
}) {
  const host = useContext(DrawerContext);
  const ref = useRef<HTMLElement>(null);
  const [id] = useState(() => ++nextId);
  const live = useRef({ onClose, dirty, title });
  live.current = { onClose, dirty, title };
  const me = useMemo<Claim>(
    () => ({
      id,
      close: () => live.current.onClose(),
      dirty: () => live.current.dirty,
      name: () => (typeof live.current.title === "string" ? live.current.title : ""),
    }),
    [id],
  );
  const claim = host?.claim;
  useLayoutEffect(() => claim?.(me), [claim, me]);
  const close = useCallback(() => (host ? host.requestClose(me) : me.close()), [host, me]);
  const shown = !host || host.current === id;

  // Escape closes it when the focus is inside and no dialog is over it.
  useEffect(() => {
    const el = ref.current;
    if (!el || !shown) return;
    const key = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented || document.querySelector('[aria-modal="true"]')) return;
      e.preventDefault();
      close();
    };
    el.addEventListener("keydown", key);
    return () => el.removeEventListener("keydown", key);
  }, [shown, close]);
  // The focus goes in when it opens (to what asks for it, else the first
  // field, else the drawer), and back where it was when it closes.
  useEffect(() => {
    if (!shown) return;
    const before = document.activeElement as HTMLElement | null;
    const el = ref.current;
    const first = el?.querySelector<HTMLElement>("[data-autofocus]") ?? el?.querySelector<HTMLElement>("input, textarea, select, [autofocus]");
    (first ?? el)?.focus({ preventScroll: true });
    return () => {
      if (before?.isConnected && (!document.activeElement || document.activeElement === document.body || el?.contains(document.activeElement))) before.focus({ preventScroll: true });
    };
  }, [shown]);

  // One waiting behind a question (keep its changes, or not) stays mounted,
  // hidden, so keeping them finds them as they were.
  if (host && !host.slot) return null;
  const width = host?.width ?? drawerMin;
  const drawer = (
    <aside
      ref={ref}
      className={`drawer ${host && !host.docked ? "floating" : ""} ${className}`}
      style={{ width }}
      hidden={!shown}
      role="complementary"
      aria-label={typeof title === "string" ? title : undefined}
      tabIndex={-1}
    >
      {host && (
        <ResizeHandle width={width} edge="left" min={drawerMin} max={() => window.innerWidth - 48} label={t("desktop.drawer.resize")} onResize={host.resize} />
      )}
      <header className="drawer-head drag-region">
        {icon && <Icon path={icon} className="muted" />}
        <h2 className="t-title spacer">{title}</h2>
        {headerActions}
        <IconButton icon={mdiClose} label={t("desktop.close")} onClick={close} />
      </header>
      <div className="body">{children}</div>
      {footer && <footer>{footer}</footer>}
    </aside>
  );
  return host ? createPortal(drawer, host.slot!) : drawer;
}
