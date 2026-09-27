import { describe, expect, it } from "vitest";
import { allCommands, builtins, filterPalette, helpText, matchCommands, parseCommand, type CommandSpec } from "./commands";

const custom: CommandSpec[] = [
  { name: "review", description: "Review changes", source: "bundled" },
  { name: "db:migrate", args: "<name>", description: "Write a migration", source: "project" },
  { name: "plan", description: "A project command shadowed by the built-in", source: "project" },
];

describe("commands", () => {
  it("parses a composer line", () => {
    expect(parseCommand("/compact the API design")).toEqual({ name: "compact", args: "the API design" });
    expect(parseCommand("  /Undo  ")).toEqual({ name: "undo", args: "" });
    expect(parseCommand("/plan add\nmulti-line goal")).toEqual({ name: "plan", args: "add\nmulti-line goal" });
    expect(parseCommand("fix /etc/hosts")).toBeUndefined();
    expect(parseCommand("/")).toBeUndefined();
    expect(parseCommand("//not a command")).toBeUndefined();
    expect(parseCommand("/usr/bin is odd")).toBeUndefined(); // a path, not a command
  });

  it("offers matches while the name is typed", () => {
    const all = allCommands(custom);
    expect(matchCommands("/co", all).map((c) => c.name)).toEqual(["cost", "context", "compact"]);
    expect(matchCommands("/snap", all).map((c) => c.name)).toEqual(["session"]); // by its description
    expect(matchCommands("/migr", all).map((c) => c.name)).toEqual(["db:migrate"]);
    expect(matchCommands("/cost now", all)).toEqual([]); // past the name
    expect(matchCommands("hello", all)).toEqual([]);
    expect(matchCommands("/", all)).toHaveLength(12);
  });

  it("lets built-ins win and lists everything in /help", () => {
    const all = allCommands(custom);
    expect(all.filter((c) => c.name === "plan")).toHaveLength(1);
    expect(all.find((c) => c.name === "plan")?.source).toBe("builtin");
    const help = helpText(all);
    expect(help).toContain("`/db:migrate <name>`");
    expect(help).toContain("`/undo [--force]`");
    expect(help).toContain("`/mode [default\\|accept-edits\\|plan\\|dont-ask\\|bypass]`"); // pipes escaped in the table
    expect(builtins().every((b) => help.includes(`/${b.name}`))).toBe(true);
  });
});

describe("filterPalette", () => {
  const noop = () => {};
  const items = [
    { group: "Commands", label: "/cost", detail: "Tokens and cost", run: noop },
    { group: "Commands", label: "/compact", detail: "Summarize older turns", run: noop },
    { group: "Workspaces", label: "Shop", detail: "/Users/me/shop", run: noop },
    { group: "Settings", label: "Dark theme", run: noop },
  ];
  it("matches every word, label starts first", () => {
    expect(filterPalette(items, "").length).toBe(4);
    expect(filterPalette(items, "co").map((i) => i.label)).toEqual(["/cost", "/compact"]);
    expect(filterPalette(items, "older sum").map((i) => i.label)).toEqual(["/compact"]);
    expect(filterPalette(items, "shop").map((i) => i.label)).toEqual(["Shop"]);
    expect(filterPalette(items, "theme").map((i) => i.label)).toEqual(["Dark theme"]);
    expect(filterPalette(items, "workspaces").map((i) => i.label)).toEqual(["Shop"]); // by group
    expect(filterPalette(items, "nothing like it")).toEqual([]);
    // A label match beats a description match, whatever the group order.
    const withView = [{ group: "Commands", label: "/review", detail: "Review the uncommitted changes", run: noop }, { group: "View", label: "Show the changes", run: noop }];
    expect(filterPalette(withView, "changes").map((i) => i.label)).toEqual(["Show the changes", "/review"]);
  });
});
