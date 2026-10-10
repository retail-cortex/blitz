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

import { EditorSelection, EditorState } from "@codemirror/state";
import { describe, expect, it } from "vitest";
import { tabAction } from "./codemirror";

// Where the cursor is (|) and what Tab does there.
describe("Tab", () => {
  it.each([
    ["|fmt.Println()", "indent"],
    ["    |x := 1", "indent"],
    ["\t\t|", "indent"],
    ["x :=|", "complete"],
    ["fmt.|", "complete"],
    ["os.pa|", "complete"],
    ["x := 1 |", "insert"],
    ["x\t|", "insert"],
  ])("%j: %s", (text, want) => {
    const at = text.indexOf("|");
    const state = EditorState.create({ doc: text.replace("|", ""), selection: EditorSelection.cursor(at) });
    expect(tabAction(state)).toBe(want);
  });

  it("indents over a selection, and completes only when every cursor is in a word", () => {
    const doc = "foo\nbar ";
    expect(tabAction(EditorState.create({ doc, selection: EditorSelection.range(0, 3) }))).toBe("indent");
    const two = (a: number, b: number) => EditorState.create({ doc, selection: EditorSelection.create([EditorSelection.cursor(a), EditorSelection.cursor(b)]), extensions: EditorState.allowMultipleSelections.of(true) });
    expect(tabAction(two(3, 7))).toBe("complete"); // one in a word, one after a blank
    expect(tabAction(two(0, 4))).toBe("indent");
  });
});
