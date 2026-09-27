import { describe, expect, it } from "vitest";
import { finishedAfterMs, shouldNotify } from "./notify";

describe("shouldNotify", () => {
  const base = { enabled: true, focused: false, shown: true, elapsedMs: finishedAfterMs };
  it("notifies only when the user is looking elsewhere", () => {
    expect(shouldNotify("waiting", base)).toBe(true);
    expect(shouldNotify("waiting", { ...base, focused: true })).toBe(false);
    expect(shouldNotify("waiting", { ...base, focused: true, shown: false })).toBe(true); // another workspace
    expect(shouldNotify("waiting", { ...base, enabled: false })).toBe(false);
  });
  it("skips quick turns, never a wait", () => {
    expect(shouldNotify("finished", { ...base, elapsedMs: 2000 })).toBe(false);
    expect(shouldNotify("finished", base)).toBe(true);
    expect(shouldNotify("waiting", { ...base, elapsedMs: 0 })).toBe(true);
  });
});
