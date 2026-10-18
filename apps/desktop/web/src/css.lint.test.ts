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

// Every stylesheet's blocks close: a block left open (an @container
// query missing its brace) quietly nests every rule after it, which then
// applies only where that query holds.
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const dir = import.meta.dirname;

function stylesheets(d: string, rel = ""): string[] {
  return readdirSync(d, { withFileTypes: true }).flatMap((e) => {
    const r = rel ? `${rel}/${e.name}` : e.name;
    if (e.isDirectory()) return e.name === "gen" ? [] : stylesheets(join(d, e.name), r);
    return e.name.endsWith(".css") ? [r] : [];
  });
}

/** The line of each block still open at the end, comments ignored. */
function unclosed(text: string): number[] {
  const open: number[] = [];
  let line = 1;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (c === "/" && text[i + 1] === "*") {
      const end = text.indexOf("*/", i + 2);
      const comment = text.slice(i, end < 0 ? text.length : end + 2);
      line += comment.split("\n").length - 1;
      i += comment.length - 1;
    } else if (c === "\n") line++;
    else if (c === "{") open.push(line);
    else if (c === "}") open.pop();
  }
  return open;
}

describe("the stylesheets", () => {
  const files = stylesheets(dir);
  it("are found", () => {
    expect(files).toContain("app.css");
  });
  it.each(files)("%s closes every block", (file) => {
    expect(unclosed(readFileSync(join(dir, file), "utf8"))).toEqual([]);
  });
});
