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


// The visual editor's Markdown (spec_visual_editor_037 VE-90): every
// Markdown file in the repository reads and writes back unchanged, and an
// edit rewrites only what it touched.
import { existsSync, readdirSync, readFileSync, realpathSync, statSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { getSchema } from "@tiptap/core";
import { Node as PMNode, type Schema } from "@tiptap/pm/model";
import { Transform } from "@tiptap/pm/transform";
import { describe, expect, it } from "vitest";
import { escapeText, fingerprints, parseMarkdown, serializeMarkdown } from "./markdown";
import { visualExtensions } from "./schema";

const schema: Schema = getSchema(visualExtensions());

/** text read, put into the schema (as the editor does), and written back. */
function roundTrip(text: string, edit?: (doc: PMNode) => PMNode): string {
  const parsed = parseMarkdown(text);
  const doc = PMNode.fromJSON(schema, parsed.doc);
  doc.check();
  const prints = fingerprints(doc);
  return serializeMarkdown(edit ? edit(doc) : doc, parsed, prints);
}

// The repository's Markdown, which Bazel puts beside the page's files (the
// markdown filegroups): the output tree's root is three folders up.
function repository(): string | undefined {
  const root = join(process.cwd(), "..", "..", "..");
  return existsSync(join(root, "docs", "content")) ? root : undefined;
}

function markdownFiles(root: string): string[] {
  const out: string[] = [];
  const walk = (dir: string) => {
    for (const name of readdirSync(dir)) {
      if (name === "node_modules" || name.startsWith("bazel-") || name === ".git" || name.endsWith(".runfiles") || name === "external") continue;
      const p = join(dir, name);
      const st = statSync(p);
      if (st.isDirectory()) walk(p);
      else if (name.endsWith(".md")) out.push(p);
    }
  };
  walk(root);
  return out;
}

describe("round trip, untouched", () => {
  const here = dirname(realpathSync("src/files/visual/markdown.test.ts"));
  const fixtures = readdirSync(join(here, "testdata")).map((f) => join(here, "testdata", f));
  const repo = repository();
  const files = [...fixtures, ...(repo ? markdownFiles(repo) : [])];

  it("finds the repository's Markdown", () => {
    expect(repo, "the markdown filegroups are the test's data").toBeDefined();
    expect(files.length).toBeGreaterThan(100);
  });

  it.each(files.map((f) => [repo ? relative(repo, f) : f, f]))("%s", (_name, file) => {
    const text = readFileSync(file, "utf8");
    expect(roundTrip(text)).toBe(text);
  });

  it.each([[""], ["\n"], ["one line"], ["no newline at the end\n\nsecond"], ["\r\nwindows\r\n\r\nline endings\r\n"]])("%j", (text) => {
    expect(roundTrip(text)).toBe(text);
  });
});

describe("round trip, edited", () => {
  const text = readFileSync(join(dirname(realpathSync("src/files/visual/markdown.test.ts")), "testdata", "edge.md"), "utf8");

  it("rewrites only the block that changed", () => {
    const parsed = parseMarkdown(text);
    const doc = PMNode.fromJSON(schema, parsed.doc);
    const prints = fingerprints(doc);
    // The first paragraph (after the front matter and the heading) gets a word.
    let target = -1;
    doc.forEach((n, offset) => {
      if (target < 0 && n.type.name === "paragraph") target = offset + 1;
    });
    const edited = new Transform(doc).insert(target, schema.text("Hello ")).doc;
    const out = serializeMarkdown(edited, parsed, prints);
    const [before, after] = [text.split("\n"), out.split("\n")];
    const changed = after.filter((l, i) => l !== before[i]);
    expect(out.startsWith(text.slice(0, text.indexOf("A paragraph")))).toBe(true);
    expect(out).toContain("Hello A paragraph with *emphasis*, **strong**, ~~strike~~, `code`");
    expect(out.slice(out.indexOf("* star bullet"))).toBe(text.slice(text.indexOf("* star bullet")));
    expect(changed.length).toBeLessThanOrEqual(3);
  });

  it("prints a new document in the house style", () => {
    const doc = schema.nodeFromJSON({
      type: "doc",
      content: [
        { type: "heading", attrs: { level: 2 }, content: [{ type: "text", text: "Title" }] },
        { type: "paragraph", content: [{ type: "text", text: "bold", marks: [{ type: "bold" }] }, { type: "text", text: " and " }, { type: "text", text: "a link", marks: [{ type: "link", attrs: { href: "https://x.y" } }] }] },
        { type: "bulletList", content: [{ type: "listItem", content: [{ type: "paragraph", content: [{ type: "text", text: "one" }] }, { type: "bulletList", content: [{ type: "listItem", content: [{ type: "paragraph", content: [{ type: "text", text: "nested" }] }] }] }] }] },
        { type: "orderedList", attrs: { start: 3 }, content: [{ type: "listItem", content: [{ type: "paragraph", content: [{ type: "text", text: "three" }] }] }] },
        { type: "taskList", content: [{ type: "taskItem", attrs: { checked: true }, content: [{ type: "paragraph", content: [{ type: "text", text: "done" }] }] }] },
        { type: "blockquote", content: [{ type: "paragraph", content: [{ type: "text", text: "quoted" }] }] },
        { type: "codeBlock", attrs: { language: "go" }, content: [{ type: "text", text: "x := \"```\"" }] },
        {
          type: "table",
          attrs: { align: ["left", "right"] },
          content: [
            { type: "tableRow", content: [{ type: "tableHeader", content: [{ type: "paragraph", content: [{ type: "text", text: "A" }] }] }, { type: "tableHeader", content: [{ type: "paragraph", content: [{ type: "text", text: "B|C" }] }] }] },
            { type: "tableRow", content: [{ type: "tableCell", content: [{ type: "paragraph", content: [{ type: "text", text: "1" }] }] }, { type: "tableCell", content: [{ type: "paragraph", content: [{ type: "text", text: "22" }] }] }] },
          ],
        },
        { type: "horizontalRule" },
      ],
    });
    const out = serializeMarkdown(doc, { sources: new Map(), trailing: "" }, new Map());
    expect(out).toBe(
      [
        "## Title",
        "",
        "**bold** and [a link](https://x.y)",
        "",
        "- one\n  - nested",
        "",
        "3. three",
        "",
        "- [x] done",
        "",
        "> quoted",
        "",
        "````go\nx := \"```\"\n````",
        "",
        "| A   | B\\|C |\n| :-- | ---: |\n| 1   | 22   |",
        "",
        "---",
        "",
      ].join("\n"),
    );
    // And it reads back as the same document.
    const again = PMNode.fromJSON(schema, parseMarkdown(out).doc);
    expect(serializeMarkdown(again, { sources: new Map(), trailing: "" }, new Map())).toBe(out);
  });
});

describe("the editor's empty last line", () => {
  // The editor adds an empty paragraph after a last block that isn't one
  // (TipTap's trailing node), to type on; the file doesn't get it.
  it.each([["ends with code\n\n```go\nx := 1\n```\n"], ["# Title\n\n| a |\n|---|\n| 1 |\n"], ["```mermaid\nflowchart LR\n  A --> B\n```"]])("%j is written unchanged", (text) => {
    const parsed = parseMarkdown(text);
    const doc = PMNode.fromJSON(schema, parsed.doc);
    const prints = fingerprints(doc);
    const withLine = doc.copy(doc.content.addToEnd(schema.nodes.paragraph.create()));
    expect(serializeMarkdown(withLine, parsed, prints)).toBe(text);
  });
  it("is written once it's typed in", () => {
    const text = "```go\nx := 1\n```\n";
    const parsed = parseMarkdown(text);
    const doc = PMNode.fromJSON(schema, parsed.doc);
    const prints = fingerprints(doc);
    const typed = doc.copy(doc.content.addToEnd(schema.nodes.paragraph.create(null, schema.text("Next"))));
    expect(serializeMarkdown(typed, parsed, prints)).toBe("```go\nx := 1\n```\n\nNext\n");
  });
});

describe("escaping", () => {
  it.each([
    ["a*b", "a\\*b"],
    ["snake_case stays", "snake_case stays"],
    ["_leading", "\\_leading"],
    ["[x]", "\\[x\\]"],
    ["a <b> c", "a \\<b> c"],
    ["1 < 2", "1 < 2"],
    ["back\\slash", "back\\\\slash"],
  ])("%j", (text, want) => {
    expect(escapeText(text)).toBe(want);
  });
  it("escapes pipes in tables", () => expect(escapeText("a|b", true)).toBe("a\\|b"));
});

