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

import { describe, expect, it } from "vitest";
import { isFocusComposerKey } from "./composerDock";

const key = (k: string, mods: Partial<Record<"metaKey" | "ctrlKey" | "altKey" | "shiftKey", boolean>> = {}) => ({ key: k, metaKey: false, ctrlKey: false, altKey: false, shiftKey: false, ...mods });

describe("the composer's shortcut", () => {
  it.each([
    ["⌘L", key("l", { metaKey: true }), true],
    ["Ctrl+L", key("l", { ctrlKey: true }), true],
    ["⌘L with caps lock", key("L", { metaKey: true }), true],
    ["L alone", key("l"), false],
    ["⌘⇧L", key("L", { metaKey: true, shiftKey: true }), false],
    ["⌥⌘L", key("l", { metaKey: true, altKey: true }), false],
    ["⌘K", key("k", { metaKey: true }), false],
  ])("%s", (_, e, want) => {
    expect(isFocusComposerKey(e)).toBe(want);
  });
});
