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


import { t } from "./i18n";

/**
 * A panel's edge that sets its width when dragged, kept when let go: the
 * left edge of a panel on the right (edge "left"), or the right edge of
 * one on the left. The width stays between min and max() (read as the drag
 * starts, for the window's size then); double-click restores the default.
 */
export function ResizeHandle({
  width,
  edge,
  min,
  max,
  label,
  onResize,
}: {
  width: number;
  edge: "left" | "right";
  min: number;
  max: () => number;
  label: string;
  onResize: (w: number) => void;
}) {
  const drag = (e: React.PointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    const handle = e.currentTarget;
    const panel = handle.parentElement!;
    const box = panel.getBoundingClientRect();
    const hi = Math.max(min, max());
    handle.setPointerCapture?.(e.pointerId);
    followDrag({
      win: window,
      handle,
      body: document.body,
      start: width,
      widthAt: (x) => Math.round(Math.min(Math.max(edge === "left" ? box.right - x : x - box.left, min), hi)),
      show: (w) => {
        panel.style.width = `${w}px`;
      },
      end: onResize,
    });
  };
  return (
    <div
      className={`panel-resize ${edge}`}
      onPointerDown={drag}
      onDoubleClick={() => onResize(0)}
      role="separator"
      aria-orientation="vertical"
      aria-label={label}
      title={t("desktop.resize_hint")}
    />
  );
}

/**
 * Follows a drag: each move shows the width at the pointer, and the drag
 * ends once, with the last width, when the pointer is let go or the drag
 * is cancelled (the system's pointercancel, or the handle losing the
 * pointer), so nothing is left listening, nor the body marked resizing.
 */
export function followDrag({
  win,
  handle,
  body,
  start,
  widthAt,
  show,
  end,
}: {
  win: EventTarget;
  handle: EventTarget;
  body: { classList: { add(c: string): void; remove(c: string): void } };
  start: number;
  widthAt: (clientX: number) => number;
  show: (w: number) => void;
  end: (w: number) => void;
}) {
  let w = start;
  const move = (ev: Event) => {
    w = widthAt((ev as PointerEvent).clientX);
    show(w);
  };
  const stop = () => {
    win.removeEventListener("pointermove", move);
    win.removeEventListener("pointerup", stop);
    win.removeEventListener("pointercancel", stop);
    handle.removeEventListener("lostpointercapture", stop);
    body.classList.remove("resizing");
    end(w);
  };
  body.classList.add("resizing");
  win.addEventListener("pointermove", move);
  win.addEventListener("pointerup", stop);
  win.addEventListener("pointercancel", stop);
  handle.addEventListener("lostpointercapture", stop);
}
