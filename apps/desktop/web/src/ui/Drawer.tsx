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

// The drawer: the views (Settings, Docs, Changes, History, an agent,
// Workers, the inbox, Licenses) slide down from under the top menu as a
// sheet over the window's middle (the Files shelf, the editor, the chat
// and the pinned input) to the status bar. The title bar stays: its menus
// work and it moves the window. Only its ✕ or Escape (with the focus in it) closes it.
import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { mdiClose, mdiUndoVariant } from "@mdi/js";
import { t } from "../i18n";
import { Button, Dialog, Icon, IconButton } from "./controls";
import { DrawerClaims, type Ask, type Claim } from "./drawerClaims";

interface DrawerState {
  slot: HTMLElement | null;
  current: number | null;
  claim: (c: Claim) => () => void;
  requestClose: (c: Claim) => void;
}

const DrawerContext = createContext<DrawerState | null>(null);

/**
 * Holds the window's one drawer: a drawer that opens closes the one that
 * was open (asking first when it has unsaved changes).
 */
export function DrawerProvider({ children }: { children: ReactNode }) {
  const [slot, setSlot] = useState<HTMLElement | null>(null);
  const [current, setCurrent] = useState<number | null>(null);
  const [pending, setPending] = useState<Ask | null>(null);
  const [claims] = useState(() => new DrawerClaims(setCurrent, setPending));
  const claim = useCallback((c: Claim) => claims.claim(c), [claims]);
  const requestClose = useCallback((c: Claim) => claims.requestClose(c), [claims]);
  const value = useMemo<DrawerState>(() => ({ slot, current, claim, requestClose }), [slot, current, claim, requestClose]);
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

/** The frame the drawer slides down in: the main area under the title bar. */
export function DrawerSlot() {
  const set = useContext(DrawerSlotContext);
  return <div className="drawer-slot" ref={set} />;
}

let nextId = 0;

/**
 * A view in the drawer: the counterpart of Dialog for what's browsed or
 * edited for a while, with the window's whole middle to lay it out. ✕ or
 * Escape, with the focus inside it and no dialog over it, closes it. A drawer with unsaved changes (dirty) asks before it closes,
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
  closeRequest = 0,
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
  /** Bumped to close it from outside (its menu button again), as its ✕ does: asking first when dirty. */
  closeRequest?: number;
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
  // Only a request made while it's open: the count it opened with is old.
  const closeAsked = useRef(closeRequest);
  useEffect(() => {
    if (closeRequest === closeAsked.current) return;
    closeAsked.current = closeRequest;
    close();
  }, [closeRequest, close]);

  // Escape closes it when the focus is inside and no dialog is over it.
  useEffect(() => {
    const el = ref.current;
    if (!el || !shown) return;
    const key = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented || document.querySelector('[aria-modal="true"]')) return;
      // A completion menu open in the field (SuggestTextarea's) takes it:
      // React's handler runs after this one, so it can't say so first.
      if ((e.target as HTMLElement | null)?.closest?.(".suggest-field")?.querySelector(".command-menu")) return;
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
  const drawer = (
    <aside
      ref={ref}
      className={`drawer ${className}`}
      hidden={!shown}
      role="complementary"
      aria-label={typeof title === "string" ? title : undefined}
      tabIndex={-1}
    >
      <header className="drawer-head">
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
