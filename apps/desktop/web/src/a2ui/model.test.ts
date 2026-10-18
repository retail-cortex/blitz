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
import { actionMessage, actionOf, byId, childrenOf, failing, formProblems, getPath, parseForm, parseMessages, resolve, safeUrl, setPath, starterForm, type Component } from "./model";

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
    expect(formProblems({ components: [field] }, data).get("f")).toEqual(["Required"]);
    const button: Component = { id: "b", component: "Button", child: "l", action: { event: { name: "approve", context: { note: { path: "/form/note" }, fixed: 1 } } } };
    const a = actionOf(button, data)!;
    expect(a).toEqual({ name: "approve", context: { note: "hi", fixed: 1 } });
    expect(actionMessage("run-7/ok", { ...a, sourceComponentId: "b", data }, new Date("2026-10-18T09:00:00Z"))).toEqual({
      version: "v0.9",
      action: { name: "approve", surfaceId: "run-7/ok", sourceComponentId: "b", timestamp: "2026-10-18T09:00:00.000Z", context: { note: "hi", fixed: 1 } },
    });
    expect(actionOf({ id: "x", component: "Button" }, data)).toBeNull();
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
    expect(parseForm("{").problem).toBeTruthy();
    expect(safeUrl("https://example.com/a.png")).toBe(true);
    expect(safeUrl("javascript:alert(1)")).toBe(false);
    expect(safeUrl("http://example.com/a.png")).toBe(false);
  });

  it.each([
    ["not JSON", "{", undefined],
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
});
