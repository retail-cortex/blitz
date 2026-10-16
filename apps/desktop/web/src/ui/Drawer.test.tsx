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


import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { Dialog } from "./controls";
import { Drawer, drawerDocks, drawerMin, drawerWidth } from "./Drawer";

describe("drawerWidth", () => {
  it.each([
    { name: "720 px by default in a wide window", saved: 0, win: 1800, want: 720 },
    { name: "45% of a smaller window", saved: 0, win: 1200, want: 540 },
    { name: "never below the minimum", saved: 0, win: 700, want: drawerMin },
    { name: "the saved width", saved: 600, win: 1600, want: 600 },
    { name: "a saved width kept to the window", saved: 2000, win: 1200, want: 1152 },
  ])("$name", ({ saved, win, want }) => expect(drawerWidth(saved, win)).toBe(want));
});

describe("drawerDocks", () => {
  it.each([
    { name: "docks beside the workspace in a wide window", width: 560, win: 1440, want: true },
    { name: "floats over the chat when the workspace would get too narrow", width: 560, win: 1100, want: false },
    { name: "docks with exactly the workspace's room", width: 400, win: 1040, want: true },
  ])("$name", ({ width, win, want }) => expect(drawerDocks(width, win)).toBe(want));
});

// What the views would show (not the page's own text).
const [settings, commit, body, foot] = ["Settings", "Commit", "body", "foot"];

describe("Drawer", () => {
  const html = renderToStaticMarkup(
    <Drawer title={settings} onClose={() => {}} footer={<span>{foot}</span>}>
      <p>{body}</p>
    </Drawer>,
  );

  it("is a region beside the page, not a modal", () => {
    expect(html).toContain('role="complementary"');
    expect(html).toContain('aria-label="Settings"');
    expect(html).not.toContain("aria-modal");
  });

  it("has a header that drags the window, a close button, its body and footer", () => {
    expect(html).toMatch(/<header class="drawer-head drag-region">/);
    expect(html).toContain('aria-label="Close"');
    expect(html).toContain("<p>body</p>");
    expect(html).toContain("<footer><span>foot</span></footer>");
  });
});

describe("Dialog", () => {
  it("keeps the title bar dragging the window over its scrim", () => {
    const html = renderToStaticMarkup(
      <Dialog title={commit} onClose={() => {}}>
        <p>{body}</p>
      </Dialog>,
    );
    expect(html).toMatch(/<div class="scrim"><div class="scrim-drag drag-region" aria-hidden="true"><\/div>/);
    expect(html).toContain('aria-modal="true"');
  });
});
