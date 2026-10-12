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

// Pan and zoom for a drawn diagram (spec_files_029 FIL-66), wherever one
// shows (Preview, Visual, the chat): ⌘/Ctrl and the wheel, or a pinch,
// zoom at the pointer; dragging pans; a double-click fits it again. The
// diagram's head gets + / − / Fit / Expand, Expand a window-sized viewer.
// Plain DOM, so React and the visual editor's node views share it.
import { mdiArrowExpand, mdiClose, mdiFitToPageOutline, mdiMagnifyMinusOutline, mdiMagnifyPlusOutline } from "@mdi/js";
import { t } from "./i18n";

/** Where the diagram is: its scale, and its offset in the host (pixels). */
export interface View {
  scale: number;
  x: number;
  y: number;
}

/** The diagram as drawn: not zoomed or moved. */
export const fit: View = { scale: 1, x: 0, y: 0 };
const minScale = 0.25;
const maxScale = 8;
const step = 1.25;

/** v zoomed by factor, the point (px, py) of the diagram's box staying where it is. */
export function zoomAt(v: View, factor: number, px: number, py: number): View {
  const scale = Math.min(maxScale, Math.max(minScale, v.scale * factor));
  const k = scale / v.scale;
  return { scale, x: px - (px - v.x) * k, y: py - (py - v.y) * k };
}

/** The zoom factor of a wheel event (a pinch is a wheel with Ctrl on most platforms). */
export function wheelFactor(deltaY: number, deltaMode: number): number {
  const px = deltaMode === 1 ? deltaY * 16 : deltaMode === 2 ? deltaY * 400 : deltaY;
  return Math.exp(-Math.max(-100, Math.min(100, px)) * 0.002); // a notch about a step
}

/** A diagram that pans and zooms. */
export interface PanZoom {
  zoomIn(): void;
  zoomOut(): void;
  reset(): void;
  destroy(): void;
}

/**
 * Pan and zoom for the <svg> in host (found on each gesture, so a diagram
 * drawn again keeps working); onChange hears each new view.
 */
export function panZoom(host: HTMLElement, onChange?: (v: View) => void): PanZoom {
  let view = fit;
  const svg = () => host.querySelector<SVGSVGElement>(":scope > svg");
  const apply = () => {
    const s = svg();
    if (!s) return;
    const moved = view.scale !== 1 || view.x !== 0 || view.y !== 0;
    s.style.transformOrigin = "0 0";
    s.style.transform = moved ? `translate(${view.x}px, ${view.y}px) scale(${view.scale})` : "";
    host.classList.toggle("zoomed", moved);
    onChange?.(view);
  };
  // A point on the screen, from the corner the diagram rests at (its box
  // less the pan: the zoom keeps its corner, transform-origin 0 0).
  const local = (clientX: number, clientY: number) => {
    const r = svg()?.getBoundingClientRect();
    return r ? { x: clientX - (r.left - view.x), y: clientY - (r.top - view.y) } : { x: 0, y: 0 };
  };
  const centre = () => {
    const s = svg();
    return { x: (s?.clientWidth ?? 0) / 2, y: (s?.clientHeight ?? 0) / 2 };
  };
  const zoom = (factor: number, at = centre()) => {
    view = zoomAt(view, factor, at.x, at.y);
    apply();
  };

  const onWheel = (e: WheelEvent) => {
    if (!e.ctrlKey && !e.metaKey) return; // the page scrolls
    e.preventDefault();
    e.stopPropagation(); // not the editor's zoom
    zoom(wheelFactor(e.deltaY, e.deltaMode), local(e.clientX, e.clientY));
  };
  // WebKit's trackpad pinch: gesture events with a scale since it began.
  let pinch = 1;
  const onGestureStart = (e: Event) => {
    e.preventDefault();
    e.stopPropagation();
    pinch = 1;
  };
  const onGestureChange = (e: Event) => {
    e.preventDefault();
    e.stopPropagation();
    const g = e as Event & { scale: number; clientX: number; clientY: number };
    zoom(g.scale / pinch, local(g.clientX, g.clientY));
    pinch = g.scale;
  };
  let drag: { id: number; x: number; y: number } | null = null;
  const onDown = (e: PointerEvent) => {
    if (e.button !== 0 || (e.target as Element).closest("a, button")) return;
    drag = { id: e.pointerId, x: e.clientX, y: e.clientY };
    host.setPointerCapture?.(e.pointerId);
    host.classList.add("panning");
  };
  const onMove = (e: PointerEvent) => {
    if (!drag || drag.id !== e.pointerId) return;
    view = { ...view, x: view.x + e.clientX - drag.x, y: view.y + e.clientY - drag.y };
    drag = { ...drag, x: e.clientX, y: e.clientY };
    apply();
  };
  const onUp = (e: PointerEvent) => {
    if (drag?.id !== e.pointerId) return;
    drag = null;
    host.classList.remove("panning");
  };
  const onDouble = () => {
    view = fit;
    apply();
  };

  host.classList.add("pannable");
  host.addEventListener("wheel", onWheel, { passive: false });
  host.addEventListener("gesturestart", onGestureStart);
  host.addEventListener("gesturechange", onGestureChange);
  host.addEventListener("pointerdown", onDown);
  host.addEventListener("pointermove", onMove);
  host.addEventListener("pointerup", onUp);
  host.addEventListener("pointercancel", onUp);
  host.addEventListener("dblclick", onDouble);
  return {
    zoomIn: () => zoom(step),
    zoomOut: () => zoom(1 / step),
    reset: onDouble,
    destroy: () => {
      host.removeEventListener("wheel", onWheel);
      host.removeEventListener("gesturestart", onGestureStart);
      host.removeEventListener("gesturechange", onGestureChange);
      host.removeEventListener("pointerdown", onDown);
      host.removeEventListener("pointermove", onMove);
      host.removeEventListener("pointerup", onUp);
      host.removeEventListener("pointercancel", onUp);
      host.removeEventListener("dblclick", onDouble);
      host.classList.remove("pannable", "zoomed", "panning");
    },
  };
}

