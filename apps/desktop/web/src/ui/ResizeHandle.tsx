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


import { t } from "../i18n";

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
    const panel = e.currentTarget.parentElement!;
    const box = panel.getBoundingClientRect();
    const hi = Math.max(min, max());
    let w = width;
    const move = (ev: PointerEvent) => {
      w = Math.round(Math.min(Math.max(edge === "left" ? box.right - ev.clientX : ev.clientX - box.left, min), hi));
      panel.style.width = `${w}px`;
    };
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      document.body.classList.remove("resizing");
      onResize(w);
    };
    document.body.classList.add("resizing");
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
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
