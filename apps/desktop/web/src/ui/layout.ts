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


// The window's width, followed as it changes (a resize, a move to another
// display), and panel widths kept to what fits it.
import { useEffect, useRef, useState } from "react";

/** The window's width, updated as it's resized or moved to another display. */
export function useWindowWidth(): number {
  const [width, setWidth] = useState(() => window.innerWidth);
  useEffect(() => {
    let frame = 0;
    const f = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => setWidth(window.innerWidth));
    };
    window.addEventListener("resize", f);
    // A move to a display with another scale can change the width without
    // a resize event in some web views.
    const scale = window.matchMedia(`(resolution: ${window.devicePixelRatio}dppx)`);
    scale.addEventListener?.("change", f);
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener("resize", f);
      scale.removeEventListener?.("change", f);
    };
  }, []);
  return width;
}

/**
 * A panel's width: the saved one (0: fallback), kept between min and max
 * (max never below min), so a width saved on a larger display still fits.
 */
export function panelWidth(saved: number, fallback: number, min: number, max: number): number {
  return Math.round(Math.min(Math.max(saved || fallback, min), Math.max(min, max)));
}

/**
 * While a panel floats over the others (query, a media query, matches), a
 * click outside it or Escape closes it. Its own toggles (data-toggles=name)
 * are left to toggle it, and clicks in menus, dialogs and the snackbar
 * don't count.
 */
export function useFloatingDismiss(open: boolean, ref: React.RefObject<HTMLElement | null>, query: string, name: string, close: () => void) {
  // The listeners stay put while it's open: a click that closes another
  // panel re-renders between listeners, and a listener added then would
  // miss the click.
  const closeRef = useRef(close);
  closeRef.current = close;
  useEffect(() => {
    if (!open) return;
    const media = window.matchMedia(query);
    const down = (e: MouseEvent) => {
      const target = e.target as Element | null;
      if (!media.matches || !target || ref.current?.contains(target)) return;
      if (target.closest(`.menu, .scrim, .snackbar, [data-toggles~="${name}"]`)) return;
      closeRef.current();
    };
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape" && media.matches && !e.defaultPrevented && !document.querySelector(".scrim, .menu.floating")) closeRef.current();
    };
    document.addEventListener("mousedown", down);
    document.addEventListener("keydown", key);
    return () => {
      document.removeEventListener("mousedown", down);
      document.removeEventListener("keydown", key);
    };
  }, [open, ref, query, name]);
}

/** Below this width the run settings float over the chat. */
export const runSettingsFloat = 1280;
/** Below this width the Files shelf floats over the editor. */
export const filesFloat = 1100;
