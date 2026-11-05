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
import shared from "../../../../../pkg/a2ui/testdata/checks.json";
import { actionMessage, actionOf, byId, childrenOf, failing, formProblems, getPath, parseForm, parseMessages, problemText, re2ToJs, resolve, safeUrl, setPath, starterForm, type Component, type Form } from "./model";

describe("A2UI forms", () => {
  const data = { form: { note: "hi", tags: ["a"] }, items: [{ name: "x" }, { name: "y" }] };

  it("reads and writes data by JSON Pointer", () => {
    expect(getPath(data, "/form/note")).toBe("hi");
    expect(getPath(data, "/items/1/name")).toBe("y");
    expect(getPath(data, "/nope/x")).toBeUndefined();
    expect(getPath(data, "/")).toBe(data);
    const next = setPath(data, "/form/priority", "high");
    expect(next.form).toEqual({ note: "hi", tags: ["a"], priority: "high" });
    expect(data.form).not.toHaveProperty("priority");
    expect(setPath({}, "/a~1b", 1)).toEqual({ "a/b": 1 });
  });

  it("resolves literals, paths, template items and catalog functions", () => {
    expect(resolve("plain", data)).toBe("plain");
    expect(resolve({ path: "/form/note" }, data)).toBe("hi");
    expect(resolve({ path: "name" }, data, "/items/0")).toBe("x");
    expect(resolve({ call: "required", args: { value: { path: "/form/note" } } }, data)).toBe(true);
    expect(resolve({ call: "email", args: { value: "a@b.co" } }, data)).toBe(true);
    expect(resolve({ call: "formatString", args: { template: "Hello ${name}", value: { path: "/items/1" } } }, data)).toBe("Hello y");
    expect(resolve({ call: "eval", args: {} }, data), "nothing outside the catalog").toBeUndefined();
  });

  it("checks values and sends a button's context", () => {
    const field: Component = { id: "f", component: "TextField", value: { path: "/form/missing" }, checks: [{ call: "required", args: { value: { path: "/form/missing" } }, message: "Required" }] };
    expect(failing(field, data)).toEqual(["Required"]);
    expect(failing({ ...field, value: { path: "/form/note" }, checks: [{ call: "length", args: { value: { path: "/form/note" }, min: 5 }, message: "Too short" }] }, data)).toEqual(["Too short"]);
    expect(failing({ id: "r", component: "TextField", value: { path: "/form/note" }, validationRegexp: "^[0-9]+$" }, data)).toHaveLength(1);
    expect(formProblems({ components: [{ ...field, id: "root" }] }, data).get("root")).toEqual(["Required"]);
    const button: Component = { id: "b", component: "Button", child: "l", action: { event: { name: "approve", context: { note: { path: "/form/note" }, fixed: 1 } } } };
    const a = actionOf(button, data)!;
    expect(a).toEqual({ name: "approve", context: { note: "hi", fixed: 1 } });
    expect(actionMessage("run-7/ok", { ...a, sourceComponentId: "b", data }, new Date("2026-10-18T09:00:00Z"))).toEqual({
      version: "v0.9",
      action: { name: "approve", surfaceId: "run-7/ok", sourceComponentId: "b", timestamp: "2026-10-18T09:00:00.000Z", context: { note: "hi", fixed: 1 } },
    });
    expect(actionOf({ id: "x", component: "Button" }, data)).toBeNull();
  });

  it("fails a check calling an unknown function, or an unusable validationRegexp", () => {
    const field = (extra: Record<string, unknown>): Component => ({ id: "f", component: "TextField", value: { path: "/form/note" }, ...extra });
    expect(failing(field({ checks: [{ call: "requried", args: { value: { path: "/form/note" } }, message: "Say why" }] }), data)).toEqual(["Say why"]);
    expect(failing(field({ checks: [{ call: "not", args: { value: { call: "exec" } } }] }), data)).toHaveLength(1);
    expect(failing(field({ checks: [{ call: "or", args: { values: [true, { call: "toString" }] } }] }), data), "not an inherited name either").toHaveLength(1);
    expect(failing(field({ checks: [{ call: "not", args: { value: { call: "required", args: { value: { path: "/form/none" } } } } }] }), data)).toEqual([]);
    expect(failing(field({ validationRegexp: "(" }), data)).toHaveLength(1);
    expect(failing(field({ validationRegexp: "(", value: { path: "/form/none" } }), data), "even with no answer yet").toHaveLength(1);
    expect(failing(field({ validationRegexp: "a".repeat(201) }), data)).toHaveLength(1);
  });

  it("counts and reads values as the service does", () => {
    expect(resolve({ call: "length", args: { value: "😀😀😀", max: 3 } }, data), "code points, not UTF-16 units").toBe(true);
    expect(resolve({ call: "numeric", args: { value: " 5 " } }, data)).toBe(true);
    for (const value of ["  ", true, "0x10", "Infinity", null]) expect(resolve({ call: "numeric", args: { value } }, data), String(value)).toBe(false);
  });

  it("lists children, plain or from a template", () => {
    expect(childrenOf({ id: "c", component: "Column", children: ["a", "b"] }, data).map((c) => c.id)).toEqual(["a", "b"]);
    expect(childrenOf({ id: "l", component: "List", children: { path: "/items", componentId: "row" } }, data)).toEqual([
      { id: "row", scope: "/items/0" },
      { id: "row", scope: "/items/1" },
    ]);
  });

  it("parses a form and trusts only safe URLs", () => {
    expect(parseForm(JSON.stringify(starterForm())).form).toBeDefined();
    expect(byId(starterForm()).get("approve")?.component).toBe("Button");
    expect(parseForm("").problem).toBe("empty");
    expect(parseForm('{"components": [{"id": "x"}]}').problem).toBe("root");
    expect(parseForm("{").problem).toBe("json");
    expect(safeUrl("https://example.com/a.png")).toBe(true);
    expect(safeUrl("javascript:alert(1)")).toBe(false);
    expect(safeUrl("http://example.com/a.png")).toBe(false);
  });

  it.each([
    ["not JSON", "{", "json"],
    ["not a list", "{}", "messages"],
    ["no surface", JSON.stringify([{ version: "v0.9", updateComponents: { surfaceId: "s", components: [{ id: "root", component: "Text" }] } }]), "createSurface"],
    ["no root", JSON.stringify([{ version: "v0.9", createSurface: { surfaceId: "s", catalogId: "c" } }]), "root"],
  ])("refuses messages: %s", (_, text, problem) => {
    const r = parseMessages(text);
    expect(r.form).toBeUndefined();
    if (problem) expect(r.problem).toBe(problem);
  });

  it("reads a surface from its messages", () => {
    const r = parseMessages(
      JSON.stringify([
        { version: "v0.9", createSurface: { surfaceId: "run-7/ok", catalogId: "basic" } },
        { version: "v0.9", updateComponents: { surfaceId: "run-7/ok", components: [{ id: "root", component: "Text", text: { path: "/note" } }] } },
        { version: "v0.9", updateDataModel: { surfaceId: "run-7/ok", path: "/", value: { note: "hi", n: 1 } } },
        { version: "v0.9", updateDataModel: { surfaceId: "run-7/ok", path: "/note", value: "later" } },
      ]),
    );
    expect(r.surface).toBe("run-7/ok");
    expect(r.form?.data).toEqual({ note: "later", n: 1 });
    expect(r.form?.components).toHaveLength(1);
  });

  it.each(["json", "messages", "createSurface", "root", "empty", "components"] as const)("says the problem %s in words", (problem) => {
    expect(problemText(problem)).not.toContain("desktop.a2ui");
  });

  it.each([
    ["a lookahead", "(?=a)", null],
    ["a lookbehind", "(?<!a)b", null],
    ["a backreference", "(a)\\1", null],
    ["a named backreference", "(?<x>a)\\k<x>", null],
    ["an escaped backslash then a digit", "\\\\1", { source: "\\\\1", flags: "" }],
    ["a lookahead's text in a class", "[(?=]", { source: "[(?=]", flags: "" }],
    ["leading flags", "(?is)a.b", { source: "a.b", flags: "is" }],
    ["text anchors", "\\Aab\\z", { source: "^ab$", flags: "" }],
    ["a one-letter class", "\\pL\\PN", { source: "\\p{L}\\P{N}", flags: "" }],
    ["a ] first in a class", "[]a]", { source: "[\\]a]", flags: "" }],
  ])("reads an RE2 pattern: %s", (_, pattern, want) => {
    expect(re2ToJs(pattern)).toEqual(want);
  });
});

// The cases the service's copy (pkg/a2ui) is tested against too.
describe("A2UI cases shared with the service", () => {
  it.each(shared.functions.map((c) => [c.name, c] as const))("%s", (_, c) => {
    expect(resolve(c.value, ("data" in c ? c.data : {}) as Record<string, unknown>)).toEqual(c.want);
  });
  it.each(shared.forms.map((c) => [c.name, c] as const))("%s", (_, c) => {
    expect([...formProblems(c.form as unknown as Form, c.data as Record<string, unknown>).keys()]).toEqual(c.failing);
  });
});