function iconButton(path: string, label: string, onClick: () => void): HTMLButtonElement {
  const b = document.createElement("button");
  b.type = "button";
  b.className = "icon-btn small diagram-control";
  b.title = label;
  b.setAttribute("aria-label", label);
  b.innerHTML = `<svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true"><path fill="currentColor" d="${path}"></path></svg>`;
  b.onmousedown = (e) => e.preventDefault(); // the editor keeps its cursor
  b.onclick = (e) => {
    e.stopPropagation();
    onClick();
  };
  return b;
}

/** The diagram's controls, for its head: zoom in and out, fit, and Expand (when the diagram's markup is given). */
export function diagramControls(pz: PanZoom, markup?: () => string): HTMLElement {
  const box = document.createElement("span");
  box.className = "diagram-controls";
  box.append(
    iconButton(mdiMagnifyMinusOutline, t("desktop.diagram.zoom_out"), () => pz.zoomOut()),
    iconButton(mdiMagnifyPlusOutline, t("desktop.diagram.zoom_in"), () => pz.zoomIn()),
    iconButton(mdiFitToPageOutline, t("desktop.diagram.fit"), () => pz.reset()),
  );
  if (markup) box.append(iconButton(mdiArrowExpand, t("desktop.diagram.expand"), () => openViewer(markup())));
  return box;
}

/**
 * The diagram over the whole window, fitted to it, to pan and zoom;
 * Escape, Close or a click beside it closes it. svg is Mermaid's own,
 * sanitised markup.
 */
export function openViewer(svg: string): () => void {
  const scrim = document.createElement("div");
  scrim.className = "diagram-viewer";
  scrim.setAttribute("role", "dialog");
  scrim.setAttribute("aria-label", t("desktop.mermaid.diagram"));
  const stage = document.createElement("div");
  stage.className = "diagram-viewer-stage mermaid-diagram";
  stage.innerHTML = svg;
  const s = stage.querySelector("svg");
  if (s) {
    s.style.transform = ""; // as drawn, not as zoomed where it came from
    s.style.maxWidth = "100%";
    s.style.maxHeight = "100%";
    s.removeAttribute("height");
    s.setAttribute("width", "100%");
  }
  const pz = panZoom(stage);
  const bar = document.createElement("div");
  bar.className = "diagram-viewer-bar";
  const close = () => {
    pz.destroy();
    scrim.remove();
    document.removeEventListener("keydown", onKey, true);
  };
  const onKey = (e: KeyboardEvent) => {
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      close();
    }
  };
  bar.append(diagramControls(pz), iconButton(mdiClose, t("desktop.diagram.close"), close));
  scrim.append(bar, stage);
  scrim.addEventListener("click", (e) => e.target === scrim && close());
  document.addEventListener("keydown", onKey, true);
  document.body.append(scrim);
  return close;
}
