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
import { useEffect, useState } from "react";

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
