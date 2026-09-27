import { describe, expect, it } from "vitest";
import { isValidElement } from "react";
import { highlight, languageFor } from "./highlight";

describe("highlight", () => {
  it("finds a grammar from a fence or a file name", () => {
    expect(languageFor("go")).toBe("go");
    expect(languageFor("sh")).toBe("bash");
    expect(languageFor("internal/cart/discount.go")).toBe("go");
    expect(languageFor("web/App.tsx")).toBe("typescript");
    expect(languageFor("Makefile")).toBe("makefile");
    expect(languageFor("notes.unknownext")).toBeUndefined();
    expect(languageFor("")).toBeUndefined();
  });
  it("returns elements for known languages and text otherwise", () => {
    expect(isValidElement(highlight("func main() {}", "go"))).toBe(true);
    expect(highlight("plain", undefined)).toBe("plain");
    expect(highlight("<script>alert(1)</script>", "nosuchlang")).toBe("<script>alert(1)</script>");
  });
});
