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

// Every exported function, component, class, type and constant of the
// page's code has a doc comment (spec_release_readiness_030 RR-12), as the
// Go code's nogo check requires there. It reads the sources with the
// TypeScript compiler; the license header doesn't count.
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import ts from "typescript";
import { describe, expect, it } from "vitest";

const dir = import.meta.dirname;
const skip = (f: string) => f.endsWith(".test.ts") || f.endsWith(".d.ts") || f.startsWith("gen");

function sources(d: string, rel = ""): string[] {
  return readdirSync(d, { withFileTypes: true }).flatMap((e) => {
    const r = rel ? `${rel}/${e.name}` : e.name;
    if (skip(r)) return [];
    if (e.isDirectory()) return sources(join(d, e.name), r);
    return /\.(ts|tsx)$/.test(e.name) ? [r] : [];
  });
}

/** Whether a statement has a comment before it other than the license header. */
function documented(text: string, node: ts.Node): boolean {
  const ranges = ts.getLeadingCommentRanges(text, node.getFullStart()) ?? [];
  return ranges.some((r) => !text.slice(r.pos, r.end).includes("Licensed under the Apache License"));
}

function undocumented(file: string): string[] {
  const text = readFileSync(join(dir, file), "utf8");
  const sf = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, file.endsWith("x") ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
  const out: string[] = [];
  for (const st of sf.statements) {
    const exported = ts.canHaveModifiers(st) && ts.getModifiers(st)?.some((m) => m.kind === ts.SyntaxKind.ExportKeyword);
    if (!exported || documented(text, st)) continue;
    let name = "?";
    if (ts.isFunctionDeclaration(st) || ts.isClassDeclaration(st) || ts.isInterfaceDeclaration(st) || ts.isTypeAliasDeclaration(st) || ts.isEnumDeclaration(st)) name = st.name?.text ?? "default";
    else if (ts.isVariableStatement(st)) name = st.declarationList.declarations.map((d) => d.name.getText(sf)).join(", ");
    out.push(`${file}:${sf.getLineAndCharacterOfPosition(st.getStart()).line + 1}: ${name}`);
  }
  return out;
}

describe("the page's code", () => {
  it("documents what it exports", () => {
    expect(sources(dir).flatMap(undocumented)).toEqual([]);
  });
});
