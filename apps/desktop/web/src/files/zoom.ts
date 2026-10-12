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

// The editor's zoom (spec_files_029 FIL-66): the code editor and the
// documents (Preview, Visual) in the editor pane, never the rest of the
// window. ⌘/Ctrl with + − 0, ⌘/Ctrl and the wheel, or a pinch; the pane
// sets --zoom, which their sizes multiply. Kept in the preferences.
import { useCallback, useEffect, useRef, useState, type RefObject } from "react";
import { useApp } from "../state";

/** The zoom steps, in percent, as browsers have them. */
export const zoomSteps = [50, 67, 75, 80, 90, 100, 110, 125, 150, 175, 200];

/** z kept within the steps' range, in whole percent. */
export function clampZoom(z: number): number {
  return Math.round(Math.min(200, Math.max(50, z)));
}

/** The step after z (dir 1) or before it (-1). */
export function stepZoom(z: number, dir: 1 | -1): number {
  if (dir > 0) return zoomSteps.find((s) => s > z) ?? 200;
  return [...zoomSteps].reverse().find((s) => s < z) ?? 50;
}

/** What a key press asks of the zoom: in, out, reset, or nothing (Cmd on a Mac, Ctrl elsewhere). */
export function zoomKey(e: { key: string; metaKey: boolean; ctrlKey: boolean; altKey: boolean }, mac: boolean): "in" | "out" | "reset" | null {
  if ((mac ? !e.metaKey || e.ctrlKey : !e.ctrlKey || e.metaKey) || e.altKey) return null;
  if (e.key === "=" || e.key === "+") return "in";
  if (e.key === "-" || e.key === "_") return "out";
  if (e.key === "0") return "reset";
  return null;
}

const mac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);

/**
 * The pane's zoom, in percent, and a way to set it: the keys, the wheel
 * and a pinch over the pane change it, and it's saved shortly after it
 * settles (one write, not one a wheel step).
 */
export function useEditorZoom(pane: RefObject<HTMLElement | null>): [number, (z: number) => void] {
  const { prefs, update } = useApp();
  const [zoom, setLocal] = useState(prefs.editor_zoom);
  const zoomRef = useRef(zoom);
  zoomRef.current = zoom;
  const saveTimer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const pending = useRef<(() => void) | undefined>(undefined);
  useEffect(() => setLocal(prefs.editor_zoom), [prefs.editor_zoom]);

  const set = useCallback(
    (z: number) => {
      const next = clampZoom(z);
      setLocal(next);
      clearTimeout(saveTimer.current);
      pending.current = () => {
        pending.current = undefined;
        update((p) => (p.editor_zoom === next ? p : { ...p, editor_zoom: next }));
      };
      saveTimer.current = setTimeout(() => pending.current?.(), 400);
    },
    [update],
  );

  useEffect(() => {
    const el = pane.current;
    if (!el) return;
    const onKey = (e: KeyboardEvent) => {
      const k = zoomKey(e, mac);
      if (!k) return;
      e.preventDefault();
      e.stopPropagation();
      set(k === "reset" ? 100 : stepZoom(zoomRef.current, k === "in" ? 1 : -1));
    };
    const onWheel = (e: WheelEvent) => {
      if (!e.ctrlKey && !e.metaKey) return;
      e.preventDefault();
      const px = e.deltaMode === 1 ? e.deltaY * 16 : e.deltaY;
      // A mouse wheel's notch (about 100 px, capped at 50) is about a step;
      // a trackpad's pinch, small deltas, is smooth.
      set(zoomRef.current * Math.exp(-Math.max(-50, Math.min(50, px)) * 0.002));
    };
    // WebKit's trackpad pinch.
    let start = 100;
    const onGestureStart = (e: Event) => {
      e.preventDefault();
      start = zoomRef.current;
    };
    const onGestureChange = (e: Event) => {
      e.preventDefault();
      set(start * (e as Event & { scale: number }).scale);
    };
    el.addEventListener("keydown", onKey, true);
    el.addEventListener("wheel", onWheel, { passive: false });
    el.addEventListener("gesturestart", onGestureStart);
    el.addEventListener("gesturechange", onGestureChange);
    return () => {
      el.removeEventListener("keydown", onKey, true);
      el.removeEventListener("wheel", onWheel);
      el.removeEventListener("gesturestart", onGestureStart);
      el.removeEventListener("gesturechange", onGestureChange);
    };
  }, [pane, set]);

  // Leaving: a zoom not yet saved is saved now.
  useEffect(
    () => () => {
      clearTimeout(saveTimer.current);
      pending.current?.();
    },
    [],
  );
  return [zoom, set];
}
